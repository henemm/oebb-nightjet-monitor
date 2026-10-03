---
spec_file: docs/specs/modules/treffer-einmalig.md
spec_sha256: 841602cf0da8cc87741a5170ac3e5eed6832167120e52438be8a3c49d705d8ed
---

# PO-Briefing: bug-5-treffer-einmalig

- **Spec:** docs/specs/modules/treffer-einmalig.md
- **Issue:** #5 (inkl. Rest von #4)
- **Erstellt:** 2026-10-03

## Was gebaut wird

Jeder gefundene Nightjet wird genau einmal gemeldet, auch nach Neustarts; scheitert Telegram, schlägt die Server-Überwachung Alarm.

## Definition of Done

Der Monitor läuft dauerhaft ohne Selbst-Beenden, meldet jeden Treffer einmal, und die Erfolgsdatei bleibt aktuell.

## Wie geprüft wird

Automatische Tests mit simuliertem Fahrplan und Telegram belegen Einmalmeldung und Fehlerfälle; echter Telegram-Versand und Dauerbetrieb nicht.

## Kritische Anmerkungen

- Zugestellter, aber nicht gespeicherter Treffer wird nach Neustart doppelt gemeldet; ebenso nach Löschen der Merkdatei.
- "Beendet sich nicht mehr selbst" ist nur per Code-Durchsicht geprüft, ohne automatischen Test.
- Alarm bei Telegram-Ausfall kommt erst, wenn die Erfolgsdatei veraltet, also zeitverzögert.

## Freigabe-Frage

Ist es für dich akzeptabel, dass ein Telegram-Ausfall erst verzögert über die Server-Überwachung auffällt?
