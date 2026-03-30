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

// RouteStatus holds the result of the last check for a single route/date.
type RouteStatus struct {
	From   string
	To     string
	Date   string
	Status string // "not bookable yet", "bookable", or error description
}

// StatusPoster updates the Slack channel topic with the current monitor status.
type StatusPoster struct {
	botToken  string
	channelID string
}

func NewStatusPoster(botToken, channelID string) *StatusPoster {
	return &StatusPoster{botToken: botToken, channelID: channelID}
}

func (sp *StatusPoster) Enabled() bool {
	return sp.botToken != "" && sp.channelID != ""
}

func (sp *StatusPoster) UpdateTopic(routes []RouteStatus, lastCheck time.Time) error {
	topic := fmt.Sprintf(":white_check_mark: Letzter Check: %s | %d Route(n) überwacht",
		lastCheck.Format("02.01. 15:04"), len(routes))

	body, _ := json.Marshal(map[string]string{
		"channel": sp.channelID,
		"topic":   topic,
	})

	req, err := http.NewRequest("POST", "https://slack.com/api/conversations.setTopic", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating setTopic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sp.botToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("setTopic request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding setTopic response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("setTopic error: %s", result.Error)
	}
	return nil
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
