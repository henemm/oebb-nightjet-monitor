# ÖBB Nightjet Monitor

## Architektur

Go-Service der stündlich ÖBB Nightjet-Verbindungen prüft und per Slack benachrichtigt.

### Dateien
- `main.go` — Entry point, Scheduler (time.Ticker), graceful shutdown
- `config.go` — YAML Config laden mit `gopkg.in/yaml.v3`
- `oebb.go` — ÖBB API Client (init, stations, timetable)
- `notify.go` — Slack Webhook Notification

### ÖBB API Flow
1. `GET https://tickets.oebb.at/api/domain/v4/init` → accessToken (Header: `Channel: inet`)
2. `GET https://shop.oebbtickets.at/api/hafas/v1/stations?name=...` → Station-IDs
3. `POST https://shop.oebbtickets.at/api/hafas/v4/timetable` → Verbindungen

**Wichtig:** Init geht über `tickets.oebb.at`, alle anderen Calls über `shop.oebbtickets.at` (Redirect-Problem).

### Nightjet erkennen
Category-Felder: `name`/`shortName`/`displayName` beginnt mit "NJ" oder "EN", oder `longName` enthält "Nightjet" (kann String oder `{"de":"..","en":".."}` sein).

### Build & Run
```bash
go build -o oebb-nightjet-monitor .
./oebb-nightjet-monitor -config config.yaml        # Daemon-Modus
./oebb-nightjet-monitor -config config.yaml -once   # Einmal prüfen
```

### Dependencies
- `gopkg.in/yaml.v3` — einzige externe Dependency
- Go stdlib für alles andere
