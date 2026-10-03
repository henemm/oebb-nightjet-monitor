# ÖBB Nightjet Monitor

Monitors ÖBB Nightjet train connections and sends a Telegram notification when a direct Nightjet appears in the timetable. Nightjets are typically released ~2 months before departure and sell out quickly.

## Setup

```bash
go build -o oebb-nightjet-monitor .
```

## Configuration

Copy and edit `config.yaml`:

```yaml
telegram_bot_token: "123456:ABC..."
telegram_chat_id: "-1001234567890"
telegram_topic_id: 0            # optional, only for groups with topics
check_interval: 60m
success_file: "/data/nightjet.success"    # optional, readiness stamp
notified_file: "/data/nightjet.notified"  # optional, remembers notified hits across restarts
connections:
  - from: "Amsterdam Centraal"
    to: "Salzburg Hbf"
    dates:
      - "2026-12-28"
```

## Usage

```bash
# Run as daemon (checks every hour)
./oebb-nightjet-monitor -config config.yaml

# Run once and exit
./oebb-nightjet-monitor -config config.yaml -once
```

## Docker

```bash
docker build -t oebb-nightjet-monitor .
docker run --rm -v $(pwd)/config.yaml:/app/config.yaml oebb-nightjet-monitor
```

## How it works

The monitor queries the public ÖBB timetable (HAFAS, `POST https://fahrplan.oebb.at/bin/mgate.exe`, unofficial web-app endpoint with the web-app AID). The ticket shop is no longer used (blocked by Cloudflare since autumn 2026); no token or init call is needed.

1. Resolves station names to ÖBB station IDs (HAFAS `LocMatch`, first match)
2. Queries `TripSearch` for each route/date combination — direct connections only (`maxChg=0`, product filter `2762`); connections departing on other days are filtered out
3. Filters for Nightjets (`prodCtx.catOutS == "NJ"` or `catOutL == "nightjet"`)
4. Sends a Telegram notification ("Direktverbindung im Fahrplan gefunden — Buchbarkeit bitte prüfen"); a timetable hit is not proof of bookability
5. Records notified hits in `notified_file` (JSON, written atomically) and never notifies the same route/date again, even after a restart. Without `notified_file` hits are only remembered in memory.

Errors: HTTP != 200, invalid JSON or HAFAS `err` != `OK` are reported as errors; `H890` (no connection) is an empty result.

Canary: optional `canary: {from, to}` in `config.yaml` (default Wien Hbf → Innsbruck Hbf). If fewer than half of the days in the window today+3..today+10 have a Nightjet for 3 cycles in a row, a Telegram alert is sent. The success file / heartbeat is only written when the watch list check, the canary query, every required Telegram delivery and saving `notified_file` succeed. A failed Telegram delivery keeps the hit on the watch list and retries next cycle.
