# Adversary Dialog: bug-5-treffer-einmalig

Tests: go vet ./... sauber, go test -count=1 -v ./... via Docker (golang:1.26-alpine): 47 PASS, 0 FAIL in Runde 1-2; Runde 3: 49 PASS, 0 FAIL (Output: docs/artifacts/bug-5-treffer-einmalig/adversary-test-output.txt).
Probes: temporaere Datei zz_probe_test.go im Worktree (10 Probes P1-P10), ausgefuehrt und wieder geloescht. Mutationstests (M1-M8) nur in einer Kopie im Scratchpad, Produktivcode unveraendert.
Hinweis: .claude/hooks/adversary_dialog.py existiert im Worktree nicht; die Liste der geaenderten Dateien wurde aus dem Arbeitsstand des Worktrees abgeleitet (config.go, main.go, main_test.go, notified.go, notified_test.go, dazu config_test.go laut Auftrag).

### Runde 1: ACs

Confirmation: AC-1 - AKZEPTIERT
Code reference: main.go:49
Evidence: newMonitor filtert die aufgeloeste Watchlist gegen den Store (filterNotified, main.go:132). TestTreffer_NotifiedOnceAcrossRestart (main_test.go:276): zwei Monitore, eine Datei, genau 1 Telegram-Aufruf, Watchlist des zweiten leer. Mutation M1 (alle drei filterNotified-Aufrufe entfernt) laesst diesen Test und AC-3 fehlschlagen, also nicht tautologisch.
Status: CONFIRMED

Confirmation: AC-2 - AKZEPTIERT
Code reference: notified.go:53
Evidence: add speichert Config-Namen und Datum, notified_at per time.RFC3339; save schreibt JSON-Array (notified.go:59). TestTreffer_NotifiedFileContent (main_test.go:295) parst die Datei unabhaengig per json.Unmarshal und time.Parse(RFC3339).
Status: CONFIRMED

Confirmation: AC-3 - AKZEPTIERT
Code reference: main.go:110
Evidence: Leere, initialisierte Watchlist liefert saveOK (true) ohne checkAll; watchListInitialized wird vor dem Filtern gesetzt (main.go:48). TestTreffer_AllNotifiedStillHealthy (main_test.go:329): 0 Meldungen, Erfolgsdatei vorhanden, watchListInitialized true.
Status: CONFIRMED

Confirmation: AC-4 - AKZEPTIERT
Code reference: main.go:322
Evidence: notifyHit gibt bei Versandfehler (false,false) zurueck, vor store.add; checkAll behaelt den Eintrag (main.go:302) und setzt deliveryOK=false. TestTreffer_TelegramFailureNoSuccess (main_test.go:354) mit HTTP 401. Mutation M6 (ok=true bei Versandfehler) wird erkannt.
Status: CONFIRMED

Confirmation: AC-5 - AKZEPTIERT
Code reference: main.go:328
Evidence: Speicherfehler nach Versand liefert (true,false): Eintrag verlaesst die Watchlist, Zyklus nicht erfolgreich. TestTreffer_SaveFailureNoSuccess (main_test.go:375), nicht existierendes Verzeichnis. Mutation M4 wird erkannt.
Status: CONFIRMED

Confirmation: AC-6 - AKZEPTIERT (mit Testluecke F002)
Code reference: main.go:96
Evidence: ensureNotified scheitert, runWatchCheck gibt false ohne Pruefung zurueck; Canary laeuft trotzdem (main.go:79). Ladeversuch in jedem Zyklus, da m.notified nil bleibt (main.go:117). TestTreffer_CorruptNotifiedFileBlocksUntilFixed (main_test.go:397). Mutation M5 (Ladefehler = Erfolg) wird erkannt.
Status: CONFIRMED

Confirmation: AC-7 - AKZEPTIERT
Code reference: main.go:380
Evidence: Scheitert sendTelegram, return false ohne alerted=true (main.go:382). Naechster Zyklus: failures >= 3 und !alerted, neuer Versuch. TestTreffer_CanaryAlertFailureNoSuccess (main_test.go:428) mit HTTP 500. Mutation M3 wird erkannt. Probe P6: Alarm scheitert, dann klappt er im 4. Zyklus, Erfolgsdatei wird geschrieben, kein dritter Alarm (alerted=true).
Status: CONFIRMED

Confirmation: AC-8 - AKZEPTIERT
Code reference: config.go:63
Evidence: Feld im Roh-Struct (config.go:49) und Uebernahme. TestLoadConfig_NotifiedFile (config_test.go:28) prueft /data/x und leeres Feld ohne Fehler.
Status: CONFIRMED

