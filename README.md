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

1. Resolves station names to ÖBB station IDs
2. Queries the ÖBB timetable API for each route/date combination
3. Filters for Nightjet connections (NJ/EN trains)
4. Sends a Signal notification (via Callmebot) when connections are found
5. Removes found connections from the watch list (no duplicate notifications)
