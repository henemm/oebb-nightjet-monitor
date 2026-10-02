---
entity_id: hafas-umstellung
type: module
created: 2026-10-02
updated: 2026-10-02
status: draft
version: "1.0"
tags: [oebb, hafas, nightjet, canary, telegram]
test_targets: [oebb_test.go, config_test.go, main_test.go, notify_test.go]
workflow: hafas-umstellung
---

# HAFAS-Umstellung (ÖBB Fahrplanauskunft statt Ticketshop)

## Approval

- [ ] Approved

## GitHub Issue

- **Issue:** #1 (Reaktivierung geplant ~November 2026; ohne diese Umstellung nicht möglich)

## Purpose

Der Monitor fragt künftig die öffentliche ÖBB-Fahrplanauskunft (HAFAS, `fahrplan.oebb.at/bin/mgate.exe`) statt des Ticketshops ab, weil `shop.oebbtickets.at` seit Herbst 2026 per Cloudflare mit 403 blockt. Beobachtet wird die Direktverbindung Amsterdam Centraal → Salzburg Hbf am 28.12.2026; die Meldung sagt ehrlich "im Fahrplan gefunden", nicht "buchbar".

## Source

- **File:** `oebb.go` (Rewrite), dazu `main.go`, `config.go`, `notify.go`
- **Identifier:** `OEBBClient`, `NewOEBBClient`, `SearchStation`, `SearchConnections`, `Connection`, `Station`, `runCanaryCheck`, `LoadConfig`, `SendTelegramNotification`

## Dependencies

| Entity | Type | Purpose |
|--------|------|---------|
| `fahrplan.oebb.at/bin/mgate.exe` | externer Dienst (inoffiziell) | HAFAS-Endpunkt: `LocMatch` (Stationssuche), `TripSearch` (Verbindungen) |
| Telegram Bot API | externer Dienst | Treffer- und Alarm-Meldungen |
| BetterStack Heartbeat | externer Dienst | Readiness-Ping nur bei fachlichem Erfolg |
| `gopkg.in/yaml.v3` | Go-Modul | Config laden (unverändert) |
| Go stdlib (`net/http`, `encoding/json`, `net/http/httptest` nur in Tests) | Bibliothek | HTTP-Client, JSON, Testserver |
| `golang:1.26-alpine` (Docker) | Build-/Test-Umgebung | Go ist auf dem Host nicht installiert |
| `docs/context/hafas-umstellung.md` | Dokument | Quelle der verifizierten API-Details |

## Scope

- **Affected Files:** `oebb.go` (MODIFY/Rewrite), `main.go` (MODIFY), `config.go` (MODIFY), `notify.go` (MODIFY), `CLAUDE.md` (MODIFY), `README.md` (MODIFY), `oebb_test.go` (CREATE), `config_test.go` (CREATE), `main_test.go` (CREATE), `notify_test.go` (CREATE), `testdata/*.json` (CREATE, aufgezeichnete HAFAS-Antworten), `config.yaml` (gitignored, manuell — Betriebsschritt)
- **Estimated Changes:** ca. +450/-170 LoC (davon Tests ca. +250)

| File | Change Type | Description |
|------|-------------|-------------|
| `oebb.go` | MODIFY (Rewrite) | Token, Init, Shop-API und Mutex entfernen; HAFAS-Client |
| `main.go` | MODIFY | Canary mit eigener Strecke, lazy Auflösung der Canary-Stationen; Zyklus-Logik testbar herausziehen |
| `config.go` | MODIFY | Optionales Feld `canary` mit Default |
| `notify.go` | MODIFY | Ehrlicher Text; Textbau als reine Funktion; "ÖBB Fahrplanauskunft" in Fehlertexten |
| `CLAUDE.md`, `README.md` | MODIFY | Abschnitt "ÖBB API Flow" auf HAFAS umstellen |
| `*_test.go`, `testdata/` | CREATE | Go-Tests mit `httptest` und Fixtures |

## Implementation Details