Confirmation: AC-9 - AKZEPTIERT
Code reference: notified.go:59
Evidence: Leerer Pfad: loadNotified liefert leeren Store (notified.go:26), save ist No-op und setzt dirty=false (notified.go:61). TestTreffer_InMemoryWithoutNotifiedFile (main_test.go:456): 2 Zyklen, 1 Meldung. TestNotified_EmptyPathSaveIsNoop (notified_test.go:66).
Status: CONFIRMED

Confirmation: AC-10 - AKZEPTIERT (Code-Review)
Code reference: main.go:194
Evidence: Scheduler-Schleife kennt nur zwei Ausgaenge: ctx.Done (SIGTERM/SIGINT, main.go:196) und den -once-Modus (main.go:177). Der alte Block "All connections notified ... Exiting." (Vorversion main.go:144-146) ist entfernt; Suche nach Exiting/os.Exit in main.go leer. Einziger log.Fatalf betrifft LoadConfig (main.go:171).
Status: CONFIRMED

Confirmation: Expected Behavior / Error-Handling-Tabelle - AKZEPTIERT (ausser F001)
Code reference: main.go:86
Evidence: Erfolg nur bei checkOK und canaryAPIOK. Fehlt: leerer Store (notified.go:32, TestNotified_MissingFileIsEmpty notified_test.go:17). Kaputt: Fehler (TestNotified_CorruptFileIsError notified_test.go:27). Treffer-Versand scheitert / Speichern scheitert / Canary-Alarm scheitert: siehe AC-4, AC-5, AC-7. SendTelegramError-Fehler: unveraendert, Zyklus wegen hadError ohnehin false (main.go:316). notified_file wird angelegt/aktualisiert (TestNotified_SaveAndReload notified_test.go:37).
Status: CONFIRMED

Confirmation: Zusatz dirty-Retry - AKZEPTIERT
Code reference: main.go:102
Evidence: Ist der Store dirty, wird jeden Zyklus save erneut versucht; bis es klappt saveOK=false, auch bei leerer Watchlist (main.go:111). dirty wird nur bei erfolgreichem save zurueckgesetzt (notified.go:81), bei leerem Pfad sofort (notified.go:61), dadurch kein dauerhaftes kein-Erfolg ohne notified_file (Probe P5: Zyklus 1 und 2 erfolgreich, dirty=false). TestTreffer_SaveFailureRetriedUntilFixed (main_test.go:472) und TestNotified_DirtyFlag (notified_test.go:80). Mutation M2 (dirty-Retry abgeschaltet) wird erkannt.
Status: CONFIRMED

### Runde 2: Edge Cases (Probes P1-P10, Mutationen M1-M8)

Probe-Ergebnisse:
- P1: Store kaputt beim Start, Watchlist schon aufgebaut; im 2. Zyklus repariert mit bereits gemeldetem Eintrag: 0 Meldungen, Watchlist leer, Erfolgsdatei. Korrekt dank Filter in runWatchCheck (main.go:109).
- P2: Stationsaufloesung scheitert beim Start, Store enthaelt Eintrag; Retry-Pfad (main.go:69) filtert: init=true, 0 Meldungen, Erfolg.
- P3: Zwei Daten, eines gemeldet: nur das andere gemeldet, Datei mit 2 Eintraegen, Erfolg.
- P4: Treffer und HAFAS-Fehler im selben Zyklus: Treffer gemeldet und gespeichert, fehlerhafter Eintrag bleibt, kein Erfolg.
- P5: leerer notified_file-Pfad: dirty bleibt nicht haengen, beide Zyklen erfolgreich.
- P6: Canary-Alarm scheitert, dann Erfolg (siehe AC-7).
- P7: veraltete .tmp-Datei: wird ueberschrieben und umbenannt, keine .tmp-Reste (notified.go:72-78).
- P8: doppelte Von/Nach/Datum-Kombination in der Config: 2 Meldungen (siehe F001).
- P9: 0-Byte-Datei = Fehler (unexpected end of JSON input), null = leerer Store. Konsistent mit Spec (ungueltiges JSON = Fehler); durch atomares Rename entsteht im Normalbetrieb keine 0-Byte-Datei.
- P10: Pfad ist ein Verzeichnis = Ladefehler (is a directory), Verhalten wie kaputte Datei.
- Nebenlaeufigkeit: alles in einer Goroutine (runCheck aus der Ticker-Schleife), kein Race.

