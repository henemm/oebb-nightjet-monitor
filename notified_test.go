package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Store gemeldeter Treffer (Spec treffer-einmalig, Abschnitt 2).
// Erwartete Schnittstelle:
//   - loadNotified(path) (*notifiedStore, error) — fehlende Datei = leer, kaputte = Fehler
//   - (s) contains(from, to, date) bool
//   - (s) add(from, to, date, at)
//   - (s) save() error — atomar (tmp + rename), No-op bei leerem Pfad

func TestNotified_MissingFileIsEmpty(t *testing.T) {
	s, err := loadNotified(filepath.Join(t.TempDir(), "nightjet.notified"))
	if err != nil {
		t.Fatalf("fehlende Datei darf kein Fehler sein: %v", err)
	}
	if s.contains(watchFrom, watchTo, "2026-12-28") {
		t.Error("leerer Store meldet Treffer als bekannt")
	}
}

func TestNotified_CorruptFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nightjet.notified")
	if err := os.WriteFile(path, []byte("{kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNotified(path); err == nil {
		t.Error("kaputte Datei muss einen Fehler liefern")
	}
}

func TestNotified_SaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nightjet.notified")
	s, err := loadNotified(path)
	if err != nil {
		t.Fatal(err)
	}
	s.add(watchFrom, watchTo, "2026-12-28", time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC))
	if !s.contains(watchFrom, watchTo, "2026-12-28") {
		t.Fatal("add/contains passen nicht zusammen")
	}
	if s.contains(watchFrom, watchTo, "2026-12-29") {
		t.Error("anderes Datum darf nicht als gemeldet gelten")
	}
	if err := s.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("nach save darf keine .tmp-Datei übrig bleiben")
	}

	reloaded, err := loadNotified(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.contains(watchFrom, watchTo, "2026-12-28") {
		t.Error("Eintrag nach Neuladen verloren")
	}
}

func TestNotified_EmptyPathSaveIsNoop(t *testing.T) {
	s, err := loadNotified("")
	if err != nil {
		t.Fatalf("leerer Pfad: %v", err)
	}
	s.add(watchFrom, watchTo, "2026-12-28", time.Now())
	if err := s.save(); err != nil {
		t.Errorf("save bei leerem Pfad muss No-op sein: %v", err)
	}
	if !s.contains(watchFrom, watchTo, "2026-12-28") {
		t.Error("Store im Arbeitsspeicher muss trotzdem funktionieren")
	}
}

func TestNotified_DirtyFlag(t *testing.T) {
	s, err := loadNotified(filepath.Join(t.TempDir(), "nightjet.notified"))
	if err != nil {
		t.Fatal(err)
	}
	s.add(watchFrom, watchTo, "2026-12-28", time.Now())
	if !s.dirty {
		t.Error("nach add muss dirty true sein")
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if s.dirty {
		t.Error("nach erfolgreichem save muss dirty false sein")
	}
}
