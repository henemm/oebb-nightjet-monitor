package main

import (
	"context"
	"flag"
	"fmt"
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

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	once := flag.Bool("once", false, "run check once and exit")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Loaded %d connection(s) to monitor", len(cfg.Connections))

	client := NewOEBBClient()

	// Resolve all station names to IDs upfront
	watchList := resolveStations(client, cfg)
	if len(watchList) == 0 {
		log.Fatal("No valid connections to watch after station resolution")
	}
	log.Printf("Watching %d route/date combination(s)", len(watchList))

	consecutiveErrors := 0

	runCheck := func() {
		checkAll(client, cfg.SlackWebhookURL, &watchList, &consecutiveErrors)
		if cfg.HeartbeatURL != "" {
			resp, err := http.Get(cfg.HeartbeatURL)
			if err != nil {
				log.Printf("Heartbeat ping failed: %v", err)
			} else {
				resp.Body.Close()
			}
		}
	}

	if *once {
		runCheck()
		return
	}

	// Scheduled mode
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run immediately on start
	runCheck()

	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()

	log.Printf("Scheduler started, checking every %s", cfg.CheckInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down gracefully...")
			return
		case <-ticker.C:
			if len(watchList) == 0 {
				log.Println("All connections found, nothing left to watch. Exiting.")
				return
			}
			runCheck()
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

func checkAll(client *OEBBClient, webhookURL string, watchList *[]watchEntry, consecutiveErrors *int) []RouteStatus {
	log.Printf("Checking %d route/date combination(s)...", len(*watchList))

	var remaining []watchEntry
	var statuses []RouteStatus
	hadError := false

	for _, entry := range *watchList {
		rs := RouteStatus{From: entry.fromName, To: entry.toName, Date: entry.date}

		connections, err := client.SearchConnections(entry.fromStation, entry.toStation, entry.date)
		if err != nil {
			log.Printf("Error checking %s → %s on %s: %v", entry.fromName, entry.toName, entry.date, err)
			rs.Status = fmt.Sprintf("Fehler: %v", err)
			statuses = append(statuses, rs)
			remaining = append(remaining, entry)
			hadError = true
			*consecutiveErrors++
			if *consecutiveErrors == consecutiveErrorThreshold {
				log.Printf("⚠ %d consecutive errors, sending alert to Slack", *consecutiveErrors)
				if alertErr := SendSlackError(webhookURL, *consecutiveErrors, err); alertErr != nil {
					log.Printf("Failed to send error alert: %v", alertErr)
				}
			}
			continue
		}

		if len(connections) == 0 {
			log.Printf("  %s → %s on %s: not bookable yet", entry.fromName, entry.toName, entry.date)
			rs.Status = "Noch nicht buchbar"
			statuses = append(statuses, rs)
			remaining = append(remaining, entry)
			continue
		}

		log.Printf("  ✅ %s → %s on %s: %d Nightjet(s) found!", entry.fromName, entry.toName, entry.date, len(connections))

		if err := SendSlackNotification(webhookURL, connections); err != nil {
			log.Printf("  ⚠ Slack notification failed: %v", err)
			rs.Status = "bookable"
			statuses = append(statuses, rs)
			remaining = append(remaining, entry)
			continue
		}
		log.Printf("  📨 Slack notification sent, removing from watch list")
		rs.Status = "bookable"
		statuses = append(statuses, rs)
	}

	if !hadError {
		*consecutiveErrors = 0
	}

	*watchList = remaining
	return statuses
}