Tautologie-Check (Mutanten in Scratchpad-Kopie):
- M1 alle Filter entfernt: AC-1, AC-3 rot. M2 dirty-Retry aus: Erweiterungstest rot. M3/M4/M6 Erfolgs-Rueckgaben verfaelscht: AC-4, AC-5, AC-7, Erweiterung rot. M5 Ladefehler = Erfolg: AC-6 rot.
- M8 nur den Filter in runWatchCheck (main.go:109) entfernt: Suite bleibt GRUEN (siehe F002).

Finding:
  ID: F001
  Severity: MEDIUM
  Category: spec_violation
  Code reference: main.go:301
  Description: checkAll ruft notifyHit fuer jeden Watchlist-Eintrag, ohne vorher store.contains zu pruefen, und weder resolveStations noch filterNotified dedupliziert. Enthaelt config.yaml dieselbe Von/Nach/Datum-Kombination doppelt (realistisch: zwei connections-Bloecke mit gleicher Strecke und ueberlappendem Datum), werden im selben Zyklus 2 Treffermeldungen gesendet und 2 identische Eintraege gespeichert (Probe P8: tg=2, entries=2). Vorbestehendes Verhalten, aber die Spec verspricht die Invariante jetzt ausdruecklich.
  Spec requirement: Expected Behavior - hoechstens eine Telegram-Treffermeldung pro Von/Nach/Datum ueber die gesamte Lebensdauer der Notified-Datei. Known Limitations nennt Duplikate nicht als Ausnahme.
  Conflict: Mit einer fehlerhaften, aber ladbaren Config wird dieselbe Kombination zweimal gemeldet.
  Remediation: In notifyHit vor dem Versand pruefen, ob store.contains schon true liefert (dann sent=true, ok=true ohne Versand), oder Duplikate in resolveStations/filterNotified entfernen. Test mit zwei gleichen Connections ergaenzen.
Code reference: notified.go:44
Evidence: contains existiert, wird aber nur in filterNotified (main.go:138) verwendet, nicht im Versandpfad.
Status: OPEN

Finding:
  ID: F002
  Severity: LOW
  Category: edge_case
  Code reference: main.go:109
  Description: Der Doppelmeldungsschutz fuer den Fall "Store erst nach Aufbau der Watchlist ladbar und enthaelt den Eintrag bereits" ist korrekt implementiert (Probe P1), aber durch keinen Test abgedeckt. Mutation M8 (Zeile entfernt) laesst alle 47 Tests gruen.
  Spec requirement: AC-6 / Abschnitt 3 - solange der Store nicht geladen ist keine Treffer-Meldung (Schutz vor Doppelmeldung)
  Conflict: Kein Verhaltensfehler; eine Regression an dieser Stelle bliebe unbemerkt.
  Remediation: In TestTreffer_CorruptNotifiedFileBlocksUntilFixed (main_test.go:397) zusaetzlich: Reparatur mit bereits gemeldetem Eintrag, Erwartung 0 Meldungen und Erfolgsdatei.
Code reference: main_test.go:415
Evidence: Der Test repariert die Datei nur mit leerem Array, daher wird der Filter in runWatchCheck nie gebraucht.
Status: OPEN

Finding:
  ID: F003
  Severity: LOW
  Category: anti_pattern
  Code reference: notified_test.go:53
  Description: Atomaritaet von save wird nur indirekt geprueft (keine .tmp-Reste). Ein nicht-atomares direktes Schreiben wuerde alle Tests bestehen. Code selbst ist korrekt (notified.go:72-78: WriteFile auf .tmp, dann Rename, Aufraeumen bei Fehlern).
  Spec requirement: Abschnitt 2 - save atomar (tmp + rename)
  Conflict: kein Verhaltensfehler, nur Testtiefe.
  Remediation: optional; per Code-Review abgenommen.
Status: OPEN

Weitere geprueft:
Code reference: config.go:36
Code reference: config_test.go:28
Code reference: notified.go:26
Code reference: notified_test.go:80
Code reference: main_test.go:472
Code reference: main.go:117

Zwischenstand nach Runde 2: BROKEN (ueberholt, siehe Runde 3)
Alle AC-1 bis AC-10, die Error-Handling-Tabelle und der dirty-Retry-Zusatz sind belegt und mutationsgetestet. Gebrochen ist die Expected-Behavior-Invariante "hoechstens eine Treffermeldung pro Von/Nach/Datum" bei doppelter Kombination in der Config (F001, MEDIUM, vorbestehend, kleiner Fix). F002/F003 sind LOW-Testluecken ohne Verhaltensfehler.

### Runde 3: Verifikation der Fixes

