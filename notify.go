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

type telegramMessage struct {
	ChatID          string `json:"chat_id"`
	Text            string `json:"text"`
	MessageThreadID int    `json:"message_thread_id,omitempty"`
}

func SendTelegramNotification(botToken, chatID string, topicID int, connections []Connection) error {
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

	return sendTelegram(botToken, chatID, topicID, sb.String())
}

func SendTelegramError(botToken, chatID string, topicID int, errCount int, lastErr error) error {
	msg := fmt.Sprintf("⚠️ Nightjet Monitor: API-Fehler\n\n"+
		"Die ÖBB API ist %dx hintereinander fehlgeschlagen.\n"+
		"Letzter Fehler: %s\n\n"+
		"Möglicherweise hat ÖBB die API geändert. Bitte prüfen.",
		errCount, lastErr)
	return sendTelegram(botToken, chatID, topicID, msg)
}

func sendTelegram(botToken, chatID string, topicID int, text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

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
