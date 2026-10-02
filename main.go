package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type watchEntry struct {
	fromStation *Station
	toStation   *Station
	date        string
	fromName    string
	toName      string
}

// monitor hält den Zustand zwischen den Zyklen (Watchlist, Zähler, Canary).
type monitor struct {
	client *OEBBClient
	cfg    *Config

	watchList            []watchEntry
	watchListInitialized bool
	consecutiveErrors    int
	errorAlerted         bool
	canaryEntry          *watchEntry // nil, solange die Canary-Stationen nicht aufgelöst sind
	canaryFailures       int
	canaryAlerted        bool
}

// newMonitor löst Watchlist- und Canary-Stationen tolerant auf. Schlägt die
// Auflösung fehl (HAFAS kurz down), wird in jedem Zyklus erneut versucht.
func newMonitor(client *OEBBClient, cfg *Config) *monitor {
	m := &monitor{client: client, cfg: cfg}

	m.watchList = resolveStations(client, cfg)
	if len(m.watchList) > 0 {
		m.watchListInitialized = true
		log.Printf("Watching %d route/date combination(s)", len(m.watchList))
	} else {
		log.Printf("⚠ No connections resolved at startup (ÖBB Fahrplanauskunft may be down). Will retry each cycle.")
	}

	m.canaryEntry = resolveCanary(client, cfg)
	return m
}

// runCheck führt einen Zyklus aus: Watchlist, Canary, Heartbeat (nur bei Erfolg).
func (m *monitor) runCheck() {
	if !m.watchListInitialized {
		log.Printf("Retrying station resolution...")
		m.watchList = resolveStations(m.client, m.cfg)
		if len(m.watchList) == 0 {
			log.Printf("⚠ Station resolution still failing — skipping cycle (no heartbeat ping)")
			return
		}
		m.watchListInitialized = true
		log.Printf("Resolved %d route/date combination(s) on retry", len(m.watchList))
	}

	checkOK := true
	if len(m.watchList) > 0 {
		checkOK = checkAll(m.client, m.cfg, &m.watchList, &m.consecutiveErrors, &m.errorAlerted)
	}

	if m.canaryEntry == nil {
		log.Printf("Retrying canary station resolution...")
		m.canaryEntry = resolveCanary(m.client, m.cfg)
	}
	canaryAPIOK := false
	if m.canaryEntry != nil {
		canaryAPIOK = runCanaryCheck(m.client, m.cfg, m.canaryEntry, &m.canaryFailures, &m.canaryAlerted)
	} else {
		log.Printf("⚠ Canary stations unresolved — no canary check this cycle")
	}

	if checkOK && canaryAPIOK {
		m.reportSuccess()
	} else {
		log.Printf("Success report skipped: API unhealthy (checkOK=%v, canaryAPIOK=%v) — Monitoring soll Alarm schlagen", checkOK, canaryAPIOK)
	}
}

// reportSuccess meldet einen fachlich erfolgreichen Zyklus: Erfolgsdatei (vom
// Server-Monitor auf Alter geprüft) und/oder optionaler BetterStack-Ping.
func (m *monitor) reportSuccess() {
	if m.cfg.SuccessFile != "" {
		stamp := time.Now().Format(time.RFC3339) + "\n"
		if err := os.WriteFile(m.cfg.SuccessFile, []byte(stamp), 0o644); err != nil {
			log.Printf("Success file write failed: %v", err)
		}
	}
	if m.cfg.HeartbeatURL != "" {
		resp, err := http.Get(m.cfg.HeartbeatURL)
		if err != nil {
			log.Printf("Heartbeat ping failed: %v", err)
		} else {
			resp.Body.Close()
		}
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	once := flag.Bool("once", false, "run check once and exit")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Loaded %d connection(s) to monitor", len(cfg.Connections))

	m := newMonitor(NewOEBBClient(), cfg)

	if *once {
		m.runCheck()
		return
	}

	// Scheduled mode
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run immediately on start
	m.runCheck()

	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()

	log.Printf("Scheduler started, checking every %s", cfg.CheckInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down gracefully...")
			return
		case <-ticker.C:
			if m.watchListInitialized && len(m.watchList) == 0 {
				log.Println("All connections notified, nothing left to watch. Exiting.")
				return
			}
			m.runCheck()
		}
	}
}

func resolveStations(client *OEBBClient, cfg *Config) []watchEntry {
	stationCache := make(map[string]*Station)
	var watchList []watchEntry

	for _, conn := range cfg.Connections {
		from, ok := resolveStation(client, conn.From, stationCache)
		if !ok {
			continue
		}
		to, ok := resolveStation(client, conn.To, stationCache)
		if !ok {
			continue
		}

		for _, date := range conn.Dates {
			watchList = append(watchList, watchEntry{
				fromStation: from,
				toStation:   to,
				date:        date,
				fromName:    conn.From,
				toName:      conn.To,
			})
		}
	}
	return watchList
}