### 1. HAFAS-Client (`oebb.go`)

- Ein POST-Endpunkt `https://fahrplan.oebb.at/bin/mgate.exe`, `Content-Type: application/json`, Browser-User-Agent (`browserUA` bleibt). Kein Token, kein Refresh, kein Mutex.
- Die Basis-URL ist im Client überschreibbar (Feld `baseURL`, Default die URL oben), damit Tests einen `httptest`-Server einsetzen können. `NewOEBBClient()` behält seine Signatur.
- Envelope (verifiziert am 2026-10-02): `lang: deu`, `svcReqL` mit genau einem Request, `client` = `{id: OEBB, v: 1, type: WEB, name: webapp}`, `ext: OEBB.1`, `ver: 1.41`, `auth` = `{type: AID, aid: OWDL4fE4ixNiPBBm}`. Weitere Header sind nicht nötig. Konstanten für AID/Version stehen an einer Stelle im Code.
- Öffentliche Signaturen bleiben: `Station{Number int, Name string}`, `SearchStation(name) (*Station, error)`, `SearchConnections(from, to *Station, date string) ([]Connection, error)`, `Connection{TrainName, Departure, Arrival, From, To, Date}`. Das Datum bleibt `YYYY-MM-DD`.

### 2. Stationssuche (`LocMatch`)

Request: `input.field = "S"`, `input.loc = {name, type: "S"}`, `maxLoc: 3`. Antwort `res.match.locL[]` mit `name`, `extId` (String, z. B. `8400058`), `lid`. Der erste Treffer wird verwendet (`Number` = `extId` als Zahl, `Name` = `name`). Leere `locL` → Fehler `no station found for "<name>"`.

### 3. Verbindungssuche (`TripSearch`)

- Request: `depLocL`/`arrLocL` mit `lid = "A=1@L=<extId>@"`, `outDate = YYYYMMDD`, `outTime = "000000"`, `jnyFltrL = [{type: PROD, mode: INC, value: "2762"}]`, `maxChg = 0`, `numF = 10`, `getPasslist = false`, `getPolyline = false`. `maxChg=0` plus Produktfilter 2762 entspricht der Webapp-Option "Nur Direktverbindungen".
- Antwort `res.outConL[]`: pro Verbindung `date` (YYYYMMDD), `dep.dTimeS`, `arr.aTimeS`, `secL[].jny.prodX` als Index in `res.common.prodL[]`.
- Abfahrtstag-Filter: TripSearch liefert Folgetage mit. Nur Verbindungen mit `date ==` gesuchtes Datum werden zurückgegeben.
- Nightjet-Erkennung: `prodL[].prodCtx.catOutS == "NJ"` oder `catOutL == "nightjet"` (Name z. B. `NJ 40421`). Andere Produkte werden ignoriert. Die alten Category-Felder (`findNightjetName`, `longNameContains`) entfallen.
- Zeiten: Abfahrt aus `date` + `dTimeS` (`HHMMSS`). Ankunft `aTimeS` ist `HHMMSS` oder 8-stellig `DDHHMMSS`, wobei `DD` der Tagesoffset zum Abfahrtstag ist (z. B. `01064600` = 06:46 am Folgetag). Zeiten in Zone `Europe/Vienna`, die Zone wird im Code geladen (`time.LoadLocation`) und der Docker-Container bringt dafür tzdata mit bzw. Go bettet `time/tzdata` ein.
- `From`/`To` der `Connection` sind die Namen aus der Antwort bzw. den übergebenen `Station`-Objekten.

### 4. Fehlerverhalten

- HTTP-Status != 200 → Fehler.
- Top-Level `err != "OK"` (sofern vorhanden) oder `svcResL[0].err != "OK"` → Fehler, AUSSER `svcResL[0].err == "H890"` (keine Verbindung) → leeres Ergebnis ohne Fehler.
- Ungültiges JSON oder fehlendes `svcResL` → Fehler.
- Kein `log.Fatal` bei externen Fehlern; Fehler werden zurückgegeben (bestehende Resilienz-Regel).

