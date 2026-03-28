package main

import (
	"context"
	"flag"
	"log"
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

	if *once {
		checkAll(client, cfg.SlackWebhookURL, &watchList)
		return
	}

	// Scheduled mode
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run immediately on start
	checkAll(client, cfg.SlackWebhookURL, &watchList)

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
			checkAll(client, cfg.SlackWebhookURL, &watchList)
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

func checkAll(client *OEBBClient, webhookURL string, watchList *[]watchEntry) {
	log.Printf("Checking %d route/date combination(s)...", len(*watchList))

	var remaining []watchEntry

	for _, entry := range *watchList {
		connections, err := client.SearchConnections(entry.fromStation, entry.toStation, entry.date)
		if err != nil {
			log.Printf("Error checking %s → %s on %s: %v", entry.fromName, entry.toName, entry.date, err)
			remaining = append(remaining, entry)
			continue
		}

		if len(connections) == 0 {
			log.Printf("  %s → %s on %s: not bookable yet", entry.fromName, entry.toName, entry.date)
			remaining = append(remaining, entry)
			continue
		}

		log.Printf("  ✅ %s → %s on %s: %d Nightjet(s) found!", entry.fromName, entry.toName, entry.date, len(connections))

		if err := SendSlackNotification(webhookURL, connections); err != nil {
			log.Printf("  ⚠ Slack notification failed: %v", err)
			// Keep in list so we retry notification next time
			remaining = append(remaining, entry)
			continue
		}
		log.Printf("  📨 Slack notification sent, removing from watch list")
	}

	*watchList = remaining
}