// resolveCanary löst die Canary-Strecke auf; nil, wenn eine Station fehlschlägt.
func resolveCanary(client *OEBBClient, cfg *Config) *watchEntry {
	cache := make(map[string]*Station)
	from, ok := resolveStation(client, cfg.Canary.From, cache)
	if !ok {
		return nil
	}
	to, ok := resolveStation(client, cfg.Canary.To, cache)
	if !ok {
		return nil
	}
	log.Printf("Canary route: %s → %s", cfg.Canary.From, cfg.Canary.To)
	return &watchEntry{
		fromStation: from,
		toStation:   to,
		fromName:    cfg.Canary.From,
		toName:      cfg.Canary.To,
	}
}

func resolveStation(client *OEBBClient, name string, cache map[string]*Station) (*Station, bool) {
	if s, ok := cache[name]; ok {
		return s, true
	}
	s, err := client.SearchStation(name)
	if err != nil {
		log.Printf("Failed to resolve station %q: %v", name, err)
		return nil, false
	}
	log.Printf("Resolved %q → %s (#%d)", name, s.Name, s.Number)
	cache[name] = s
	return s, true
}

const consecutiveErrorThreshold = 3

func checkAll(client *OEBBClient, cfg *Config, watchList *[]watchEntry, consecutiveErrors *int, alerted *bool) bool {
	log.Printf("Checking %d route/date combination(s)...", len(*watchList))

	var remaining []watchEntry
	hadError := false

	for _, entry := range *watchList {
		connections, err := client.SearchConnections(entry.fromStation, entry.toStation, entry.date)
		if err != nil {
			log.Printf("Error checking %s → %s on %s: %v", entry.fromName, entry.toName, entry.date, err)
			remaining = append(remaining, entry)
			hadError = true
			*consecutiveErrors++
			if *consecutiveErrors >= consecutiveErrorThreshold && !*alerted {
				log.Printf("⚠ %d consecutive errors, sending alert via Telegram", *consecutiveErrors)
				if alertErr := SendTelegramError(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, *consecutiveErrors, err); alertErr != nil {
					log.Printf("Failed to send error alert: %v", alertErr)
				} else {
					*alerted = true
				}
			}
			continue
		}

		if len(connections) == 0 {
			log.Printf("  %s → %s on %s: no direct Nightjet in timetable yet", entry.fromName, entry.toName, entry.date)
			remaining = append(remaining, entry)
			continue
		}

		log.Printf("  ✅ %s → %s on %s: %d Nightjet(s) found in timetable!", entry.fromName, entry.toName, entry.date, len(connections))

		if err := SendTelegramNotification(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, connections); err != nil {
			log.Printf("  ⚠ Telegram notification failed: %v", err)
			remaining = append(remaining, entry)
			continue
		}
		log.Printf("  📨 Telegram notification sent, removing from watch list")
	}

	if !hadError {
		*consecutiveErrors = 0
		*alerted = false
	}

	*watchList = remaining
	return !hadError
}

const (
	canaryFailureThreshold = 3
	canaryWindowStartDays  = 3
	canaryWindowEndDays    = 10
)

func runCanaryCheck(client *OEBBClient, cfg *Config, entry *watchEntry, failures *int, alerted *bool) bool {
	from := time.Now().AddDate(0, 0, canaryWindowStartDays).Format("2006-01-02")
	to := time.Now().AddDate(0, 0, canaryWindowEndDays).Format("2006-01-02")
	log.Printf("Canary check: %s → %s in window %s..%s", entry.fromName, entry.toName, from, to)

	daysWithNightjet := 0
	daysChecked := 0
	apiErrors := 0
	for offset := canaryWindowStartDays; offset <= canaryWindowEndDays; offset++ {
		date := time.Now().AddDate(0, 0, offset).Format("2006-01-02")
		connections, err := client.SearchConnections(entry.fromStation, entry.toStation, date)
		if err != nil {
			apiErrors++
			continue
		}
		daysChecked++
		if len(connections) > 0 {
			daysWithNightjet++
		}
	}

	if daysChecked == 0 {
		log.Printf("  Canary: API error on all %d days — markiere API als unhealthy", apiErrors)
		return false
	}

	// Erwartung: an mind. 50% der erfolgreich abgefragten Tage findet die Detection einen NJ.
	if daysWithNightjet*2 >= daysChecked {
		log.Printf("  Canary: ✅ %d/%d days with Nightjet — detection works", daysWithNightjet, daysChecked)
		*failures = 0
		*alerted = false
		return true
	}

	*failures++
	log.Printf("  Canary: ⚠ only %d/%d days with Nightjet (%d/%d)", daysWithNightjet, daysChecked, *failures, canaryFailureThreshold)

	if *failures >= canaryFailureThreshold && !*alerted {
		msg := buildCanaryAlertText(*failures, entry.fromName, entry.toName)
		if err := sendTelegram(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, msg); err != nil {
			log.Printf("  Canary: Telegram alert failed: %v", err)
			return true
		}
		log.Printf("  Canary: 📨 Alert sent")
		*alerted = true
	}
	return true
}