### 5. Config und Canary (`config.go`, `main.go`)

- Neues optionales Feld `canary: {from, to}`. Fehlt es (oder ist ein Teilfeld leer), gilt der Default Wien Hbf → Innsbruck Hbf. Gründe: dort fahren täglich 2 bis 3 Direkt-Nightjets, gemessen auch nach dem Fahrplanwechsel; die Zielstrecke Amsterdam → Salzburg hat ab 13.12. keine Direktverbindung und taugt daher nicht als Referenz.
- Pflichtfelder (`telegram_bot_token`, `telegram_chat_id`, mindestens eine Connection) werden weiter validiert; `check_interval`-Default bleibt 60 Minuten.
- Der Canary nutzt seine eigene Strecke, nicht mehr `watchList[0]`. Die Canary-Stationen werden lazy aufgelöst: schlägt die Auflösung fehl, wird in jedem Zyklus erneut versucht, und in dieser Zeit wird kein Heartbeat gepingt.
- Canary-Logik unverändert: Fenster heute+3 bis heute+10 Tage, mindestens 50 % der erfolgreich abgefragten Tage mit Nightjet, nach 3 Fehlzyklen in Folge Telegram-Alarm.
- Heartbeat unverändert: nur bei `checkOK && canaryAPIOK`.
- Damit Zyklus-Logik ohne Live-Netz testbar ist, wird der Inhalt von `runCheck` in eine Funktion mit injizierbaren Abhängigkeiten (Client, Heartbeat-URL) ausgelagert. Verhalten bleibt gleich.

### 6. Telegram (`notify.go`)

- Treffer-Text: statt "Nightjet jetzt buchbar!" nun "Direktverbindung im Fahrplan gefunden — Buchbarkeit bitte prüfen". Der Link `https://tickets.oebb.at` bleibt.
- Der Text wird in einer reinen Funktion (z. B. `buildNotificationText`) gebaut, die ohne Netz testbar ist.
- Fehler-Alarmtexte: "ÖBB API" wird zu "ÖBB Fahrplanauskunft" (Fehler- und Canary-Alarm).
- Verhalten der Watchlist nach erfolgreicher Meldung bleibt: Eintrag wird entfernt; bei Telegram-Fehler bleibt er bestehen.

### 7. Dokumentation

`CLAUDE.md` und `README.md`: Abschnitt "ÖBB API Flow" ersetzen durch den HAFAS-Ablauf (Endpunkt, LocMatch, TripSearch, Nightjet-Erkennung, Fehlerverhalten, Canary-Konfiguration). Die Hinweise auf Init/Token/Shop entfallen.

## Expected Behavior

- **Input:** Config mit Strecken/Daten und optionaler Canary-Strecke; Datum je Abfrage als `YYYY-MM-DD`.
- **Output:** `SearchConnections` liefert nur Nightjet-Direktverbindungen mit Abfahrtstag == Suchdatum, mit korrekter Abfahrts- und Ankunftszeit; sonst leere Liste. Telegram-Meldung bei Treffer; Heartbeat-Ping nur bei Erfolg.
- **Side effects:** HTTPS-POSTs an `fahrplan.oebb.at`, Telegram-Nachrichten, Heartbeat-GET, Log-Ausgaben.

## Error Handling

- HAFAS nicht erreichbar, HTTP != 200 oder `err != "OK"` (außer H890): Fehler an Aufrufer; Zähler `consecutiveErrors` greift (3 Fehler in Folge → Telegram-Fehleralarm), Watchlist-Eintrag bleibt, kein Heartbeat.
- `H890`: leeres Ergebnis, kein Fehler ("noch keine Direktverbindung").
- Stationsauflösung fehlschlägt (Watchlist oder Canary): kein Absturz, Retry im nächsten Zyklus, kein Heartbeat.
- Telegram-Versand schlägt fehl: Eintrag bleibt in der Watchlist, nächster Zyklus versucht erneut.

## Known Limitations

