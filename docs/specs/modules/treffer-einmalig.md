---
entity_id: treffer-einmalig
type: module
created: 2026-10-03
updated: 2026-10-03
status: draft
version: "1.0"
tags: [nightjet, telegram, dedupe, readiness]
test_targets: [main_test.go, config_test.go, notified_test.go]
workflow: bug-5-treffer-einmalig
---

# Treffer genau einmal melden, Zustellfehler sichtbar machen

## Approval

- [ ] Approved

## GitHub Issue

- **Issue:** #5 (Treffer werden nach Container-Neustart stündlich erneut gemeldet)
- **Dazu:** Rest von #4 (Telegram-Zustellfehler werden nur geloggt, nicht gemeldet)

## Purpose

Ein im Fahrplan gefundener Nightjet wird genau einmal per Telegram gemeldet, auch über Container-Neustarts hinweg. Scheitert eine Telegram-Zustellung, gilt der Zyklus als nicht erfolgreich. Es wird keine Erfolgsdatei geschrieben, und `henemm-infra/scripts/monitor.sh` alarmiert über die Infra-Kanäle unabhängig vom Bot.

## Source

- **File:** `main.go`, `config.go`, neu `notified.go`
- **Identifier:** `monitor`, `newMonitor`, `runCheck`, `checkAll`, `runCanaryCheck`, `main`, `Config.NotifiedFile`, `notifiedStore`

## Dependencies

| Entity | Type | Purpose |
|--------|------|---------|
| Telegram Bot API | externer Dienst | Treffer- und Alarm-Meldungen |
| `henemm-infra/scripts/monitor.sh` (`check_nightjet`) | Infra-Check | Alarmiert, wenn die Erfolgsdatei zu alt ist (keine Änderung nötig) |
| Volume `/home/hem/backups/nightjet-monitor:/data` | Docker-Volume | Speicherort von Erfolgs- und Notified-Datei (existiert) |
| Go stdlib (`encoding/json`, `os`) | Bibliothek | Datei lesen und schreiben |

## Scope

**In Scope**
- Persistente Liste gemeldeter Treffer (`notified_file`)
- Filtern bereits gemeldeter Treffer beim Aufbau der Watchlist
- Kein Selbst-Beenden des Prozesses bei leerer Watchlist
- Zustellfehler (Treffer, Canary-Alarm) und Speicherfehler führen zu einem nicht erfolgreichen Zyklus
- Doku (CLAUDE.md, README.md) und Server-`config.yaml`

**Out of Scope**
- Änderungen an `monitor.sh` oder Alarm-Kanälen der Infra
- Wiederholte Erinnerungen zu bereits gemeldeten Treffern
- Erkennen, dass ein gemeldeter Treffer wieder aus dem Fahrplan verschwindet

## Implementation Details

### 1. Config (`config.go`)
Neues optionales Feld `notified_file` (`Config.NotifiedFile`, String). Bleibt es leer, merkt sich der Monitor Treffer nur im Arbeitsspeicher und schreibt beim Start eine Warnung ins Log.

### 2. Store (`notified.go`)
- Schlüssel eines Treffers: Von, Nach und Datum, jeweils exakt wie in `config.yaml` angegeben (stabil gegenüber HAFAS-Namensänderungen).
- Dateiformat: JSON-Array von Objekten `{"from","to","date","notified_at"}` (`notified_at` als RFC3339).
- `load(path)`: Eine fehlende Datei ergibt eine leere Menge ohne Fehler. Unlesbares oder ungültiges JSON ergibt einen Fehler.
- `contains(from, to, date)`, `add(from, to, date, time)`.
- `save()`: atomar, also erst nach `<path>.tmp` schreiben und dann `os.Rename`. Ist der Pfad leer, ist `save` ein No-op.

