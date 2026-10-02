package main

import (
	"net/http"
	"testing"
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
