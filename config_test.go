package main

import (
	"os"
	"path/filepath"
	"testing"
)

const baseConfig = `telegram_bot_token: "tok"
telegram_chat_id: "123"
check_interval: 30m
connections:
  - from: "Amsterdam Centraal"
    to: "Salzburg Hbf"
    dates: ["2026-12-28"]
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// AC-7: ohne canary-Block gilt der Default.
func TestLoadConfig_CanaryDefault(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, baseConfig))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.Canary.From != "Wien Hbf" || cfg.Canary.To != "Innsbruck Hbf" {
		t.Errorf("Canary = %+v, erwartet Wien Hbf → Innsbruck Hbf", cfg.Canary)
	}
}

// AC-7: konfigurierte Canary-Strecke wird übernommen.
func TestLoadConfig_CanaryFromConfig(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, baseConfig+"canary:\n  from: \"Wien Hbf\"\n  to: \"Salzburg Hbf\"\n"))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.Canary.From != "Wien Hbf" || cfg.Canary.To != "Salzburg Hbf" {
		t.Errorf("Canary = %+v, erwartet Wien Hbf → Salzburg Hbf", cfg.Canary)
	}
}

// AC-7: Teilfeld leer → Default für die ganze Strecke.
func TestLoadConfig_CanaryPartialFallsBackToDefault(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, baseConfig+"canary:\n  from: \"Graz Hbf\"\n"))
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if cfg.Canary.From != "Wien Hbf" || cfg.Canary.To != "Innsbruck Hbf" {
		t.Errorf("Canary = %+v, erwartet Default Wien Hbf → Innsbruck Hbf", cfg.Canary)
	}
}

// AC-7: Pflichtfelder werden weiter validiert.
func TestLoadConfig_RequiredFieldsStillValidated(t *testing.T) {
	noTelegram := "check_interval: 30m\nconnections:\n  - from: A\n    to: B\n    dates: [\"2026-12-28\"]\n"
	noConnections := "telegram_bot_token: tok\ntelegram_chat_id: \"1\"\ncheck_interval: 30m\n"
	for name, content := range map[string]string{"ohne Telegram": noTelegram, "ohne Connections": noConnections} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(writeConfig(t, content)); err == nil {
				t.Fatal("AC-7: erwartet Fehler")
			}
		})
	}
}