### 3. Monitor (`main.go`)
- `newMonitor` lädt den Store. Scheitert das Laden, merkt sich der Monitor den Fehler und versucht es in jedem Zyklus erneut.
- Die Watchlist wird wie bisher aus `config.yaml` aufgelöst. Anschließend werden Einträge entfernt, die im Store stehen. `watchListInitialized` bedeutet „Stationen aufgelöst“ (Liste vor dem Filtern nicht leer). Sind alle Einträge bereits gemeldet, ist die Watchlist leer, aber initialisiert.
- Solange der Store nicht geladen ist: keine Watch-Prüfung und keine Treffer-Meldung (Schutz vor Doppelmeldung). Der Zyklus gilt als nicht erfolgreich, der Canary läuft trotzdem.
- `checkAll` nach erfolgreichem Versand: Eintrag in den Store, `save()`, Eintrag verlässt die Watchlist. Scheitert `save()`, verlässt der Eintrag trotzdem die Watchlist (keine Doppelmeldung im laufenden Prozess), der Zyklus gilt aber als nicht erfolgreich.
- `checkAll` bei Versandfehler: Der Eintrag bleibt auf der Watchlist (erneuter Versuch im nächsten Zyklus), der Zyklus gilt als nicht erfolgreich.
- `runCanaryCheck`: Scheitert der Versand des Canary-Alarms, ist das Ergebnis „nicht erfolgreich“ (`false`). Das Flag `alerted` bleibt `false`, damit im nächsten Zyklus erneut versucht wird.
- `main()`: Der Block „All connections notified … Exiting.“ entfällt. Der Prozess läuft weiter, bis SIGTERM oder SIGINT kommt.

### 4. Dokumentation
CLAUDE.md (Abschnitt Deployment: `notified_file`, Verhalten bei Zustellfehlern), README.md (Config-Beispiel), Server-`config.yaml`: `notified_file: "/data/nightjet.notified"`.

## Expected Behavior

- **Input:** Config mit Verbindungen, optional `notified_file`, dazu Antworten von HAFAS und Telegram.
- **Output:** Höchstens eine Telegram-Treffermeldung pro Von/Nach/Datum über die gesamte Lebensdauer der Notified-Datei. Eine Erfolgsdatei nur, wenn HAFAS, Canary, alle nötigen Telegram-Zustellungen und das Speichern geklappt haben.
- **Side effects:** Die Datei `notified_file` wird angelegt und aktualisiert.

## Error Handling

| Fall | Verhalten |
|------|-----------|
| Notified-Datei fehlt | leere Menge, normaler Betrieb |
| Notified-Datei kaputt oder unlesbar | Log-Fehler, keine Watch-Prüfung, kein Erfolg (Alarm über `monitor.sh`), erneuter Ladeversuch in jedem Zyklus |
| Telegram-Treffer scheitert | Eintrag bleibt, kein Erfolg |
| Speichern nach Versand scheitert | Eintrag verlässt die Watchlist, kein Erfolg |
| Canary-Alarm scheitert | kein Erfolg, erneuter Versuch im nächsten Zyklus |
| Fehler-Alarm (`SendTelegramError`) scheitert | unverändert: Zyklus ist wegen des HAFAS-Fehlers ohnehin nicht erfolgreich |

## Known Limitations

- Wird `notified_file` gelöscht, wird ein noch im Fahrplan stehender Treffer erneut gemeldet. Das ist bewusst so, als Möglichkeit zum Zurücksetzen.
- Ändert der PO in `config.yaml` die Schreibweise einer Station, gilt die Kombination als neu.
- Ein Treffer, der zugestellt wurde, aber nicht gespeichert werden konnte, wird nach einem Neustart erneut gemeldet. Der Alarm über `monitor.sh` macht den Zustand vorher sichtbar.

## Definition of Done

- Alle AC durch automatische Tests abgedeckt und grün (`go test ./...` im Container `golang:1.26-alpine`)
- `go vet` sauber
- Server-`config.yaml` ergänzt, Container per Compose neu gebaut, Log zeigt einen normalen Zyklus, Erfolgsdatei aktuell
- CLAUDE.md und README.md aktualisiert
- Issue #5 und Rest von #4 geschlossen

## Acceptance Criteria

