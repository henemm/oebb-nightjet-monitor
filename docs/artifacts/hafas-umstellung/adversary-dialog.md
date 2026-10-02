# Adversary Dialog: hafas-umstellung

Tests: `go test ./... -v` via Docker: alle PASS (Output: docs/artifacts/hafas-umstellung/adversary-test-output.txt). `go vet ./...` sauber. `-race` nicht ausfuehrbar (golang:alpine ohne cgo) - als Luecke vermerkt.
Live-Check (1x curl): Wien Hbf -> Innsbruck Hbf 2026-10-07 = HTTP 200, 2 NJ (NJ 13486, NJ 19946); Amsterdam -> Salzburg 2026-12-28 = H890 (erwartet leer). Die echte Wien-Innsbruck-Antwort wurde in einer Probe-Kopie (nur Scratchpad) durch SearchConnections geschickt: korrekte Namen/Zeiten (21:39 -> 04:27 Folgetag), 06.10. und Folgetage korrekt gefiltert.

### Runde 1: ACs

Confirmation: AC-1 - AKZEPTIERT
Code reference: oebb.go:214
Evidence: TestSearchConnections_RealRecording_OnlyRequestedDay (oebb_test.go:27) nutzt aufgezeichnete echte Antwort; erwartet NJ 40421, 07.12. 18:01 und 08.12. 06:46 (Europe/Vienna). hafasTime (oebb.go:325) wertet 8-stelliges DDHHMMSS aus. Nicht tautologisch: Fixture ist unabhaengig, Zeit-Erwartung per time.Date.
Status: CONFIRMED

Confirmation: AC-2 - AKZEPTIERT
Code reference: oebb.go:275
Evidence: Filter conn.Date != outDate; Test oebb_test.go:69 (3 Tage, nur der 08.12.) plus Real-Fixture.
Status: CONFIRMED

Confirmation: AC-3 - AKZEPTIERT
Code reference: oebb.go:236
Evidence: H890 wird in call() (oebb.go:140) durchgelassen, SearchConnections gibt nil,nil. TestSearchConnections_H890IsEmptyNotError. Live-Check bestaetigt: echtes H890 enthaelt ein res-Objekt, das vor dem Parsen verworfen wird.
Status: CONFIRMED

Confirmation: AC-4 - AKZEPTIERT (mit Luecke F004)
Code reference: oebb.go:117
Evidence: HTTP!=200, Top-Level err, svcResL err, kaputtes JSON, fehlendes/null svcResL liefern Fehler (Tabelle oebb_test.go:102 plus eigene Probe: leerer Body, svc err leer, CGI_READ). SearchStation nur fuer HTTP 403 getestet; Top-Level/H890 bei LocMatch per Probe = Fehler (oebb.go:172).
Status: CONFIRMED

Confirmation: AC-5 - AKZEPTIERT
Code reference: oebb.go:289
Evidence: TestSearchConnections_IgnoresNonNightjet (RJX), zusaetzlich catOutL-Pfad getestet. prodX ausserhalb prodL wird sicher uebersprungen (oebb.go:285, Probe: leer, kein Panic).
Status: CONFIRMED

Confirmation: AC-6 - AKZEPTIERT
Code reference: oebb.go:160
Evidence: TestSearchStation_Resolves (8400058 / Amsterdam Centraal), EmptyResultIsError, HTTPErrorIsError. Nicht-numerische extId = Fehler (Probe).
Status: CONFIRMED

Confirmation: AC-7 - AKZEPTIERT
Code reference: config.go:89
Evidence: config_test.go: Default, Uebernahme, Teilfeld -> Default, Pflichtfelder.
Status: CONFIRMED

Confirmation: AC-8 - AKZEPTIERT
Code reference: main.go:169
Evidence: TestCanary_UsesOwnRoute_NotFirstWatchEntry zaehlt 8 Canary-Abfragen auf Canary-lids und 1 auf Zielstrecke; TestCanary_LazyResolve_RetriesAndNoHeartbeatUntilResolved loest im 2. Zyklus erneut auf (main.go:71).
Status: CONFIRMED

Confirmation: AC-9 - AKZEPTIERT
Code reference: main.go:82
Evidence: Heartbeat nur bei checkOK && canaryAPIOK; Tests: Healthy=1 Ping, Watchlist-Fehler=0, Canary komplett down=0, Canary unresolved=0. canaryAPIOK=false bei nil-Canary (main.go:75).
Status: CONFIRMED

Confirmation: AC-10 - AKZEPTIERT
Code reference: notify.go:24
Evidence: buildNotificationText enthaelt Direktverbindung im Fahrplan gefunden, Buchbarkeit bitte pruefen, https://tickets.oebb.at; Fehler-/Canary-Texte (notify.go:39,47) nennen OeBB Fahrplanauskunft. Tests notify_test.go.
Status: CONFIRMED

Confirmation: Expected Behavior / Error Handling (13 Punkte) - AKZEPTIERT
Code reference: main.go:204
Evidence: checkAll zaehlt Fehler, Alarm bei 3 in Folge, Eintrag bleibt; Telegram-Fehler behaelt Eintrag (TestWatchHit_TelegramFailureKeepsEntry). Kein log.Fatal ausser LoadConfig.
Status: CONFIRMED

