package main

import (
	"encoding/json"
	"os"
	"time"
)

// notifiedEntry ist ein bereits gemeldeter Treffer (Namen wie in config.yaml).
type notifiedEntry struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Date       string `json:"date"`
	NotifiedAt string `json:"notified_at"`
}

// notifiedStore merkt sich gemeldete Treffer, optional persistent in einer JSON-Datei.
type notifiedStore struct {
	path    string
	entries []notifiedEntry
	dirty   bool // ungespeicherte Einträge vorhanden
}

// loadNotified lädt den Store. Leerer Pfad oder fehlende Datei = leerer Store;
// unlesbare oder kaputte Datei = Fehler.
func loadNotified(path string) (*notifiedStore, error) {
	s := &notifiedStore{path: path}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *notifiedStore) contains(from, to, date string) bool {
	for _, e := range s.entries {
		if e.From == from && e.To == to && e.Date == date {
			return true
		}
	}
	return false
}

func (s *notifiedStore) add(from, to, date string, at time.Time) {
	s.entries = append(s.entries, notifiedEntry{From: from, To: to, Date: date, NotifiedAt: at.Format(time.RFC3339)})
	s.dirty = true
}

// save schreibt atomar (tmp + rename). Leerer Pfad = nur Arbeitsspeicher, No-op.
func (s *notifiedStore) save() error {
	if s.path == "" {
		s.dirty = false
		return nil
	}
	entries := s.entries
	if entries == nil {
		entries = []notifiedEntry{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	s.dirty = false
	return nil
}
