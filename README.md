# ÖBB Nightjet Monitor

Monitors ÖBB Nightjet train connections and sends a Signal notification (via Callmebot) when they become bookable. Nightjets are typically released ~2 months before departure and sell out quickly.

## Setup

```bash
go build -o oebb-nightjet-monitor .
```

## Configuration

Copy and edit `config.yaml`:

```yaml
signal_phone: "YOUR_SIGNAL_PHONE_UUID"
signal_apikey: "YOUR_CALLMEBOT_APIKEY"
check_interval: 60m
connections:
  - from: "Münster(Westf)Hbf"
    to: "Wörgl Hbf"
    dates:
      - "2026-04-30"
      - "2026-05-15"
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
5. Removes found connections from the watch list (no duplicate notifications)

Errors: HTTP != 200, invalid JSON or HAFAS `err` != `OK` are reported as errors; `H890` (no connection) is an empty result.

Canary: optional `canary: {from, to}` in `config.yaml` (default Wien Hbf → Innsbruck Hbf). If fewer than half of the days in the window today+3..today+10 have a Nightjet for 3 cycles in a row, a Telegram alert is sent. The heartbeat is only pinged when the watch list check and the canary query succeed.
