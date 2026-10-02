---
spec_file: docs/specs/modules/hafas-umstellung.md
spec_sha256: 7da04ddd4fe2f58f57879e9b58c1d233d4d7063d8b7bfb968ffecc6b203cc67e
---

# PO-Briefing: hafas-umstellung

- **Spec:** docs/specs/modules/hafas-umstellung.md
- **Issue:** #1
- **Erstellt:** 2026-10-02

## Was gebaut wird

Der Monitor prüft die öffentliche ÖBB-Fahrplanauskunft und meldet per Telegram, sobald Amsterdam–Salzburg am 28.12.2026 direkt fährt.

## Definition of Done

Ein Probelauf gegen die echte Fahrplanauskunft findet Stationen, zeigt grünen Referenzcheck Wien–Innsbruck und sendet den Überwachungs-Ping; Konfiguration steht auf Amsterdam–Salzburg.

## Wie geprüft wird

Automatische Tests mit nachgespielten Antworten belegen alle zehn Kriterien; ob die echte ÖBB-Schnittstelle dauerhaft funktioniert, beweisen sie nicht.

## Kritische Anmerkungen

- Abweichung: Ticket nennt Amsterdam–Wörgl per Config-Änderung; Spec baut Datenquelle komplett um und beobachtet Amsterdam–Salzburg.
- Ab 13.12. gibt es keine Direktverbindung; Meldung kann ausbleiben, und ein Treffer bedeutet nicht buchbar.
- Wien–Innsbruck-Referenzstrecke ist Zusatz, den niemand verlangte; Live-Lauf und Konfiguration sind manuell, ohne automatischen Test.

## Freigabe-Frage

Gebe ich die Umstellung auf Amsterdam–Salzburg samt Datenquellen-Umbau frei?
