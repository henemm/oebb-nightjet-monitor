package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// telegramBaseURL ist die Basis-URL der Telegram Bot API (in Tests überschreibbar).
var telegramBaseURL = "https://api.telegram.org"

type telegramMessage struct {
	ChatID          string `json:"chat_id"`
	Text            string `json:"text"`
	MessageThreadID int    `json:"message_thread_id,omitempty"`
}

// buildNotificationText baut den Treffer-Text. Ein Fahrplan-Treffer ist kein
// Buchbarkeits-Nachweis — der Text sagt das ehrlich.
func buildNotificationText(connections []Connection) string {
	var sb strings.Builder
	sb.WriteString("🚂 Direktverbindung im Fahrplan gefunden — Buchbarkeit bitte prüfen\n\n")

	for _, c := range connections {
		sb.WriteString(fmt.Sprintf("%s: %s → %s\n", c.TrainName, c.From, c.To))
		sb.WriteString(fmt.Sprintf("📅 %s\n", c.Date))
		sb.WriteString(fmt.Sprintf("🕐 Abfahrt: %s — Ankunft: %s\n",
			c.Departure.Format("15:04"),
			c.Arrival.Format("15:04")))
		sb.WriteString("🔗 https://tickets.oebb.at\n\n")
	}
	return sb.String()
}

func buildErrorText(errCount int, lastErr error) string {
	return fmt.Sprintf("⚠️ Nightjet Monitor: Fahrplan-Fehler\n\n"+
		"Die ÖBB Fahrplanauskunft ist %dx hintereinander fehlgeschlagen.\n"+
		"Letzter Fehler: %s\n\n"+
		"Möglicherweise hat ÖBB die Schnittstelle geändert. Bitte prüfen.",
		errCount, lastErr)
}

func buildCanaryAlertText(failures int, fromName, toName string) string {
	return fmt.Sprintf("🐤 Nightjet Monitor: Canary-Alarm\n\n"+
		"Seit %d Checks findet der Monitor an weniger als der Hälfte der Tage einen Nightjet auf der Referenzstrecke %s → %s (Fenster heute+%d bis heute+%d Tage).\n\n"+
		"Möglicherweise hat sich die ÖBB Fahrplanauskunft oder die Nightjet-Erkennung geändert. Bitte prüfen.",
		failures, fromName, toName, canaryWindowStartDays, canaryWindowEndDays)
}

func SendTelegramNotification(botToken, chatID string, topicID int, connections []Connection) error {
	if len(connections) == 0 {
		return nil
	}
	return sendTelegram(botToken, chatID, topicID, buildNotificationText(connections))
}

func SendTelegramError(botToken, chatID string, topicID int, errCount int, lastErr error) error {
	return sendTelegram(botToken, chatID, topicID, buildErrorText(errCount, lastErr))
}

func sendTelegram(botToken, chatID string, topicID int, text string) error {
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", telegramBaseURL, botToken)

	msg := telegramMessage{
		ChatID:          chatID,
		Text:            text,
		MessageThreadID: topicID,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling telegram message: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending telegram notification: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
