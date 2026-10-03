package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Zyklus-Tests gegen Fake-HAFAS, Fake-Heartbeat und Fake-Telegram.
// Erwartete Schnittstelle (Spec Abschnitt 5/6):
//   - newMonitor(client, cfg) *monitor   — löst Stationen tolerant auf, wirft bei Ausfall keinen Fehler
//   - (m *monitor) runCheck()            — ein Zyklus inkl. Canary und Heartbeat
//   - m.watchList                        — verbleibende Beobachtungen
//   - telegramBaseURL (Variable)         — Telegram-Basis-URL, in Tests umbiegbar

const (
	watchFrom  = "Amsterdam Centraal"
	watchTo    = "Salzburg Hbf"
	canaryFrom = "Wien Hbf"
	canaryTo   = "Innsbruck Hbf"
)

func testConfig(heartbeatURL string) *Config {
	return &Config{
		TelegramBotToken: "tok",
		TelegramChatID:   "1",
		HeartbeatURL:     heartbeatURL,
		Connections:      []ConnectionConfig{{From: watchFrom, To: watchTo, Dates: []string{"2026-12-28"}}},
		Canary:           CanaryConfig{From: canaryFrom, To: canaryTo},
	}
}

func useFakeTelegram(t *testing.T) *countingServer {
	t.Helper()
	tg := newCountingServer(t)
	old := telegramBaseURL
	telegramBaseURL = tg.srv.URL
	t.Cleanup(func() { telegramBaseURL = old })
	return tg
}

// canaryNightjets: auf der Canary-Strecke fährt an jedem Tag ein Nightjet; sonst H890.
func canaryNightjets(c hafasCall) (int, string) {
	if c.Dep == lidFor("8103000") && c.Arr == lidFor("8100108") {
		return http.StatusOK, tripJSON(njConn(c.Date, "200000", "01080000"))
	}
	return http.StatusOK, tripH890JSON()
}

// AC-8: Der Canary fragt seine eigene Strecke ab, nicht watchList[0].
func TestCanary_UsesOwnRoute_NotFirstWatchEntry(t *testing.T) {
	useFakeTelegram(t)
	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = canaryNightjets

	m := newMonitor(f.client(), testConfig(hb.srv.URL))
	m.runCheck()

	var canaryCalls, watchCalls int
	for _, c := range f.tripCalls() {
		switch {
		case c.Dep == lidFor("8103000") && c.Arr == lidFor("8100108"):
			canaryCalls++
		case c.Dep == lidFor("8400058") && c.Arr == lidFor("8100002"):
			watchCalls++
		default:
			t.Errorf("unerwartete Strecke abgefragt: %+v", c)
		}
	}
	if canaryCalls != 8 { // Fenster heute+3 .. heute+10
		t.Errorf("AC-8: Canary-Abfragen auf Canary-Strecke = %d, erwartet 8", canaryCalls)
	}
	if watchCalls != 1 { // genau die eine Watchlist-Abfrage, kein Canary auf der Zielstrecke
		t.Errorf("AC-8: Abfragen auf Zielstrecke = %d, erwartet 1", watchCalls)
	}
}

// AC-8 + AC-9: Canary-Stationen werden bei Fehlschlag im nächsten Zyklus erneut aufgelöst;
// bis dahin kein Heartbeat.
func TestCanary_LazyResolve_RetriesAndNoHeartbeatUntilResolved(t *testing.T) {
	useFakeTelegram(t)
	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = canaryNightjets
	delete(f.stations, canaryFrom) // Auflösung schlägt fehl

	m := newMonitor(f.client(), testConfig(hb.srv.URL))
	m.runCheck()
	if hb.count() != 0 {
		t.Fatalf("AC-9: Heartbeat trotz nicht aufgelöster Canary-Station (%d Pings)", hb.count())
	}

	f.stations[canaryFrom] = "8103000" // HAFAS wieder normal
	m.runCheck()
	if hb.count() != 1 {
		t.Fatalf("AC-8/AC-9: nach erneuter Auflösung genau 1 Heartbeat erwartet, bekam %d", hb.count())
	}
}

// AC-9: fehlerfreier Zyklus pingt genau einmal.
func TestHeartbeat_PingedOnceOnHealthyCycle(t *testing.T) {
	useFakeTelegram(t)
	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = canaryNightjets

	newMonitor(f.client(), testConfig(hb.srv.URL)).runCheck()
	if hb.count() != 1 {
		t.Fatalf("AC-9: erwartet genau 1 Heartbeat, bekam %d", hb.count())
	}
}

