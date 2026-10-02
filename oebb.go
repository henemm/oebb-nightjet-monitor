package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
	_ "time/tzdata" // Zeitzonen-Daten einbetten (Alpine/Scratch-Container ohne tzdata)
)

// HAFAS-Fahrplanauskunft der ÖBB (inoffizieller Webapp-Endpunkt).
// AID/Version/Client stehen nur hier — ändert ÖBB sie, ist nur dieser Block anzupassen.
const (
	hafasURL        = "https://fahrplan.oebb.at/bin/mgate.exe"
	hafasAID        = "OWDL4fE4ixNiPBBm"
	hafasVersion    = "1.41"
	hafasExt        = "OEBB.1"
	hafasLang       = "deu"
	hafasClientID   = "OEBB"
	hafasClientV    = "1"
	hafasClientType = "WEB"
	hafasClientName = "webapp"

	// Produktfilter der Webapp-Option "Nur Direktverbindungen" (zusammen mit maxChg=0).
	hafasProductFilter = "2762"
	// HAFAS-Fehlercode "keine Verbindung gefunden" — leeres Ergebnis, kein Fehler.
	hafasNoConnection = "H890"

	// Manche ÖBB-Endpunkte blocken den Go-Default-UA.
	browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36"
)

// viennaLoc ist die Zeitzone der HAFAS-Zeitangaben.
var viennaLoc = loadVienna()

func loadVienna() *time.Location {
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		log.Printf("⚠ Zeitzone Europe/Vienna nicht ladbar, nutze UTC: %v", err)
		return time.UTC
	}
	return loc
}

type OEBBClient struct {
	httpClient *http.Client
	baseURL    string
}

func NewOEBBClient() *OEBBClient {
	return &OEBBClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    hafasURL,
	}
}

type hafasSvcReq struct {
	Meth string      `json:"meth"`
	Req  interface{} `json:"req"`
}

type hafasClientInfo struct {
	ID   string `json:"id"`
	V    string `json:"v"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type hafasAuth struct {
	Type string `json:"type"`
	AID  string `json:"aid"`
}

type hafasEnvelope struct {
	Lang    string          `json:"lang"`
	SvcReqL []hafasSvcReq   `json:"svcReqL"`
	Client  hafasClientInfo `json:"client"`
	Ext     string          `json:"ext"`
	Ver     string          `json:"ver"`
	Auth    hafasAuth       `json:"auth"`
}

// call sendet genau einen HAFAS-Request und liefert res sowie den Fehlercode aus svcResL[0].
// Transport-, HTTP-, JSON- und Top-Level-Fehler werden als error zurückgegeben.
func (c *OEBBClient) call(meth string, req interface{}) (json.RawMessage, string, error) {
	env := hafasEnvelope{
		Lang:    hafasLang,
		SvcReqL: []hafasSvcReq{{Meth: meth, Req: req}},
		Client:  hafasClientInfo{ID: hafasClientID, V: hafasClientV, Type: hafasClientType, Name: hafasClientName},
		Ext:     hafasExt,
		Ver:     hafasVersion,
		Auth:    hafasAuth{Type: "AID", AID: hafasAID},
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling %s request: %w", meth, err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("creating %s request: %w", meth, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", browserUA)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("%s request failed: %w", meth, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s returned %d: %s", meth, resp.StatusCode, truncate(string(respBody), 200))
	}

	var parsed struct {
		Err     *string `json:"err"`
		ErrTxt  string  `json:"errTxt"`
		SvcResL []struct {
			Err    string          `json:"err"`
			ErrTxt string          `json:"errTxt"`
			Res    json.RawMessage `json:"res"`
		} `json:"svcResL"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, "", fmt.Errorf("decoding %s response: %w", meth, err)
	}
	if parsed.Err != nil && *parsed.Err != "OK" {
		return nil, "", fmt.Errorf("%s error %s: %s", meth, *parsed.Err, parsed.ErrTxt)
	}
	if len(parsed.SvcResL) == 0 {
		return nil, "", fmt.Errorf("%s response without svcResL", meth)
	}
	svc := parsed.SvcResL[0]
	if svc.Err != "OK" && svc.Err != hafasNoConnection {
		return nil, "", fmt.Errorf("%s error %s: %s", meth, svc.Err, svc.ErrTxt)
	}
	return svc.Res, svc.Err, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Station represents an ÖBB station.