- **AC-1:** Given HAFAS liefert für die Watch-Strecke einen Nightjet und Telegram antwortet OK, `notified_file` zeigt in ein leeres Temp-Verzeichnis / When ein Monitor einen Zyklus läuft und danach ein **neuer** Monitor mit derselben Config einen Zyklus läuft / Then wurde insgesamt genau eine Treffermeldung an Telegram gesendet, und die Watchlist des zweiten Monitors ist leer
- **AC-2:** Given ein Zyklus mit zugestelltem Treffer / When die Notified-Datei gelesen wird / Then enthält sie gültiges JSON mit einem Eintrag `from`/`to`/`date` gleich den Config-Werten und einem RFC3339-`notified_at`
- **AC-3:** Given alle Watch-Einträge sind bereits gemeldet, HAFAS und Canary gesund, `success_file` gesetzt / When ein Zyklus läuft / Then wird keine Treffermeldung gesendet, die Erfolgsdatei wird geschrieben und `watchListInitialized` ist `true`
- **AC-4:** Given HAFAS liefert einen Treffer, Telegram antwortet mit HTTP 401 / When ein Zyklus läuft / Then bleibt der Eintrag auf der Watchlist, es wird **keine** Erfolgsdatei geschrieben und kein Eintrag in der Notified-Datei angelegt
- **AC-5:** Given HAFAS liefert einen Treffer, Telegram OK, das Verzeichnis der Notified-Datei ist nicht beschreibbar / When ein Zyklus läuft / Then verlässt der Eintrag die Watchlist und es wird **keine** Erfolgsdatei geschrieben
- **AC-6:** Given die Notified-Datei enthält ungültiges JSON, HAFAS liefert einen Treffer / When ein Zyklus läuft / Then wird **keine** Treffermeldung gesendet und keine Erfolgsdatei geschrieben. When die Datei danach repariert wird und ein weiterer Zyklus läuft / Then wird normal geprüft
- **AC-7:** Given der Canary findet an weniger als 50 % der Tage einen Nightjet (3 Zyklen in Folge), Telegram antwortet mit HTTP 500 / When der dritte Zyklus läuft / Then wird keine Erfolgsdatei geschrieben, und im nächsten Zyklus wird der Canary-Alarm erneut versucht
- **AC-8:** Given eine Config-Datei mit `notified_file: "/data/x"` / When `LoadConfig` sie lädt / Then ist `cfg.NotifiedFile == "/data/x"`; ohne das Feld ist es `""` und das Laden gelingt
- **AC-9:** Given `notified_file` ist leer / When derselbe Monitor zwei Zyklen mit Treffer läuft / Then wird genau eine Treffermeldung gesendet (Verhalten im Arbeitsspeicher bleibt)
- **AC-10:** Given der Code in `main.go` / When man ihn liest / Then gibt es keinen Pfad mehr, der den Scheduler wegen leerer Watchlist beendet (Code-Review-AC, vom Adversary geprüft)

## Test Plan

- `notified_test.go`: Laden (fehlt, gültig, kaputt), `add`/`contains`, atomares `save` (kein `.tmp` bleibt übrig).
- `main_test.go`: AC-1 bis AC-7 und AC-9 mit Fake-HAFAS (`hafas_fake_test.go`) und Fake-Telegram (`telegramBaseURL`). Für AC-4 und AC-7 gibt der Telegram-Testserver 401 bzw. 500 zurück.
- `config_test.go`: AC-8.
- Bestehende Tests bleiben grün (`TestWatchHit_SendsTelegramAndRemovesEntry`, `TestWatchHit_TelegramFailureKeepsEntry`, Heartbeat- und Erfolgsdatei-Tests).

## Architektur-Entscheidung (ADR)

Einfache JSON-Datei statt Datenbank: höchstens eine Handvoll Einträge, ein einziger Schreiber (der Container), dasselbe Volume wie die Erfolgsdatei. Atomares Schreiben per Rename verhindert halb geschriebene Dateien bei Absturz. Das Signal für Zustellfehler läuft über die bestehende Erfolgsdatei und braucht keinen zweiten Alarmweg, denn `monitor.sh` alarmiert bereits unabhängig von Telegram.

## Changelog

- 2026-10-03: Erstfassung (v1.0)
