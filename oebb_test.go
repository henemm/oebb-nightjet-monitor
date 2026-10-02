package main

import (
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

var (
	amsterdam = &Station{Number: 8400058, Name: "Amsterdam Centraal"}
	salzburg  = &Station{Number: 8100002, Name: "Salzburg Hbf"}
)

func vienna(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatalf("Europe/Vienna nicht ladbar: %v", err)
	}
	return loc
}

// AC-1: aufgezeichnete echte HAFAS-Antwort (NJ 40421, 07.–12.12.2026).
// AC-2: Folgetage werden herausgefiltert.
func TestSearchConnections_RealRecording_OnlyRequestedDay(t *testing.T) {
	fixture, err := os.ReadFile("testdata/tripsearch_ams_sbg_2026-12-07.json")
	if err != nil {
		t.Fatalf("Fixture fehlt: %v", err)
	}
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) { return http.StatusOK, string(fixture) }

	conns, err := f.client().SearchConnections(amsterdam, salzburg, "2026-12-07")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("AC-1/AC-2: erwartet genau 1 Connection (nur 07.12.), bekam %d: %+v", len(conns), conns)
	}
	c := conns[0]
	if c.TrainName != "NJ 40421" {
		t.Errorf("TrainName = %q, erwartet %q", c.TrainName, "NJ 40421")
	}
	loc := vienna(t)
	wantDep := time.Date(2026, 12, 7, 18, 1, 0, 0, loc)
	wantArr := time.Date(2026, 12, 8, 6, 46, 0, 0, loc)
	if !c.Departure.Equal(wantDep) {
		t.Errorf("Departure = %v, erwartet %v", c.Departure, wantDep)
	}
	if !c.Arrival.Equal(wantArr) {
		t.Errorf("Arrival = %v, erwartet %v", c.Arrival, wantArr)
	}
	if c.Date != "2026-12-07" {
		t.Errorf("Date = %q, erwartet 2026-12-07", c.Date)
	}

	calls := f.tripCalls()
	if len(calls) != 1 {
		t.Fatalf("erwartet 1 TripSearch-Aufruf, bekam %d", len(calls))
	}
	if calls[0].Date != "20261207" || calls[0].Dep != lidFor("8400058") || calls[0].Arr != lidFor("8100002") {
		t.Errorf("TripSearch-Request falsch: %+v", calls[0])
	}
}

// AC-2 (synthetisch): nur die Verbindung vom gesuchten Tag bleibt übrig.
func TestSearchConnections_FiltersFollowingDays(t *testing.T) {
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) {
		return http.StatusOK, tripJSON(
			njConn("20261207", "180100", "01064600"),
			njConn("20261208", "180100", "01064600"),
			njConn("20261209", "180100", "01064600"),
		)
	}
	conns, err := f.client().SearchConnections(amsterdam, salzburg, "2026-12-08")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(conns) != 1 || conns[0].Departure.Day() != 8 {
		t.Fatalf("erwartet genau die Verbindung vom 08.12., bekam %+v", conns)
	}
}

// AC-3: H890 = keine Verbindung → leer, kein Fehler.
func TestSearchConnections_H890IsEmptyNotError(t *testing.T) {
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) { return http.StatusOK, tripH890JSON() }

	conns, err := f.client().SearchConnections(amsterdam, salzburg, "2026-12-28")
	if err != nil {
		t.Fatalf("H890 darf kein Fehler sein, bekam: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("erwartet leeres Ergebnis, bekam %+v", conns)
	}
}