// AC-9: Fehler bei der Watchlist-Abfrage → kein Heartbeat.
func TestHeartbeat_SkippedOnWatchlistError(t *testing.T) {
	useFakeTelegram(t)
	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) {
		if c.Dep == lidFor("8400058") {
			return http.StatusServiceUnavailable, "down"
		}
		return canaryNightjets(c)
	}

	newMonitor(f.client(), testConfig(hb.srv.URL)).runCheck()
	if hb.count() != 0 {
		t.Fatalf("AC-9: Heartbeat trotz Watchlist-Fehler (%d Pings)", hb.count())
	}
}

// AC-9: Canary-API komplett down → kein Heartbeat (Regression des 4-Tage-Ausfalls).
func TestHeartbeat_SkippedWhenCanaryAPIDown(t *testing.T) {
	useFakeTelegram(t)
	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) { return http.StatusForbidden, "gesperrt" }

	newMonitor(f.client(), testConfig(hb.srv.URL)).runCheck()
	if hb.count() != 0 {
		t.Fatalf("AC-9: Heartbeat trotz ausgefallener API (%d Pings)", hb.count())
	}
}

// Treffer auf der Zielstrecke: genau eine Telegram-Meldung, Eintrag verlässt die Watchlist.
func TestWatchHit_SendsTelegramAndRemovesEntry(t *testing.T) {
	tg := useFakeTelegram(t)
	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) {
		if c.Dep == lidFor("8400058") {
			return http.StatusOK, tripJSON(testConn{
				Date: c.Date, DepS: "180100", ArrS: "01064600",
				Name: "NJ 40421", Num: "40421", CatOutS: "NJ", CatOutL: "nightjet",
			})
		}
		return canaryNightjets(c)
	}

	m := newMonitor(f.client(), testConfig(hb.srv.URL))
	m.runCheck()
	if tg.count() != 1 {
		t.Errorf("erwartet 1 Telegram-Meldung, bekam %d", tg.count())
	}
	if len(m.watchList) != 0 {
		t.Errorf("Eintrag muss nach erfolgreicher Meldung entfernt sein, watchList = %+v", m.watchList)
	}
	if hb.count() != 1 {
		t.Errorf("erwartet 1 Heartbeat, bekam %d", hb.count())
	}
}

// Telegram-Fehler: Eintrag bleibt bestehen (Spec Abschnitt 6).
func TestWatchHit_TelegramFailureKeepsEntry(t *testing.T) {
	failing := newCountingServer(t)
	failing.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	old := telegramBaseURL
	telegramBaseURL = failing.srv.URL
	t.Cleanup(func() { telegramBaseURL = old })

	hb := newCountingServer(t)
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) {
		if c.Dep == lidFor("8400058") {
			return http.StatusOK, tripJSON(njConn(c.Date, "180100", "01064600"))
		}
		return canaryNightjets(c)
	}

	m := newMonitor(f.client(), testConfig(hb.srv.URL))
	m.runCheck()
	if len(m.watchList) != 1 {
		t.Errorf("Eintrag muss bei Telegram-Fehler bleiben, watchList = %+v", m.watchList)
	}
}

// Erfolgsdatei: wird nur bei fachlich erfolgreichem Zyklus geschrieben (Readiness),
// der Server-Monitor prüft ihr Alter.
func TestSuccessFile_WrittenOnlyOnHealthyCycle(t *testing.T) {
	useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) { return http.StatusForbidden, "gesperrt" }

	path := filepath.Join(t.TempDir(), "nightjet.success")
	cfg := testConfig("")
	cfg.SuccessFile = path

	newMonitor(f.client(), cfg).runCheck()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("Erfolgsdatei trotz ausgefallener API geschrieben")
	}

	f.trip = canaryNightjets
	newMonitor(f.client(), cfg).runCheck()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Erfolgsdatei nach gesundem Zyklus erwartet: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err != nil {
		t.Errorf("Erfolgsdatei enthält keinen RFC3339-Zeitstempel: %q", data)
	}
}

// --- treffer-einmalig: Treffer genau einmal melden, Zustellfehler sichtbar ---