type Station struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// SearchStation looks up a station by name (HAFAS LocMatch) and returns the best match.
func (c *OEBBClient) SearchStation(name string) (*Station, error) {
	req := map[string]interface{}{
		"input": map[string]interface{}{
			"field":  "S",
			"loc":    map[string]interface{}{"name": name, "type": "S"},
			"maxLoc": 3,
		},
	}
	res, code, err := c.call("LocMatch", req)
	if err != nil {
		return nil, err
	}
	if code != "OK" {
		return nil, fmt.Errorf("LocMatch error %s for %q", code, name)
	}

	var parsed struct {
		Match struct {
			LocL []struct {
				Name  string `json:"name"`
				ExtID string `json:"extId"`
			} `json:"locL"`
		} `json:"match"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		return nil, fmt.Errorf("decoding LocMatch result: %w", err)
	}
	if len(parsed.Match.LocL) == 0 {
		return nil, fmt.Errorf("no station found for %q", name)
	}
	first := parsed.Match.LocL[0]
	num, err := strconv.Atoi(first.ExtID)
	if err != nil {
		return nil, fmt.Errorf("invalid extId %q for %q: %w", first.ExtID, name, err)
	}
	return &Station{Number: num, Name: first.Name}, nil
}

// Connection represents a found Nightjet connection.
type Connection struct {
	TrainName string
	Departure time.Time
	Arrival   time.Time
	From      string
	To        string
	Date      string
}

func stationLid(s *Station) string {
	return "A=1@L=" + strconv.Itoa(s.Number) + "@"
}

// SearchConnections queries HAFAS TripSearch for direct Nightjet connections
// departing on the given date (YYYY-MM-DD).
func (c *OEBBClient) SearchConnections(from, to *Station, date string) ([]Connection, error) {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q: %w", date, err)
	}
	outDate := day.Format("20060102")

	req := map[string]interface{}{
		"depLocL":     []map[string]string{{"type": "S", "lid": stationLid(from)}},
		"arrLocL":     []map[string]string{{"type": "S", "lid": stationLid(to)}},
		"outDate":     outDate,
		"outTime":     "000000",
		"jnyFltrL":    []map[string]string{{"type": "PROD", "mode": "INC", "value": hafasProductFilter}},
		"maxChg":      0,
		"numF":        10,
		"getPasslist": false,
		"getPolyline": false,
	}
	res, code, err := c.call("TripSearch", req)
	if err != nil {
		return nil, err
	}
	if code == hafasNoConnection {
		return nil, nil
	}

	var parsed struct {
		Common struct {
			ProdL []struct {
				Name    string `json:"name"`
				ProdCtx struct {
					Name    string `json:"name"`
					CatOutS string `json:"catOutS"`
					CatOutL string `json:"catOutL"`
				} `json:"prodCtx"`
			} `json:"prodL"`
		} `json:"common"`
		OutConL []struct {
			Date string `json:"date"`
			Dep  struct {
				DTimeS string `json:"dTimeS"`
			} `json:"dep"`
			Arr struct {
				ATimeS string `json:"aTimeS"`
			} `json:"arr"`
			SecL []struct {
				Type string `json:"type"`
				Jny  *struct {
					ProdX *int `json:"prodX"`
				} `json:"jny"`
			} `json:"secL"`
		} `json:"outConL"`
	}
	if err := json.Unmarshal(res, &parsed); err != nil {
		return nil, fmt.Errorf("decoding TripSearch result: %w", err)
	}

	prodL := parsed.Common.ProdL
	var nightjets []Connection
	for _, conn := range parsed.OutConL {
		// TripSearch liefert Folgetage mit — nur der gesuchte Abfahrtstag zählt.
		if conn.Date != outDate {
			continue
		}

		trainName := ""
		for _, sec := range conn.SecL {
			if sec.Type != "JNY" || sec.Jny == nil || sec.Jny.ProdX == nil {
				continue
			}
			idx := *sec.Jny.ProdX
			if idx < 0 || idx >= len(prodL) {
				continue
			}
			p := prodL[idx]
			if p.ProdCtx.CatOutS == "NJ" || p.ProdCtx.CatOutL == "nightjet" {
				trainName = p.ProdCtx.Name
				if trainName == "" {
					trainName = p.Name
				}
				break
			}
		}
		if trainName == "" {
			continue
		}

		dep, err := hafasTime(conn.Date, conn.Dep.DTimeS)
		if err != nil {
			return nil, fmt.Errorf("parsing departure: %w", err)
		}
		arr, err := hafasTime(conn.Date, conn.Arr.ATimeS)
		if err != nil {
			return nil, fmt.Errorf("parsing arrival: %w", err)
		}

		nightjets = append(nightjets, Connection{
			TrainName: trainName,
			Departure: dep,
			Arrival:   arr,
			From:      from.Name,
			To:        to.Name,
			Date:      date,
		})
	}

	return nightjets, nil
}

// hafasTime kombiniert ein HAFAS-Datum (YYYYMMDD) mit einer Zeit HHMMSS oder
// DDHHMMSS (DD = Tagesoffset zum Datum) in Europe/Vienna.
func hafasTime(date, t string) (time.Time, error) {
	day, err := time.Parse("20060102", date)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: %w", date, err)
	}
	offset := 0
	switch len(t) {
	case 6:
	case 8:
		offset, err = strconv.Atoi(t[:2])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time %q", t)
		}
		t = t[2:]
	default:
		return time.Time{}, fmt.Errorf("invalid time %q", t)
	}
	hh, err1 := strconv.Atoi(t[0:2])
	mm, err2 := strconv.Atoi(t[2:4])
	ss, err3 := strconv.Atoi(t[4:6])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("invalid time %q", t)
	}
	return time.Date(day.Year(), day.Month(), day.Day()+offset, hh, mm, ss, 0, viennaLoc), nil
}