- Der Endpunkt `mgate.exe` samt AID ist inoffiziell. ÖBB kann AID/Version ändern oder den Zugriff sperren. Canary und Heartbeat fangen das ab (Alarm bzw. ausbleibender Ping), verhindern es aber nicht.
- Ein Fahrplan-Treffer ist kein Buchbarkeits-Nachweis. Die ÖBB-Webapp weist darauf hin, dass derzeit nur Nightjet-Verbindungen buchbar sind und der Fahrplan 2027 nicht abgeschlossen ist. Der Monitor entfernt den Watchlist-Eintrag nach der Meldung trotzdem (unverändertes Verhalten); wer buchen will, muss die Buchbarkeit selbst auf tickets.oebb.at prüfen.
- NJ 40421 fährt Amsterdam → Salzburg direkt nur bis 12.12.2026. Im Fahrplan 2027 (ab 13.12.) existiert derzeit keine Direktverbindung; der Monitor wartet auf deren Erscheinen für den 28.12.2026. Die ÖBB-Planung 2027 ist nicht abgeschlossen.
- Nur Direktverbindungen (`maxChg=0`); Verbindungen mit Umstieg werden bewusst nicht gemeldet.

## Definition of Done

Fertig ist diese Änderung, wenn:

- [ ] Jede Acceptance Criterion unten ist durch einen automatischen Test belegt
- [ ] `docker run --rm -v "$PWD":/app -w /app golang:1.26-alpine go test ./...` läuft grün, ohne dass ein Test das Live-Netz braucht
- [ ] `oebb.go` enthält keinen Token-, Init-, Shop- oder Mutex-Code mehr; `go build` ist über Docker erfolgreich
- [ ] `CLAUDE.md` und `README.md` beschreiben HAFAS statt "ÖBB API Flow" mit Token/Shop
- [ ] Betriebsschritt (kein Code): `config.yaml` im Hauptordner (gitignored) ist manuell auf Amsterdam Centraal → Salzburg Hbf, Datum 2026-12-28 angepasst (optional `canary`); erst danach wird der Container `nightjet-monitor` gestartet
- [ ] Ein einmaliger Lauf (`-once`) gegen das echte HAFAS zeigt im Log aufgelöste Stationen und einen grünen Canary (Wien Hbf → Innsbruck Hbf); der Heartbeat wird gepingt
- [ ] Keine bestehende Funktion ist dabei kaputtgegangen (Regressionslauf grün)

## Acceptance Criteria

- **AC-1:** Given ein HAFAS-Testserver liefert für Amsterdam → Salzburg am 2026-12-07 eine Antwort mit NJ 40421 (Abfahrt 18:01, Ankunft `01064600`) / When `SearchConnections` für `2026-12-07` aufgerufen wird / Then wird genau eine Connection `NJ 40421` mit Abfahrt 2026-12-07 18:01 und Ankunft 2026-12-08 06:46 zurückgegeben
  - Test: *(populated after TDD RED phase)*

- **AC-2:** Given die Antwort enthält zusätzlich Verbindungen mit Abfahrtstag nach dem gesuchten Datum / When `SearchConnections` für `2026-12-07` aufgerufen wird / Then werden nur Verbindungen mit Abfahrtstag 2026-12-07 zurückgegeben (Folgetage herausgefiltert)
  - Test: *(populated after TDD RED phase)*

- **AC-3:** Given der Testserver antwortet mit `svcResL[0].err = "H890"` / When `SearchConnections` aufgerufen wird / Then ist das Ergebnis leer und der Fehler `nil`
  - Test: *(populated after TDD RED phase)*

- **AC-4:** Given der Testserver antwortet mit HTTP 403, HTTP 5xx oder `err != "OK"` (Top-Level oder `svcResL[0].err`, nicht H890) / When `SearchConnections` oder `SearchStation` aufgerufen wird / Then wird ein Fehler zurückgegeben
  - Test: *(populated after TDD RED phase)*