Testlauf: go vet ./... sauber, go test -count=1 -v ./... via Docker: 49 PASS, 0 FAIL (adversary-test-output.txt neu geschrieben). Mutanten in frischer Scratchpad-Kopie, Probes Q1-Q3 als temporaere zz_probe_test.go, danach geloescht.

Finding:
  ID: F001
  Severity: MEDIUM
  Category: spec_violation
  Code reference: main.go:322
  Description: notifyHit prueft jetzt zuerst store.contains und gibt (true,true) ohne Versand zurueck. Da add vor save passiert (main.go:330), greift der Schutz auch bei Speicherfehler.
  Spec requirement: Expected Behavior - hoechstens eine Treffermeldung pro Von/Nach/Datum
  Conflict: behoben.
  Remediation: umgesetzt.
Code reference: main_test.go:507
Evidence: TestTreffer_DuplicateConfigEntryNotifiedOnce (doppeltes Datum): 1 Meldung, 1 Eintrag, Erfolgsdatei. F001-Mutante (Bedingung in main.go:322 durch false ersetzt) macht genau diesen Test rot. Probes: Q1 zwei identische connections-Bloecke: 1 Meldung, 1 Eintrag, Erfolg. Q2 Duplikat plus Speicherfehler, 2 Zyklen: 1 Meldung, Watchlist leer, kein Erfolg. Q3 Duplikat plus Telegram 401: 2 Zustellversuche, beide scheitern, beide Eintraege bleiben, nichts gespeichert, kein Erfolg (keine Doppelmeldung moeglich, da nichts zugestellt). Der Guard kann keinen echten neuen Treffer verschlucken, denn ein im Store stehender Schluessel waere ohnehin herausgefiltert worden (main.go:138 nutzt notified.go:44).
Status: RESOLVED

Finding:
  ID: F002
  Severity: LOW
  Category: edge_case
  Code reference: main.go:109
  Description: Filter in runWatchCheck jetzt durch Test abgedeckt.
  Spec requirement: AC-6 / Abschnitt 3 - Schutz vor Doppelmeldung, solange der Store nicht geladen war
  Conflict: behoben.
  Remediation: umgesetzt.
Code reference: main_test.go:536
Evidence: TestTreffer_RepairedNotifiedFileFiltersBeforeCheck: kaputt beim Start, vor dem ersten Zyklus mit gemeldetem Eintrag repariert; erwartet 0 Meldungen, keine TripSearch auf der Watch-Strecke (lid 8400058), Erfolgsdatei. Die Pruefung auf keine Abfrage ist noetig, weil der F001-Guard sonst das Fehlen des Filters maskieren wuerde. Mutante M8 (main.go:109 geloescht) macht genau diesen Test rot (selbst gegengeprueft).
Status: RESOLVED

Finding:
  ID: F003
  Severity: LOW
  Category: anti_pattern
  Code reference: notified.go:77
  Description: Unveraendert bewusst belassen; tmp-plus-Rename-Code korrekt, nur Testtiefe.
  Spec requirement: Abschnitt 2 - save atomar
  Conflict: kein Verhaltensfehler.
  Remediation: keine (akzeptiert).
Status: ACCEPTED

Regressionen: keine. Alle Round-1-Tests weiterhin gruen; Zeilen in main.go ab notifyHit um 3 verschoben (z. B. Canary-Alarm jetzt main.go:383/385), Verhalten unveraendert.
Weitere geprueft:
Code reference: config.go:63
Code reference: config_test.go:28
Code reference: notified.go:26
Code reference: notified_test.go:80
Code reference: main.go:383

VERDICT: VERIFIED
Alle AC-1 bis AC-10, Expected Behavior, Error-Handling-Tabelle und dirty-Retry belegt; F001 und F002 behoben und per Mutation gegengeprueft (je genau ein neuer Test wird rot), F003 als LOW akzeptiert. 49 PASS, 0 FAIL, go vet sauber.

## Geprüfte Dateien

- sha256:462ef668a13a37d01b2ac78b2b865aa0be71cb31d8a251c0f290db084e881186  config.go
- sha256:258e560cf294fa8192c6f93bd555888828a4685ed929c506cc7287c09e3cc76d  config_test.go
- sha256:c0796c58b0744b7acf3d7969ce61f6ec81c82cc9edd93e90ba0ec7f5055b6ec2  main.go
- sha256:c9c40b2b674d412472336765f4ce5b39db215ea638f500fd0787e7e6ad092abf  main_test.go
- sha256:c4df80fd3dd97b0c67b709dc81acfa37b69e8883e43c9542ea072b9820ad0dfc  notified.go
- sha256:78abdb8f8954128f2dc543cee8747199d79e75da723a8978e0e32efafafe26a6  notified_test.go
