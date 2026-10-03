# ÖBB Nightjet Monitor

## Architektur

Go-Service der stündlich ÖBB Nightjet-Verbindungen prüft und per Telegram benachrichtigt.

### Dateien
- `main.go` — Entry point, Scheduler (time.Ticker), graceful shutdown
- `config.go` — YAML Config laden mit `gopkg.in/yaml.v3`
- `oebb.go` — HAFAS-Client der ÖBB-Fahrplanauskunft (LocMatch, TripSearch)
- `notify.go` — Telegram Notification

### HAFAS-Ablauf (ÖBB Fahrplanauskunft)
Der Ticketshop (`shop.oebbtickets.at`) blockt seit Herbst 2026 per Cloudflare (403). Abgefragt wird deshalb die öffentliche Fahrplanauskunft.

- **Endpunkt:** `POST https://fahrplan.oebb.at/bin/mgate.exe` (inoffiziell), `Content-Type: application/json`, Browser-User-Agent. Kein Token, kein Init.
- **Envelope:** `lang: deu`, `svcReqL` mit genau einem Request, `client {id: OEBB, v: 1, type: WEB, name: webapp}`, `ext: OEBB.1`, `ver: 1.41`, `auth {type: AID, aid: OWDL4fE4ixNiPBBm}`. AID/Version stehen als Konstanten in `oebb.go`.
1. `LocMatch` → `res.match.locL[0]` (`extId` = Stationsnummer, `name`)
2. `TripSearch` mit `lid = A=1@L=<extId>@`, `outDate = YYYYMMDD`, `maxChg = 0` und Produktfilter `2762` (= Webapp "Nur Direktverbindungen") → `res.outConL[]`. Nur Verbindungen mit `date` == gesuchtes Datum zählen (Folgetage werden mitgeliefert). Zeiten in Europe/Vienna; Ankunft `aTimeS` ist `HHMMSS` oder `DDHHMMSS` (DD = Tagesoffset).

**Fehler:** HTTP != 200, ungültiges JSON, fehlendes `svcResL`, Top-Level `err` != OK oder `svcResL[0].err` != OK → Fehler. Ausnahme `H890` (keine Verbindung) → leeres Ergebnis.

**Hinweis:** Ein Fahrplan-Treffer ist kein Buchbarkeits-Nachweis — die Telegram-Meldung sagt "Direktverbindung im Fahrplan gefunden — Buchbarkeit bitte prüfen".

### Nightjet erkennen
`secL[].jny.prodX` → `res.common.prodL[]`: `prodCtx.catOutS == "NJ"` oder `prodCtx.catOutL == "nightjet"` (Zugname aus `prodCtx.name`, z. B. `NJ 40421`).

### Canary
Optionaler Config-Block `canary: {from, to}` — Referenzstrecke, Default Wien Hbf → Innsbruck Hbf (täglich Direkt-Nightjets). Fenster heute+3..heute+10, mind. 50 % der Tage mit Nightjet, nach 3 Fehlzyklen Telegram-Alarm. Canary-Stationen werden lazy aufgelöst; solange das scheitert, kein Heartbeat.

### Build & Run
```bash
go build -o oebb-nightjet-monitor .
./oebb-nightjet-monitor -config config.yaml        # Daemon-Modus
./oebb-nightjet-monitor -config config.yaml -once   # Einmal prüfen
```

### Dependencies
- `gopkg.in/yaml.v3` — einzige externe Dependency
- Go stdlib für alles andere

## Deployment & Infrastruktur

Globale Server-Infos und Monitoring-Anleitung stehen in `~/.claude/CLAUDE.md`.

- **Container:** Docker Compose (`docker-compose.yml`), restart: unless-stopped
- **Config:** `config.yaml` (Telegram Bot-Token/Chat-ID/Topic-ID, Heartbeat-URL, Verbindungen)
- **Infrastruktur-Repo:** `henemm/henemm-infra`
- **Erfolgsmeldung (Readiness):** Nach jedem fachlich erfolgreichen Zyklus (`checkOK && canaryAPIOK`) schreibt der Monitor einen RFC3339-Zeitstempel in die Datei `success_file` (`config.yaml`, im Container `/data/nightjet.success`, Host: `/home/hem/backups/nightjet-monitor/nightjet.success`). `henemm-infra/scripts/monitor.sh` (`check_nightjet`) prüft das Alter und alarmiert kritisch. Der BetterStack-Ping (`heartbeat_url`) ist optional — die Quota ist voll, der alte Heartbeat gelöscht.
- **Kein Erfolg ohne Zustellung:** Ein gescheiterter Telegram-Versand (Treffer oder Canary-Alarm) und ein nicht gespeicherter Treffer zählen als nicht erfolgreicher Zyklus. Die Erfolgsdatei veraltet, und `monitor.sh` alarmiert über die Infra-Kanäle unabhängig vom Bot. Der Treffer bleibt auf der Watchlist und wird im nächsten Zyklus erneut gesendet.
- **Gemeldete Treffer:** `notified_file` (Container `/data/nightjet.notified`, Host `/home/hem/backups/nightjet-monitor/nightjet.notified`), JSON-Liste `{from, to, date, notified_at}` mit den Namen aus `config.yaml`. Bereits gemeldete Kombinationen werden beim Start herausgefiltert. Löschen der Datei setzt das zurück: Treffer werden dann erneut gemeldet. Ist die Datei kaputt, gibt es keine Watch-Prüfung und keinen Erfolg (Alarm), bis sie repariert ist. Der Prozess beendet sich nicht mehr selbst, wenn alles gemeldet ist.
- **Telegram:** Bot `@nightjet_bot`, Gruppe „Nightjet“ (einfache Gruppe ohne Themen, `telegram_topic_id: 0`).
- **Deploy aus einer Worktree-Session:** `docker compose -f /home/hem/oebb-nightjet-monitor/docker-compose.yml --project-directory /home/hem/oebb-nightjet-monitor up -d --build` (baut aus dem Hauptordner, dort liegt `config.yaml`).

## Messaging

Diese Instanz heißt `nightjet`. Siehe `~/.claude/CLAUDE.md` → "Inter-Instance Messaging" für Details.