- **AC-5:** Given die Antwort enthält am gesuchten Tag nur ein Nicht-Nightjet-Produkt (z. B. `catOutS = "RJX"`) / When `SearchConnections` aufgerufen wird / Then ist das Ergebnis leer und der Fehler `nil`
  - Test: *(populated after TDD RED phase)*

- **AC-6:** Given der Testserver liefert für `LocMatch` "Amsterdam Centraal" mit `extId` 8400058 / When `SearchStation("Amsterdam Centraal")` aufgerufen wird / Then ist `Station.Number == 8400058` und `Station.Name == "Amsterdam Centraal"`; bei leerer `locL` wird ein Fehler zurückgegeben
  - Test: *(populated after TDD RED phase)*

- **AC-7:** Given eine Config ohne `canary`-Block / When `LoadConfig` aufgerufen wird / Then ist die Canary-Strecke Wien Hbf → Innsbruck Hbf; Given ein `canary`-Block mit `from`/`to` / Then werden diese Werte übernommen; Given fehlende Pflichtfelder (Telegram-Daten oder Connections) / Then liefert `LoadConfig` weiterhin einen Fehler
  - Test: *(populated after TDD RED phase)*

- **AC-8:** Given eine Watchlist mit Strecke A → B und eine abweichende Canary-Strecke / When der Canary-Check läuft / Then fragt der Testserver für den Canary ausschließlich die Canary-Stationen ab (nicht `watchList[0]`), und die Canary-Stationen werden bei fehlgeschlagener Auflösung im nächsten Zyklus erneut aufgelöst
  - Test: *(populated after TDD RED phase)*

- **AC-9:** Given ein Zyklus mit Canary- oder Watchlist-Fehler bzw. nicht aufgelösten Canary-Stationen / When der Zyklus läuft / Then wird der Heartbeat-Testserver nicht gepingt; Given fehlerfreier Zyklus / Then wird genau einmal gepingt (Regression des bestehenden Verhaltens)
  - Test: *(populated after TDD RED phase)*

- **AC-10:** Given eine Connection / When der Telegram-Text gebaut wird / Then enthält er "Direktverbindung im Fahrplan gefunden" und den Hinweis "Buchbarkeit bitte prüfen", enthält nicht "jetzt buchbar" und enthält den Link `https://tickets.oebb.at`; die Fehler-Alarmtexte nennen "ÖBB Fahrplanauskunft" statt "ÖBB API"
  - Test: *(populated after TDD RED phase)*

## Test Plan

Go-Tests, ausgeführt über Docker (Go ist auf dem Host nicht installiert):

- `docker run --rm -v "$PWD":/app -w /app golang:1.26-alpine go test ./...`
- `oebb_test.go`: `httptest`-Server mit aufgezeichneten HAFAS-Antworten aus `testdata/` (Treffer 07.12.2026, Folgetag-Treffer, H890, Nicht-Nightjet, Fehler-Antworten, HTTP 403/503, LocMatch mit/ohne Treffer) — deckt AC-1 bis AC-6.
- `config_test.go`: Config-Dateien in `t.TempDir()` — AC-7.
- `main_test.go`: Canary- und Zyklus-Logik gegen `httptest`-HAFAS und `httptest`-Heartbeat — AC-8, AC-9.
- `notify_test.go`: Textbau-Funktionen ohne Netz — AC-10.
- Kein Test darf das Live-Netz (`fahrplan.oebb.at`, Telegram, BetterStack) benötigen.

## Architektur-Entscheidung (ADR)

- **ADR-Nr.:** keine
- **Rationale:** Es ist ein Austausch der Datenquelle innerhalb eines kleinen Einzelservices bei gleichbleibender Struktur (Signaturen, Heartbeat, Canary bleiben). Es entsteht keine neue Architektur, die später schwer änderbar wäre; die Gründe (Cloudflare-Sperre des Shops, HAFAS-Befund) sind in `docs/context/hafas-umstellung.md` dokumentiert.

## Changelog

- 2026-10-02: Initial spec created