Confirmation: Doku/DoD - AKZEPTIERT
Code reference: CLAUDE.md:13
Evidence: oebb.go enthaelt kein Token/Init/Shop/Mutex (Suche leer). CLAUDE.md und README.md (README.md:46) beschreiben HAFAS. config.yaml ist ausgeschlossen, keine Secrets in den versionierten Dateien. Dockerfile bringt tzdata mit; oebb.go:12 bettet time/tzdata ein.
Status: CONFIRMED

### Runde 2: Edge Cases (Probes im Scratchpad)

- aTimeS HHMMSS / DDHHMMSS / Zeitumstellung (28.03.2027, 25.10.2026): korrekt in Europe/Vienna.
- Mehrere Sections: erster NJ-Abschnitt zaehlt (oebb.go:280).
- Falsches Datumsformat: Fehler, kein Panic.

Finding:
  ID: F001
  Severity: LOW
  Category: edge_case
  Code reference: config.go:61
  Description: UnmarshalYAML ruft time.ParseDuration auch bei fehlendem check_interval; ParseDuration("") schlaegt fehl, der Default 60m (config.go:86) ist unerreichbar. Vorbestehend (identisch im Vorgaenger-Commit), Spec sagt Default bleibt.
  Spec requirement: Abschnitt 5 - check_interval-Default bleibt 60 Minuten
  Conflict: Config ohne check_interval wird abgelehnt (Probe bestaetigt).
  Remediation: Leeren String nicht parsen.

Finding:
  ID: F002
  Severity: LOW
  Category: edge_case
  Code reference: oebb.go:301
  Description: Eine einzelne Nightjet-Verbindung mit leerem/falschem dTimeS/aTimeS laesst die ganze Suche mit Fehler abbrechen (Probe: arr leer -> parsing arrival error). Laut, nicht still.
  Spec requirement: Abschnitt 4 - nicht explizit geregelt
  Conflict: kein Verstoss, Robustheitsluecke.
  Remediation: Defekte Verbindung loggen und ueberspringen.

Finding:
  ID: F003
  Severity: LOW
  Category: edge_case
  Code reference: main.go:141
  Description: resolveStations ueberspringt fehlgeschlagene Connections stillschweigend; sobald eine andere aufgeloest ist, gilt watchListInitialized und die uebersprungene wird nie erneut aufgeloest. Vorbestehend; bei der Zielkonfiguration (1 Strecke) ohne Wirkung.
  Spec requirement: Error Handling - Stationsaufloesung fehlschlaegt: Retry im naechsten Zyklus
  Conflict: gilt nur teilweise (alles-oder-nichts).
  Remediation: Pro Connection erneut aufloesen.

Finding:
  ID: F004
  Severity: LOW
  Category: anti_pattern
  Code reference: oebb_test.go:179
  Description: Testluecken: SearchStation nur fuer HTTP-Fehler getestet, kein Test fuer Canary-Alarm nach 3 Fehlzyklen, kein Test fuer Zeitumstellung/Mitternacht, -race nicht lauffaehig. Verhalten per Probe als korrekt belegt.
  Spec requirement: AC-4 / Canary-Logik
  Conflict: kein Verhalten falsch.
  Remediation: Tests ergaenzen.

Weitere geprueft:
Code reference: main.go:259
Code reference: notify.go:65
Code reference: main_test.go:50
Code reference: notify_test.go:1
Code reference: config_test.go:1
Code reference: hafas_fake_test.go:1
Code reference: README.md:46
Code reference: oebb_test.go:27

VERDICT: VERIFIED
Alle AC-1 bis AC-10 und Expected Behavior/Error Handling belegt; nur LOW-Findings (F001 und F003 vorbestehend).

## Geprüfte Dateien

- sha256:1982523fc316b39e58d44317b2f6b568bfb3e518ded6cedb4bad03ba44fd04e3  CLAUDE.md
- sha256:5c5258f1413a4e4aec5d3f6de380a941f442c8db0844052aa98c0359ed823f71  README.md
- sha256:bc1e7871c63ce4bff3cc0d74201008bf389db59a65691d042610aded5b026697  config.go
- sha256:9cdcd3fb1b23c8f06a2b08992c3f0e92076130696af3fcb79d5ca6c555d8e20e  config_test.go
- sha256:795cf912bcfa19588b2f272c46e3d7491f0fae1801b9d96f7fac0fcf65d57dab  hafas_fake_test.go
- sha256:94cdfa4ead7c57ea716b520bcfc743fd212bdf71f41c5a3419d5f6be681dace8  main.go
- sha256:e849f1c22ac895f4cbba9f496dcf536d0914cf5276d02e480fd22edab49ecd79  main_test.go
- sha256:31e76f41d5a1ad86e6db155f053ecbef5619070663649cc238ba183db4cb9ca7  notify.go
- sha256:2d495638964724241c7275838a633aadd6cb25b481aedebfb4beca5fca5270b3  notify_test.go
- sha256:f96ff1ebe692513327bc24ee0259fb63ac5bab89a89ee2d57c6765b2cdfcb376  oebb.go
- sha256:8d99489dcd5d99d43876416811f9b0110be3874901960d2f7ce85604642aa2e3  oebb_test.go
