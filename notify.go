package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func SendSignalNotification(phone, apiKey string, connections []Connection) error {
	if len(connections) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🚂 Nightjet jetzt buchbar!\n\n")

	for _, c := range connections {
		sb.WriteString(fmt.Sprintf("%s: %s → %s\n", c.TrainName, c.From, c.To))
		sb.WriteString(fmt.Sprintf("📅 %s\n", c.Date))
		sb.WriteString(fmt.Sprintf("🕐 Abfahrt: %s — Ankunft: %s\n",
			c.Departure.Format("15:04"),
			c.Arrival.Format("15:04")))
		sb.WriteString(fmt.Sprintf("🔗 https://tickets.oebb.at\n\n"))
	}

	return sendSignal(phone, apiKey, sb.String())
}

func SendSignalError(phone, apiKey string, errCount int, lastErr error) error {
	msg := fmt.Sprintf("⚠️ Nightjet Monitor: API-Fehler\n\n"+
		"Die ÖBB API ist %dx hintereinander fehlgeschlagen.\n"+
		"Letzter Fehler: %s\n\n"+
		"Möglicherweise hat ÖBB die API geändert. Bitte prüfen.",
		errCount, lastErr)
	return sendSignal(phone, apiKey, msg)
}

func sendSignal(phone, apiKey, text string) error {
	apiURL := fmt.Sprintf("https://signal.callmebot.com/signal/send.php?phone=%s&apikey=%s&text=%s",
		url.QueryEscape(phone),
		url.QueryEscape(apiKey),
		url.QueryEscape(text))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return fmt.Errorf("sending signal notification: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signal API returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
