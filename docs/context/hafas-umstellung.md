# Context: hafas-umstellung

## Request Summary
Der Monitor soll auf die öffentliche ÖBB-Fahrplanauskunft (HAFAS, `fahrplan.oebb.at/bin/mgate.exe`) umgestellt werden, weil der Ticketshop (`shop.oebbtickets.at`) seit Herbst 2026 per Cloudflare mit 403 blockt. Beobachtet wird die Direktverbindung Amsterdam Centraal → Salzburg am 28.12.2026.

## Befund (gemessen am 2026-10-02 vom Server aus)
- `tickets.oebb.at/api/domain/v4/init` leitet auf `shop.oebbtickets.at` um, dort 403 "Zugriff vorübergehend eingeschränkt" (Cloudflare, auch mit Browser-UA). Der alte Ablauf (Token holen, Station, Timetable) ist damit tot.
- `fahrplan.oebb.at/bin/mgate.exe` ist ohne Sperre erreichbar (LocMatch, TripSearch).
- TripSearch mit `maxChg=0` und Produktfilter `2762` (= Webapp "Nur Direktverbindungen") reproduziert die Webapp exakt: 07.12.2026 Treffer (NJ 40421), 14.12. und 28.12.2026 `H890` = keine Verbindung.
- NJ 40421 fährt Amsterdam→Salzburg direkt nur bis 12.12.2026; im Fahrplan 2027 (ab 13.12.) existiert bisher keine Direktverbindung.
- Der Fahrplan ist kein Buchbarkeits-Nachweis. ÖBB-Hinweis in der Webapp: "Derzeit sind nur ÖBB Nightjet-Verbindungen buchbar" (Fahrplan 2027 noch nicht abgeschlossen).

## Related Files
| File | Relevance |
|------|-----------|
| oebb.go | Kompletter ÖBB-Client — wird auf HAFAS umgeschrieben (kein Token mehr, LocMatch + TripSearch) |
| main.go | Scheduler, Canary, Heartbeat — nutzt `Station`, `SearchStation`, `SearchConnections` |
| config.go | Config laden — neues Feld `canary` (eigene Referenzstrecke) |
| notify.go | Telegram-Text anpassen ("im Fahrplan gefunden, Buchbarkeit prüfen") |
| config.yaml (gitignored, Hauptordner) | Strecke auf Amsterdam→Salzburg, 2026-12-28 |
| CLAUDE.md, README.md | Architektur-Beschreibung veraltet (ÖBB API Flow) |

## Existing Patterns
- Heartbeat nur bei fachlichem Erfolg (checkOK && canaryAPIOK) — bleibt.
- Kein log.Fatal bei Ausfall externer APIs, lazy Retry der Stationsauflösung — bleibt.
- Canary: Referenzstrecke, ≥50 % der erfolgreich abgefragten Tage müssen NJ liefern, sonst Telegram-Alarm.

## Entscheidungen
- Canary-Strecke konfigurierbar, Default Wien Hbf → Innsbruck Hbf: täglich 2–3 Direkt-Nightjets, gemessen auch nach dem Fahrplanwechsel (Dez 2026, Jan 2027). Die frühere Canary-Strecke (= Zielstrecke) taugt nicht, weil dort ab 13.12. keine Direktverbindung existiert.
- Direktverbindungen via `maxChg=0`; Treffer werden zusätzlich auf Abfahrtstag == gesuchtes Datum gefiltert (TripSearch liefert Folgetage mit).
- `H890` ist ein leeres Ergebnis, kein Fehler.

## Dependencies
- Upstream: `fahrplan.oebb.at` (inoffizieller, aber öffentlich genutzter Webapp-Endpunkt mit Client-AID der Webapp), Telegram API, BetterStack-Heartbeat.
- Downstream: Docker-Container `nightjet-monitor` (derzeit gestoppt).

