# Context: bug-5-treffer-einmalig

## Request Summary
Issue #5 + Rest von #4: Ein Treffer soll genau einmal per Telegram gemeldet werden, auch über Container-Neustarts hinweg. Scheitert eine Telegram-Zustellung, darf der Zyklus nicht als erfolgreich gelten, damit `henemm-infra/scripts/monitor.sh` über einen anderen Kanal alarmiert. Vom PO am 2026-10-03 freigegeben.

## Befund (2026-10-03)
- Das Telegram-Token war ungültig (401). Das fiel niemandem auf, weil Zustellfehler nur geloggt wurden und die Erfolgsdatei trotzdem geschrieben wurde. Token und Gruppe sind inzwischen ersetzt (#4).
- Der Container läuft seit dem 2026-10-03 über Compose mit `restart: unless-stopped`.

## Analysis

### Type
Bug

### Root Cause
1. `main.go:253`: Ein gemeldeter Treffer wird nur aus `m.watchList` entfernt, also nur im Arbeitsspeicher.
2. `main.go:144-146`: Bei leerer Watchlist endet `main()` mit Exit 0. Docker startet neu, `resolveStations` baut die Watchlist aus `config.yaml` wieder auf, und es kommt zur erneuten Meldung. Das passiert ungefähr einmal pro Intervall. Jeder andere Neustart (Deploy, Reboot) löst ebenfalls eine erneute Meldung aus.
3. `main.go:248-251`: Ein Fehler bei `SendTelegramNotification` hält den Eintrag zwar fest, `checkAll` liefert aber trotzdem `true`, und die Erfolgsdatei wird geschrieben.
4. `main.go:231-232`: Ein gescheiterter Fehler-Alarm wird nur geloggt. Der Zyklus ist wegen `hadError` ohnehin nicht erfolgreich, das ist also okay.
5. `main.go:310-312`: Ein gescheiterter Canary-Alarm führt zu `return true`, und die Erfolgsdatei wird geschrieben.

### Affected Files (with changes)
| File | Change Type | Description |
|------|-------------|-------------|
| main.go | MODIFY | Notified-Store laden und filtern, nach Versand speichern, kein Selbst-Exit, Zustellfehler bedeuten keinen Erfolg |
| config.go | MODIFY | Neues Feld `notified_file` |
| notified.go | CREATE | Kleiner Store: Laden, Enthält, Hinzufügen und atomares Speichern (tmp + rename) |
| main_test.go | MODIFY | Tests für Dedupe über Neustart, Zustellfehler und leere Watchlist |
| config_test.go | MODIFY | `notified_file` wird geladen |
| CLAUDE.md, README.md | MODIFY | Doku |
| config.yaml (gitignored, Hauptordner) | MODIFY | `notified_file: "/data/nightjet.notified"` |

### Scope Assessment
- Files: 5 Code-/Testdateien + Doku
- Estimated LoC: +150/-10
- Risk Level: LOW–MEDIUM (zentraler Zyklus, aber durch bestehende Fake-HAFAS-/Fake-Telegram-Tests gut abgedeckt)

### Technical Approach
- Schlüssel eines Treffers: `from|to|date` mit den Namen aus `config.yaml`. Die Namen aus dem Config bleiben stabil, auch wenn die HAFAS-Antwort sich ändert.
- Datei: JSON-Liste `[{"from","to","date","notified_at"}]`. Das Speichern ist atomar: erst in `<file>.tmp` schreiben, dann `rename`.
- `watchListInitialized` bedeutet „Stationen aufgelöst“, nicht „Watchlist nicht leer“. Sind alle Treffer gemeldet, ist die Watchlist leer, gilt aber als initialisiert. Der Zyklus läuft dann mit Canary und Erfolgsdatei weiter.
- Fehlende Datei bedeutet: noch nichts gemeldet. Eine unlesbare oder kaputte Datei wird in jedem Zyklus erneut geladen. Solange das scheitert, gibt es keine Watch-Prüfung und keine Treffer-Meldung (Schutz vor Spam) und keinen Erfolg (Alarm).
- Wurde ein Treffer zugestellt, aber das Speichern scheitert, verlässt der Eintrag trotzdem die Watchlist (keine Doppelmeldung im laufenden Prozess). Der Zyklus gilt aber als nicht erfolgreich, das löst einen Alarm aus.
- Ist `notified_file` leer, wird nur im Arbeitsspeicher gemerkt (Warnung im Log). Der Self-Exit entfällt trotzdem.
- Zustellfehler bei Treffer oder Canary-Alarm bedeuten: Zyklus nicht erfolgreich.

### Dependencies
- `henemm-infra/scripts/monitor.sh` (`check_nightjet`) wertet das Alter der Erfolgsdatei aus. Dort ist keine Änderung nötig.
- Volume `/home/hem/backups/nightjet-monitor:/data` existiert bereits.

### Open Questions
- keine
