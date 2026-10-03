---
spec_file: docs/specs/modules/treffer-einmalig.md
spec_sha256: 3caa56bbb7f7616a62cb93b63d5edd751c1e8897ce96921dad9f059160c8ac4c
---

# PO-Briefing: bug-5-treffer-einmalig

- **Spec:** docs/specs/modules/treffer-einmalig.md
- **Issue:** #5 (plus Rest von #4)
- **Erstellt:** 2026-10-03

## Was gebaut wird

Der Monitor meldet jeden gefundenen Nightjet genau einmal, auch nach Neustart, und schlägt bei Telegram-Fehlern Alarm.

## Definition of Done

Nach Neustart kommt keine Doppelmeldung, der Monitor läuft dauerhaft weiter, und ein Telegram-Fehler lässt die Erfolgsdatei veralten.

## Wie geprüft wird

Automatische Tests mit simuliertem Fahrplan und Telegram belegen Einmal-Meldung und Fehlerfälle; echte Telegram-Zustellung und Server-Alarm werden nicht getestet.

## Kritische Anmerkungen

- Dass der Monitor nie von selbst endet, prüft kein Test, nur Code-Durchsicht.
- Zugestellt, aber Speichern gescheitert: Nach Neustart doppelte Meldung; Alarm kommt nur verzögert über das Alter der Erfolgsdatei.
- Gelöschte Merkdatei oder geänderte Stationsschreibweise führt bewusst zu erneuter Meldung.

## Freigabe-Frage

Soll der Monitor Treffer dauerhaft merken, nie selbst enden und bei Telegram-Fehlern Alarm auslösen, wie beschrieben?
