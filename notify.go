package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type slackMessage struct {
	Text string `json:"text"`
}

func SendSlackNotification(webhookURL string, connections []Connection) error {
	if len(connections) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🚂 *Nightjet jetzt buchbar!*\n\n")

	for _, c := range connections {
		sb.WriteString(fmt.Sprintf("*%s*: %s → %s\n", c.TrainName, c.From, c.To))
		sb.WriteString(fmt.Sprintf("📅 %s\n", c.Date))
		sb.WriteString(fmt.Sprintf("🕐 Abfahrt: %s — Ankunft: %s\n",
			c.Departure.Format("15:04"),
			c.Arrival.Format("15:04")))
		sb.WriteString(fmt.Sprintf("🔗 <https://tickets.oebb.at|Jetzt buchen>\n\n"))
	}

	return sendSlack(webhookURL, sb.String())
}

func SendSlackError(webhookURL string, errCount int, lastErr error) error {
	msg := fmt.Sprintf("⚠️ *Nightjet Monitor: API-Fehler*\n\n"+
		"Die ÖBB API ist %dx hintereinander fehlgeschlagen.\n"+
		"Letzter Fehler: `%s`\n\n"+
		"Möglicherweise hat ÖBB die API geändert. Bitte prüfen.",
		errCount, lastErr)
	return sendSlack(webhookURL, msg)
}

func sendSlack(webhookURL, text string) error {
	msg := slackMessage{Text: text}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling slack message: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}

	return nil
}
