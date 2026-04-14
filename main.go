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
	canaryFailures := 0
	canaryAlerted := false

	runCheck := func() {
		checkAll(client, cfg, &watchList, &consecutiveErrors)
		runCanaryCheck(client, cfg, &watchList[0], &canaryFailures, &canaryAlerted)
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

func checkAll(client *OEBBClient, cfg *Config, watchList *[]watchEntry, consecutiveErrors *int) {
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
			if *consecutiveErrors == consecutiveErrorThreshold {
				log.Printf("⚠ %d consecutive errors, sending alert via Telegram", *consecutiveErrors)
				if alertErr := SendTelegramError(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, *consecutiveErrors, err); alertErr != nil {
					log.Printf("Failed to send error alert: %v", alertErr)
				}
			}
			continue
		}

		if len(connections) == 0 {
			log.Printf("  %s → %s on %s: not bookable yet", entry.fromName, entry.toName, entry.date)
			remaining = append(remaining, entry)
			continue
		}

		log.Printf("  ✅ %s → %s on %s: %d Nightjet(s) found!", entry.fromName, entry.toName, entry.date, len(connections))

		if err := SendTelegramNotification(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, connections); err != nil {
			log.Printf("  ⚠ Telegram notification failed: %v", err)
			remaining = append(remaining, entry)
			continue
		}
		log.Printf("  📨 Telegram notification sent, removing from watch list")
	}

	if !hadError {
		*consecutiveErrors = 0
	}

	*watchList = remaining
}

const canaryFailureThreshold = 3

func runCanaryCheck(client *OEBBClient, cfg *Config, entry *watchEntry, failures *int, alerted *bool) {
	canaryDate := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	log.Printf("Canary check: %s → %s on %s", entry.fromName, entry.toName, canaryDate)

	connections, err := client.SearchConnections(entry.fromStation, entry.toStation, canaryDate)
	if err != nil {
		log.Printf("  Canary: API error (handled separately): %v", err)
		return
	}

	if len(connections) > 0 {
		log.Printf("  Canary: ✅ %d Nightjet(s) found — detection works", len(connections))
		*failures = 0
		*alerted = false
		return
	}

	*failures++
	log.Printf("  Canary: ⚠ no Nightjet found (%d/%d)", *failures, canaryFailureThreshold)

	if *failures >= canaryFailureThreshold && !*alerted {
		msg := fmt.Sprintf("🐤 Nightjet Monitor: Canary-Alarm\n\n"+
			"Seit %d Checks findet der Monitor keinen Nightjet auf der Referenzstrecke %s → %s für nahe Termine.\n\n"+
			"Möglicherweise hat sich die ÖBB API oder die Nightjet-Erkennung geändert. Bitte prüfen.",
			*failures, entry.fromName, entry.toName)
		if err := sendTelegram(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramTopicID, msg); err != nil {
			log.Printf("  Canary: Telegram alert failed: %v", err)
			return
		}
		log.Printf("  Canary: 📨 Alert sent")
		*alerted = true
	}
}