## Risks & Considerations
- Inoffizielle Schnittstelle: ÖBB kann AID/Version ändern oder ebenfalls sperren → Canary + Heartbeat fangen das ab.
- Meldung bedeutet "Direktverbindung im Fahrplan", nicht sicher "buchbar" — Telegram-Text muss das ehrlich sagen.
- Auf dem Host ist kein Go installiert; Build/Test via Docker (`golang:1.26-alpine`).
- Infra-Nachrichten (Stalwart-SMTP, Heartbeat-Timer) sind NICHT Teil dieses Workflows.

## Analysis

### Type
Feature (Umbau der Datenquelle; gehört zu Issue #1 „Reaktivierung geplant ~November 2026“ — die Reaktivierung ist ohne diese Umstellung nicht möglich, weil der Ticketshop per Cloudflare blockt).

### Affected Files (with changes)
| File | Change Type | Description |
|------|-------------|-------------|
| oebb.go | MODIFY (Rewrite) | Token/Init/Shop-API entfernen; HAFAS-Client (`LocMatch`, `TripSearch` mit `maxChg=0` + Produktfilter 2762), Datumsfilter auf Abfahrtstag, `H890` = leer statt Fehler |
| oebb_test.go | CREATE | Tests mit aufgezeichneten HAFAS-Antworten (Treffer, H890, Folgetag-Treffer, Fehler/403) via `httptest` |
| config.go | MODIFY | Neues optionales Feld `canary` (from/to), Default Wien Hbf → Innsbruck Hbf |
| main.go | MODIFY | Canary nutzt eigene Strecke statt `watchList[0]`; Stationsauflösung für Canary separat; sonst Ablauf unverändert |
| notify.go | MODIFY | Text „Direktverbindung im Fahrplan gefunden — Buchbarkeit prüfen“ statt „jetzt buchbar“ |
| CLAUDE.md, README.md | MODIFY | Abschnitt „ÖBB API Flow“ ersetzen |
| config.yaml (gitignored, Hauptordner) | MODIFY (manuell) | Amsterdam Centraal → Salzburg Hbf, 2026-12-28, ggf. `canary` |

### Scope Assessment
- Files: 6 Code/Doku + 1 neue Testdatei (+ lokale config.yaml)
- Estimated LoC: +300/-150 (oebb.go dominiert)
- Risk Level: MEDIUM — inoffizielle Schnittstelle, kann sich ohne Ankündigung ändern. Gemildert durch Canary + Heartbeat-bei-Erfolg (bestehende Mechanik).

### Technical Approach
1. HAFAS-Client in `oebb.go` neu: ein POST-Endpunkt `fahrplan.oebb.at/bin/mgate.exe`, JSON-Envelope (Client-Block mit Webapp-AID/Version, `svcReqL` mit `LocMatch` bzw. `TripSearch`). Kein Token, kein Refresh, kein Mutex mehr nötig.
2. Schnittstellen `Station`, `SearchStation`, `SearchConnections`, `Connection` bleiben in Namen/Signatur erhalten, damit `main.go` nur minimal angefasst wird.
3. Direktverbindung: `maxChg=0` + Produktfilter 2762. Treffer zusätzlich auf Abfahrtstag == gesuchtes Datum filtern (TripSearch liefert Folgetage mit).
4. Nightjet-Erkennung aus HAFAS-Produktdaten (Produktklasse/Name „NJ …“) statt der alten Category-Felder.
5. Canary: eigene konfigurierbare Referenzstrecke (Default Wien Hbf → Innsbruck Hbf), Logik (≥50 % der Tage, 3 Fehlzyklen → Alarm) unverändert.
6. Telegram-Text ehrlich: Fahrplan-Treffer ≠ buchbar.
7. Build/Test nur via Docker (`golang:1.26-alpine`), da Go auf dem Host fehlt.

### Dependencies
- Upstream: `fahrplan.oebb.at` (inoffiziell), Telegram API, BetterStack-Heartbeat.
- Intern: `main.go` nutzt `Station`, `SearchStation`, `SearchConnections`, `watchEntry`; `notify.go` nutzt `Connection`.
- Nicht Teil dieses Workflows: Infra-Nachrichten (Stalwart-SMTP, Heartbeat-Timer, `nightjet-heartbeat.service`).

### Open Questions
- [ ] Welche Client-AID/Version der Webapp wird für mgate genutzt? (in Spec-Phase aus der Webapp bzw. einer Live-Abfrage ermitteln und festschreiben)
- [ ] Wie genau ist ein Nightjet im HAFAS-Ergebnis markiert (Produktklasse vs. Name)? Mit echter Antwort NJ 40421 am 07.12.2026 verifizieren und als Testfixture sichern.
- [ ] Soll die Meldung nach Treffer weiterhin den Eintrag aus der Watchlist entfernen (heutiges Verhalten)? Empfehlung: ja, aber Text nennt die Unsicherheit — wegen Fahrplan-2027-Unklarheit evtl. erst bei Treffer an einem Tag nach 13.12. relevant.
- [ ] Telegram-Token in config.yaml noch gültig? (Issue #1)
- [ ] Docker-Zeitzone für den Abfahrtstag-Filter (Europe/Vienna) sicherstellen.

## Live-Verifikation HAFAS (2026-10-02, Spec-Phase)
Geklärt (ersetzt die entsprechenden offenen Fragen aus der Analyse):
- **Endpunkt:** `POST https://fahrplan.oebb.at/bin/mgate.exe`, `Content-Type: application/json`, Browser-User-Agent.
- **Envelope:** `{"lang":"deu","svcReqL":[...],"client":{"id":"OEBB","v":"1","type":"WEB","name":"webapp"},"ext":"OEBB.1","ver":"1.41","auth":{"type":"AID","aid":"OWDL4fE4ixNiPBBm"}}` — funktioniert ohne weitere Header.
- **LocMatch:** `{"meth":"LocMatch","req":{"input":{"field":"S","loc":{"name":"<Name>","type":"S"},"maxLoc":3}}}` → `res.match.locL[]` mit `name`, `extId`, `lid` (z. B. Amsterdam Centraal extId 8400058, Salzburg Hbf 8100002).
- **TripSearch:** `{"meth":"TripSearch","req":{"depLocL":[{"type":"S","lid":"A=1@L=<extId>@"}],"arrLocL":[...],"outDate":"YYYYMMDD","outTime":"000000","jnyFltrL":[{"type":"PROD","mode":"INC","value":"2762"}],"maxChg":0,"numF":10,"getPasslist":false,"getPolyline":false}}`.
- **Antwort:** `res.outConL[]` mit `date` (YYYYMMDD), `dep.dTimeS` / `arr.aTimeS` (HHMMSS, bei Ankunft nächster Tag als 8-stellig `DDHHMMSS`, z. B. `01064600`), `chg` (Umstiege), `secL[].jny.prodX` → Index in `res.common.prodL[]`.
- **Nightjet-Erkennung:** `prodL[].prodCtx.catOutS == "NJ"` bzw. `catOutL == "nightjet"` (Name z. B. `NJ 40421`). Alte Category-Felder entfallen.
- **Treffer-Beispiel Amsterdam Centraal → Salzburg Hbf, ab 2026-12-07:** NJ 40421, Abfahrt 18:01, Ankunft 06:46 Folgetag; Liste liefert 07.–12.12.2026 (je ein Treffer), danach nichts — bestätigt Fahrplan-Ende am 12.12.2026.
- **Fehler:** Top-Level `err` ≠ "OK" oder `svcResL[0].err` ≠ "OK" → Fehler; `H890` (kein Verbindung) kommt als `svcResL[0].err`, ist aber leer, kein Fehler.
- Antwortgröße ~36 KB je TripSearch.