// AC-4: Fehlerfälle liefern einen Fehler.
func TestSearchConnections_Errors(t *testing.T) {
	cases := map[string]func(c hafasCall) (int, string){
		"HTTP 403": func(hafasCall) (int, string) { return http.StatusForbidden, "Zugriff eingeschränkt" },
		"HTTP 503": func(hafasCall) (int, string) { return http.StatusServiceUnavailable, "down" },
		"Top-Level err": func(hafasCall) (int, string) {
			return http.StatusOK, `{"err":"AUTH","errTxt":"unknown aid","svcResL":[]}`
		},
		"svcResL err": func(hafasCall) (int, string) {
			return http.StatusOK, `{"err":"OK","svcResL":[{"meth":"TripSearch","err":"H9360","errTxt":"Date outside of timetable period"}]}`
		},
		"kaputtes JSON": func(hafasCall) (int, string) { return http.StatusOK, "<html>kein json</html>" },
		"svcResL fehlt": func(hafasCall) (int, string) { return http.StatusOK, `{"err":"OK"}` },
	}
	for name, trip := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeHafas(t)
			f.trip = trip
			conns, err := f.client().SearchConnections(amsterdam, salzburg, "2026-12-07")
			if err == nil {
				t.Fatalf("AC-4: erwartet Fehler, bekam nil (Ergebnis %+v)", conns)
			}
		})
	}
}

// AC-5: Nicht-Nightjet-Produkt wird ignoriert.
func TestSearchConnections_IgnoresNonNightjet(t *testing.T) {
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) {
		return http.StatusOK, tripJSON(testConn{
			Date: "20261207", DepS: "080000", ArrS: "130000",
			Name: "RJX 63", Num: "63", CatOutS: "RJX", CatOutL: "railjet xpress",
		})
	}
	conns, err := f.client().SearchConnections(amsterdam, salzburg, "2026-12-07")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("AC-5: erwartet leeres Ergebnis, bekam %+v", conns)
	}
}

// Nightjet wird auch über catOutL erkannt.
func TestSearchConnections_DetectsNightjetByLongName(t *testing.T) {
	f := newFakeHafas(t)
	f.trip = func(c hafasCall) (int, string) {
		return http.StatusOK, tripJSON(testConn{
			Date: "20261207", DepS: "200000", ArrS: "01080000",
			Name: "NJ 233", Num: "233", CatOutS: "", CatOutL: "nightjet",
		})
	}
	conns, err := f.client().SearchConnections(amsterdam, salzburg, "2026-12-07")
	if err != nil || len(conns) != 1 {
		t.Fatalf("erwartet 1 Nightjet via catOutL, bekam %+v / %v", conns, err)
	}
}

// AC-6: Stationssuche.
func TestSearchStation_Resolves(t *testing.T) {
	f := newFakeHafas(t)
	s, err := f.client().SearchStation("Amsterdam Centraal")
	if err != nil {
		t.Fatalf("unerwarteter Fehler: %v", err)
	}
	if s.Number != 8400058 || s.Name != "Amsterdam Centraal" {
		t.Errorf("Station = %+v, erwartet {8400058 Amsterdam Centraal}", s)
	}
}

func TestSearchStation_EmptyResultIsError(t *testing.T) {
	f := newFakeHafas(t)
	if _, err := f.client().SearchStation("Gibt Es Nicht"); err == nil {
		t.Fatal("AC-6: erwartet Fehler bei leerer locL")
	}
}

func TestSearchStation_HTTPErrorIsError(t *testing.T) {
	f := newFakeHafas(t)
	f.locFail = func(string) (int, string) { return http.StatusForbidden, "gesperrt" }
	if _, err := f.client().SearchStation("Amsterdam Centraal"); err == nil {
		t.Fatal("AC-4: erwartet Fehler bei HTTP 403")
	}
}

// Der Request trägt Envelope-Pflichtfelder (AID) und den Direkt-Filter.
func TestRequestEnvelope(t *testing.T) {
	var gotBody string
	var gotMethod string
	f := newFakeHafas(t)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotMethod = string(b), r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tripH890JSON()))
	})
	_, _ = f.client().SearchConnections(amsterdam, salzburg, "2026-12-28")
	if gotMethod != http.MethodPost {
		t.Errorf("Methode = %q, erwartet POST", gotMethod)
	}
	for _, want := range []string{`"aid":"OWDL4fE4ixNiPBBm"`, `"maxChg":0`, `"value":"2762"`, `"outDate":"20261228"`, `"TripSearch"`} {
		if !contains(gotBody, want) {
			t.Errorf("Request enthält %s nicht: %s", want, gotBody)
		}
	}
}
