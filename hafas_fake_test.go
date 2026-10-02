package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Gemeinsame Testhilfen: ein Fake-HAFAS-Server (httptest) und Antwort-Bauer.
// Kein Test darf das Live-Netz brauchen.

type hafasCall struct {
	Meth string
	Name string // LocMatch: gesuchter Stationsname
	Dep  string // TripSearch: lid der Abfahrtsstation
	Arr  string // TripSearch: lid der Zielstation
	Date string // TripSearch: outDate (YYYYMMDD)
}

type fakeHafas struct {
	mu       sync.Mutex
	calls    []hafasCall
	stations map[string]string                           // Stationsname -> extId; unbekannt/fehlend -> leere locL
	trip     func(c hafasCall) (status int, body string) // Antwort auf TripSearch
	locFail  func(name string) (status int, body string) // optional: LocMatch-Fehler erzwingen
	srv      *httptest.Server
}

func newFakeHafas(t *testing.T) *fakeHafas {
	t.Helper()
	f := &fakeHafas{
		stations: map[string]string{
			"Amsterdam Centraal": "8400058",
			"Salzburg Hbf":       "8100002",
			"Wien Hbf":           "8103000",
			"Innsbruck Hbf":      "8100108",
		},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHafas) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		SvcReqL []struct {
			Meth string `json:"meth"`
			Req  struct {
				Input struct {
					Loc struct {
						Name string `json:"name"`
					} `json:"loc"`
				} `json:"input"`
				DepLocL []struct {
					Lid string `json:"lid"`
				} `json:"depLocL"`
				ArrLocL []struct {
					Lid string `json:"lid"`
				} `json:"arrLocL"`
				OutDate string `json:"outDate"`
			} `json:"req"`
		} `json:"svcReqL"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || len(req.SvcReqL) != 1 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s := req.SvcReqL[0]
	call := hafasCall{Meth: s.Meth, Name: s.Req.Input.Loc.Name, Date: s.Req.OutDate}
	if len(s.Req.DepLocL) > 0 {
		call.Dep = s.Req.DepLocL[0].Lid
	}
	if len(s.Req.ArrLocL) > 0 {
		call.Arr = s.Req.ArrLocL[0].Lid
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	status, body := http.StatusOK, ""
	switch s.Meth {
	case "LocMatch":
		if f.locFail != nil {
			if st, b := f.locFail(call.Name); st != 0 {
				status, body = st, b
				break
			}
		}
		body = locMatchJSON(call.Name, f.stations[call.Name])
	case "TripSearch":
		if f.trip != nil {
			status, body = f.trip(call)
		} else {
			body = tripH890JSON()
		}
	default:
		status, body = http.StatusBadRequest, "unknown meth"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (f *fakeHafas) tripCalls() []hafasCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []hafasCall
	for _, c := range f.calls {
		if c.Meth == "TripSearch" {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeHafas) client() *OEBBClient {
	c := NewOEBBClient()
	c.baseURL = f.srv.URL
	return c
}

func lidFor(extID string) string { return "A=1@L=" + extID + "@" }

func locMatchJSON(name, extID string) string {
	locL := []map[string]interface{}{}
	if extID != "" {
		locL = append(locL, map[string]interface{}{"name": name, "extId": extID, "lid": lidFor(extID), "type": "S"})
	}
	return mustJSON(map[string]interface{}{
		"err": "OK",
		"svcResL": []interface{}{map[string]interface{}{
			"meth": "LocMatch", "err": "OK",
			"res": map[string]interface{}{"match": map[string]interface{}{"locL": locL}},
		}},
	})
}

func tripH890JSON() string {
	return mustJSON(map[string]interface{}{
		"err": "OK",
		"svcResL": []interface{}{map[string]interface{}{
			"meth": "TripSearch", "err": "H890", "errTxt": "HAFAS Kernel: No connection found.",
		}},
	})
}

type testConn struct {
	Date    string // YYYYMMDD (Abfahrtstag)
	DepS    string // HHMMSS
	ArrS    string // HHMMSS oder DDHHMMSS
	Name    string
	Num     string
	CatOutS string
	CatOutL string
}

func tripJSON(conns ...testConn) string {
	prodL := []interface{}{}
	outConL := []interface{}{}
	for i, c := range conns {
		prodL = append(prodL, map[string]interface{}{
			"prodCtx": map[string]interface{}{
				"name": c.Name, "num": c.Num, "catOutS": c.CatOutS, "catOutL": c.CatOutL,
			},
		})
		outConL = append(outConL, map[string]interface{}{
			"date": c.Date,
			"chg":  0,
			"dep":  map[string]interface{}{"dTimeS": c.DepS},
			"arr":  map[string]interface{}{"aTimeS": c.ArrS},
			"secL": []interface{}{map[string]interface{}{
				"type": "JNY", "jny": map[string]interface{}{"prodX": i},
			}},
		})
	}
	return mustJSON(map[string]interface{}{
		"err": "OK",
		"svcResL": []interface{}{map[string]interface{}{
			"meth": "TripSearch", "err": "OK",
			"res": map[string]interface{}{
				"common":  map[string]interface{}{"prodL": prodL},
				"outConL": outConL,
			},
		}},
	})
}

func njConn(date, depS, arrS string) testConn {
	return testConn{Date: date, DepS: depS, ArrS: arrS, Name: "NJ 233", Num: "233", CatOutS: "NJ", CatOutL: "nightjet"}
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// countingServer zählt Aufrufe (Heartbeat- bzw. Telegram-Testserver).
type countingServer struct {
	mu   sync.Mutex
	hits int
	srv  *httptest.Server
}

func newCountingServer(t *testing.T) *countingServer {
	t.Helper()
	c := &countingServer{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *countingServer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
