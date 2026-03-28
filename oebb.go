package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	initURL     = "https://tickets.oebb.at/api/domain/v4/init"
	shopBaseURL = "https://shop.oebbtickets.at"
	tokenMaxAge = 2300 * time.Second // refresh before 2400s timeout
)

type OEBBClient struct {
	httpClient  *http.Client
	accessToken string
	tokenTime   time.Time
	mu          sync.Mutex
}

func NewOEBBClient() *OEBBClient {
	return &OEBBClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Init fetches a fresh access token from the ÖBB API.
func (c *OEBBClient) Init() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, err := http.NewRequest("GET", initURL, nil)
	if err != nil {
		return fmt.Errorf("creating init request: %w", err)
	}
	req.Header.Set("Channel", "inet")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("init request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("init returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding init response: %w", err)
	}
	if result.AccessToken == "" {
		return fmt.Errorf("empty access token in init response")
	}

	c.accessToken = result.AccessToken
	c.tokenTime = time.Now()
	log.Printf("ÖBB token acquired")
	return nil
}

func (c *OEBBClient) ensureToken() error {
	c.mu.Lock()
	needsRefresh := c.accessToken == "" || time.Since(c.tokenTime) > tokenMaxAge
	c.mu.Unlock()

	if needsRefresh {
		return c.Init()
	}
	return nil
}

func (c *OEBBClient) getToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken
}

func (c *OEBBClient) doRequest(req *http.Request) (*http.Response, error) {
	req.Header.Set("Channel", "inet")
	req.Header.Set("AccessToken", c.getToken())
	req.Header.Set("x-ts-supportid", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	return c.httpClient.Do(req)
}

// Station represents an ÖBB station.
type Station struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// SearchStation looks up a station by name and returns the best match.
func (c *OEBBClient) SearchStation(name string) (*Station, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	u := shopBaseURL + "/api/hafas/v1/stations?" + url.Values{"name": {name}}.Encode()
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating station request: %w", err)
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("station request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("station search returned %d: %s", resp.StatusCode, string(body))
	}

	var stations []Station
	if err := json.NewDecoder(resp.Body).Decode(&stations); err != nil {
		return nil, fmt.Errorf("decoding station response: %w", err)
	}
	if len(stations) == 0 {
		return nil, fmt.Errorf("no station found for %q", name)
	}

	return &stations[0], nil
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

// SearchConnections queries the ÖBB timetable for Nightjet connections on a given date.
func (c *OEBBClient) SearchConnections(from, to *Station, date string) ([]Connection, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"datetimeDeparture": date + "T20:00:00.000",
		"filter": map[string]interface{}{
			"regionaltrains": false,
			"direct":         false,
			"changeTime":     false,
			"wheelchair":     false,
			"bikes":          false,
			"trains":         false,
		},
		"passengers": []map[string]interface{}{{}},
		"count":      5,
		"from":       map[string]interface{}{"number": from.Number, "name": from.Name},
		"to":         map[string]interface{}{"number": to.Number, "name": to.Name},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling timetable request: %w", err)
	}

	req, err := http.NewRequest("POST", shopBaseURL+"/api/hafas/v4/timetable", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating timetable request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("timetable request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("timetable returned %d: %s", resp.StatusCode, string(respBody))
	}

	var timetableResp struct {
		Connections []struct {
			From struct {
				Name      string `json:"name"`
				Departure string `json:"departure"`
			} `json:"from"`
			To struct {
				Name    string `json:"name"`
				Arrival string `json:"arrival"`
			} `json:"to"`
			Sections []sectionInfo `json:"sections"`
		} `json:"connections"`
	}

	if err := json.Unmarshal(respBody, &timetableResp); err != nil {
		return nil, fmt.Errorf("decoding timetable response: %w", err)
	}

	var nightjets []Connection
	for _, conn := range timetableResp.Connections {
		trainName := findNightjetName(conn.Sections)
		if trainName == "" {
			continue
		}
		dep, _ := time.Parse("2006-01-02T15:04:05.000", conn.From.Departure)
		arr, _ := time.Parse("2006-01-02T15:04:05.000", conn.To.Arrival)

		nightjets = append(nightjets, Connection{
			TrainName: trainName,
			Departure: dep,
			Arrival:   arr,
			From:      conn.From.Name,
			To:        conn.To.Name,
			Date:      date,
		})
	}

	return nightjets, nil
}

type sectionInfo struct {
	Category struct {
		Name        string          `json:"name"`
		Number      string          `json:"number"`
		ShortName   string          `json:"shortName"`
		DisplayName string          `json:"displayName"`
		LongName    json.RawMessage `json:"longName"`
	} `json:"category"`
	Type string `json:"type"`
}

// findNightjetName checks if any section is a Nightjet and returns its name, or "" if not.
func findNightjetName(sections []sectionInfo) string {
	for _, s := range sections {
		cat := s.Category
		if strings.HasPrefix(cat.Name, "NJ") || strings.HasPrefix(cat.Name, "EN") ||
			strings.HasPrefix(cat.DisplayName, "NJ") || strings.HasPrefix(cat.DisplayName, "EN") ||
			strings.HasPrefix(cat.ShortName, "NJ") || strings.HasPrefix(cat.ShortName, "EN") ||
			longNameContains(cat.LongName, "Nightjet") {
			name := cat.Name
			if cat.Number != "" {
				name += " " + cat.Number
			}
			return name
		}
	}
	return ""
}

func longNameContains(raw json.RawMessage, target string) bool {
	if len(raw) == 0 {
		return false
	}
	// Try as string first
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == target
	}
	// Try as localized object {"de":"..","en":".."}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		for _, v := range m {
			if v == target {
				return true
			}
		}
	}
	return false
}
