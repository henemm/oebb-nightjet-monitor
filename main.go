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
	notified             *notifiedStore // nil, solange die Notified-Datei nicht ladbar ist
}

// newMonitor löst Watchlist- und Canary-Stationen tolerant auf. Schlägt die
// Auflösung fehl (HAFAS kurz down), wird in jedem Zyklus erneut versucht.
func newMonitor(client *OEBBClient, cfg *Config) *monitor {
	m := &monitor{client: client, cfg: cfg}
	if cfg.NotifiedFile == "" {
		log.Printf("⚠ notified_file nicht gesetzt — gemeldete Treffer nur im Arbeitsspeicher (Neustart = erneute Meldung)")
	}
	m.ensureNotified()

	// Initialisiert = Stationen aufgelöst, auch wenn danach alles gemeldet ist.
	if resolved := resolveStations(client, cfg); len(resolved) > 0 {
		m.watchListInitialized = true
		m.watchList = m.filterNotified(resolved)
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
		resolved := resolveStations(m.client, m.cfg)
		if len(resolved) == 0 {
			log.Printf("⚠ Station resolution still failing — skipping cycle (no heartbeat ping)")
			return
		}
		m.watchListInitialized = true
		m.watchList = m.filterNotified(resolved)
		log.Printf("Resolved %d route/date combination(s) on retry", len(m.watchList))
	}

	checkOK := m.runWatchCheck()

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
		log.Printf("Success report skipped: cycle not successful (checkOK=%v, canaryOK=%v) — Monitoring soll Alarm schlagen", checkOK, canaryAPIOK)
	}
}

// runWatchCheck prüft die Watchlist. Ohne geladenen Notified-Store keine Prüfung
// (Schutz vor Doppelmeldung) und kein Erfolg.
func (m *monitor) runWatchCheck() bool {
	if !m.ensureNotified() {
		log.Printf("⚠ Notified-Datei nicht ladbar — keine Watch-Prüfung in diesem Zyklus (kein Erfolg)")
		return false
	}
	// Ungespeicherte Treffer: Speichern erneut versuchen, bis es klappt (sonst kein Erfolg).
	saveOK := true
	if m.notified.dirty {
		if err := m.notified.save(); err != nil {
			log.Printf("⚠ Notified-Datei weiterhin nicht gespeichert: %v (kein Erfolg)", err)
			saveOK = false
		}
	}
	// Filtern auch hier: der Store kann erst nach dem Aufbau der Watchlist ladbar geworden sein.
	m.watchList = m.filterNotified(m.watchList)
	if len(m.watchList) == 0 {
		return saveOK
	}
	return checkAll(m.client, m.cfg, m.notified, &m.watchList, &m.consecutiveErrors, &m.errorAlerted) && saveOK
}

// ensureNotified lädt den Notified-Store, falls noch nicht geschehen.
func (m *monitor) ensureNotified() bool {
	if m.notified != nil {
		return true
	}
	s, err := loadNotified(m.cfg.NotifiedFile)
	if err != nil {
		log.Printf("⚠ Notified-Datei %q nicht ladbar: %v", m.cfg.NotifiedFile, err)
		return false
	}
	m.notified = s
	return true
}

// filterNotified entfernt bereits gemeldete Einträge (Schlüssel = Config-Namen + Datum).
// Ohne geladenen Store bleibt die Liste unverändert.
func (m *monitor) filterNotified(list []watchEntry) []watchEntry {
	if m.notified == nil {
		return list
	}
	var kept []watchEntry
	for _, e := range list {
		if !m.notified.contains(e.fromName, e.toName, e.date) {
			kept = append(kept, e)
		}
	}
	return kept
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

func checkAll(client *OEBBClient, cfg *Config, store *notifiedStore, watchList *[]watchEntry, consecutiveErrors *int, alerted *bool) bool {
	log.Printf("Checking %d route/date combination(s)...", len(*watchList))

	var remaining []watchEntry
	hadError := false
	deliveryOK := true

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

		sent, ok := notifyHit(cfg, store, entry, connections)
		if !sent {
			remaining = append(remaining, entry)
		}
		if !ok {
			deliveryOK = false
		}
	}

	if !hadError {
		*consecutiveErrors = 0
		*alerted = false
	}

	*watchList = remaining
	return !hadError && deliveryOK
}

// notifyHit meldet einen Treffer und merkt ihn sich. sent: Eintrag verlässt die
// Watchlist; ok: Versand und Speichern haben geklappt.
func notifyHit(cfg *Config, store *notifiedStore, entry watchEntry, connections []Connection) (sent, ok bool) {
	if store.contains(entry.fromName, entry.toName, entry.date) {
		return true, true // doppelter Config-Eintrag, im selben Zyklus schon gemeldet
	}
	if err := SendTelegramNotification(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, connections); err != nil {
		log.Printf("  ⚠ Telegram notification failed: %v", err)
		return false, false
	}
	log.Printf("  📨 Telegram notification sent, removing from watch list")
	store.add(entry.fromName, entry.toName, entry.date, time.Now())
	if err := store.save(); err != nil {
		log.Printf("  ⚠ Notified-Datei nicht gespeichert: %v — nach Neustart droht erneute Meldung", err)
		return true, false
	}
	return true, true
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
			return false
		}
		log.Printf("  Canary: 📨 Alert sent")
		*alerted = true
	}
	return true
}
