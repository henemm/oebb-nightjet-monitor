package main

import (
	"errors"
	"testing"
	"time"
)

// AC-10: ehrlicher Treffer-Text.
func TestBuildNotificationText_HonestWording(t *testing.T) {
	conns := []Connection{{
		TrainName: "NJ 40421",
		From:      "Amsterdam Centraal",
		To:        "Salzburg Hbf",
		Date:      "2026-12-28",
		Departure: time.Date(2026, 12, 28, 18, 1, 0, 0, time.UTC),
		Arrival:   time.Date(2026, 12, 29, 6, 46, 0, 0, time.UTC),
	}}
	text := buildNotificationText(conns)

	for _, want := range []string{"Direktverbindung im Fahrplan gefunden", "Buchbarkeit bitte prüfen", "https://tickets.oebb.at", "NJ 40421", "2026-12-28"} {
		if !contains(text, want) {
			t.Errorf("Text enthält %q nicht:\n%s", want, text)
		}
	}
	if contains(text, "jetzt buchbar") {
		t.Errorf("Text darf nicht \"jetzt buchbar\" behaupten:\n%s", text)
	}
}

// AC-10: Fehler- und Canary-Alarm nennen die Fahrplanauskunft.
func TestAlarmTexts_NameFahrplanauskunft(t *testing.T) {
	errText := buildErrorText(3, errors.New("boom"))
	canaryText := buildCanaryAlertText(3, "Wien Hbf", "Innsbruck Hbf")
	for name, text := range map[string]string{"Fehleralarm": errText, "Canary-Alarm": canaryText} {
		if !contains(text, "ÖBB Fahrplanauskunft") {
			t.Errorf("%s nennt \"ÖBB Fahrplanauskunft\" nicht:\n%s", name, text)
		}
		if contains(text, "ÖBB API") {
			t.Errorf("%s enthält noch \"ÖBB API\":\n%s", name, text)
		}
	}
	if !contains(canaryText, "Wien Hbf") || !contains(canaryText, "Innsbruck Hbf") {
		t.Errorf("Canary-Alarm nennt die Strecke nicht:\n%s", canaryText)
	}
}