// newStatusServer zählt Aufrufe und antwortet immer mit dem angegebenen Status.
func newStatusServer(t *testing.T, status int) *countingServer {
	t.Helper()
	c := newCountingServer(t)
	c.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
		http.Error(w, `{"ok":false}`, status)
	})
	return c
}

func useTelegramServer(t *testing.T, c *countingServer) {
	t.Helper()
	old := telegramBaseURL
	telegramBaseURL = c.srv.URL
	t.Cleanup(func() { telegramBaseURL = old })
}

// watchHit: Nightjet auf der Watch-Strecke, Canary gesund.
func watchHit(c hafasCall) (int, string) {
	if c.Dep == lidFor("8400058") {
		return http.StatusOK, tripJSON(njConn(c.Date, "180100", "01064600"))
	}
	return canaryNightjets(c)
}

// trefferConfig: Config mit Notified- und Erfolgsdatei in einem Temp-Verzeichnis.
func trefferConfig(t *testing.T) (*Config, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := testConfig("")
	cfg.NotifiedFile = filepath.Join(dir, "nightjet.notified")
	cfg.SuccessFile = filepath.Join(dir, "nightjet.success")
	return cfg, cfg.NotifiedFile, cfg.SuccessFile
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// AC-1: Ein Treffer wird über einen Neustart (neuer Monitor, gleiche Datei) nur einmal gemeldet.
func TestTreffer_NotifiedOnceAcrossRestart(t *testing.T) {
	tg := useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg, _, _ := trefferConfig(t)

	newMonitor(f.client(), cfg).runCheck()
	restarted := newMonitor(f.client(), cfg)
	restarted.runCheck()

	if tg.count() != 1 {
		t.Errorf("AC-1: erwartet genau 1 Treffermeldung über Neustart, bekam %d", tg.count())
	}
	if len(restarted.watchList) != 0 {
		t.Errorf("AC-1: Watchlist nach Neustart muss leer sein, ist %+v", restarted.watchList)
	}
}

// AC-2: Die Notified-Datei enthält den Treffer mit Config-Namen und RFC3339-Zeitstempel.
func TestTreffer_NotifiedFileContent(t *testing.T) {
	useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg, notifiedPath, _ := trefferConfig(t)

	newMonitor(f.client(), cfg).runCheck()

	data, err := os.ReadFile(notifiedPath)
	if err != nil {
		t.Fatalf("AC-2: Notified-Datei fehlt: %v", err)
	}
	var entries []struct {
		From       string `json:"from"`
		To         string `json:"to"`
		Date       string `json:"date"`
		NotifiedAt string `json:"notified_at"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("AC-2: kein gültiges JSON: %v (%q)", err, data)
	}
	if len(entries) != 1 {
		t.Fatalf("AC-2: erwartet 1 Eintrag, bekam %d", len(entries))
	}
	e := entries[0]
	if e.From != watchFrom || e.To != watchTo || e.Date != "2026-12-28" {
		t.Errorf("AC-2: Eintrag = %+v, erwartet Config-Werte", e)
	}
	if _, err := time.Parse(time.RFC3339, e.NotifiedAt); err != nil {
		t.Errorf("AC-2: notified_at %q ist kein RFC3339", e.NotifiedAt)
	}
}

// AC-3: Alles bereits gemeldet → keine Meldung, aber Erfolg und Watchlist gilt als initialisiert.
func TestTreffer_AllNotifiedStillHealthy(t *testing.T) {
	tg := useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg, notifiedPath, successPath := trefferConfig(t)
	seed := `[{"from":"` + watchFrom + `","to":"` + watchTo + `","date":"2026-12-28","notified_at":"2026-10-01T10:00:00Z"}]`
	if err := os.WriteFile(notifiedPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newMonitor(f.client(), cfg)
	m.runCheck()

	if tg.count() != 0 {
		t.Errorf("AC-3: bereits gemeldeter Treffer erneut gesendet (%d)", tg.count())
	}
	if !m.watchListInitialized {
		t.Error("AC-3: watchListInitialized muss true sein, auch wenn alles gemeldet ist")
	}
	if !fileExists(successPath) {
		t.Error("AC-3: Erfolgsdatei fehlt trotz gesundem Zyklus")
	}
}

// AC-4: Telegram 401 → Eintrag bleibt, kein Erfolg, nichts gespeichert.
func TestTreffer_TelegramFailureNoSuccess(t *testing.T) {
	useTelegramServer(t, newStatusServer(t, http.StatusUnauthorized))
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg, notifiedPath, successPath := trefferConfig(t)

	m := newMonitor(f.client(), cfg)
	m.runCheck()

	if len(m.watchList) != 1 {
		t.Errorf("AC-4: Eintrag muss bei Zustellfehler bleiben, watchList = %+v", m.watchList)
	}
	if fileExists(successPath) {
		t.Error("AC-4: Erfolgsdatei trotz Telegram-Zustellfehler geschrieben")
	}
	if s, err := loadNotified(notifiedPath); err == nil && s.contains(watchFrom, watchTo, "2026-12-28") {
		t.Error("AC-4: nicht zugestellter Treffer als gemeldet gespeichert")
	}
}

// AC-5: Zugestellt, aber Speichern scheitert → Eintrag verlässt Watchlist, kein Erfolg.
func TestTreffer_SaveFailureNoSuccess(t *testing.T) {
	tg := useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg, _, successPath := trefferConfig(t)
	cfg.NotifiedFile = filepath.Join(t.TempDir(), "gibt-es-nicht", "nightjet.notified")

	m := newMonitor(f.client(), cfg)
	m.runCheck()

	if tg.count() != 1 {
		t.Fatalf("AC-5: erwartet 1 Treffermeldung, bekam %d", tg.count())
	}
	if len(m.watchList) != 0 {
		t.Errorf("AC-5: zugestellter Eintrag muss die Watchlist verlassen, watchList = %+v", m.watchList)
	}
	if fileExists(successPath) {
		t.Error("AC-5: Erfolgsdatei trotz Speicherfehler geschrieben")
	}
}

// AC-6: Kaputte Notified-Datei → keine Meldung, kein Erfolg; nach Reparatur normaler Betrieb.
func TestTreffer_CorruptNotifiedFileBlocksUntilFixed(t *testing.T) {
	tg := useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg, notifiedPath, successPath := trefferConfig(t)
	if err := os.WriteFile(notifiedPath, []byte("{kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newMonitor(f.client(), cfg)
	m.runCheck()
	if tg.count() != 0 {
		t.Errorf("AC-6: Treffer trotz unlesbarer Notified-Datei gesendet (%d)", tg.count())
	}
	if fileExists(successPath) {
		t.Error("AC-6: Erfolgsdatei trotz unlesbarer Notified-Datei geschrieben")
	}

	if err := os.WriteFile(notifiedPath, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.runCheck()
	if tg.count() != 1 {
		t.Errorf("AC-6: nach Reparatur erwartet 1 Treffermeldung, bekam %d", tg.count())
	}
	if !fileExists(successPath) {
		t.Error("AC-6: nach Reparatur Erfolgsdatei erwartet")
	}
}

// AC-7: Canary-Alarm scheitert → kein Erfolg, im nächsten Zyklus neuer Versuch.
func TestTreffer_CanaryAlertFailureNoSuccess(t *testing.T) {
	tg := newStatusServer(t, http.StatusInternalServerError)
	useTelegramServer(t, tg)
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) { return http.StatusOK, tripH890JSON() } // nirgends ein Nightjet
	cfg, _, successPath := trefferConfig(t)

	m := newMonitor(f.client(), cfg)
	for i := 1; i < canaryFailureThreshold; i++ {
		m.runCheck()
	}
	os.Remove(successPath)

	m.runCheck() // Schwelle erreicht, Alarm scheitert
	if tg.count() != 1 {
		t.Fatalf("AC-7: erwartet 1 Alarmversuch, bekam %d", tg.count())
	}
	if fileExists(successPath) {
		t.Error("AC-7: Erfolgsdatei trotz gescheitertem Canary-Alarm geschrieben")
	}

	m.runCheck()
	if tg.count() != 2 {
		t.Errorf("AC-7: im nächsten Zyklus erneuter Alarmversuch erwartet, Versuche = %d", tg.count())
	}
}

// AC-9: Ohne notified_file merkt sich derselbe Monitor Treffer im Arbeitsspeicher.
func TestTreffer_InMemoryWithoutNotifiedFile(t *testing.T) {
	tg := useFakeTelegram(t)
	f := newFakeHafas(t)
	f.trip = watchHit
	cfg := testConfig("")

	m := newMonitor(f.client(), cfg)
	m.runCheck()
	m.runCheck()
	if tg.count() != 1 {
		t.Errorf("AC-9: erwartet 1 Treffermeldung, bekam %d", tg.count())
	}
}
