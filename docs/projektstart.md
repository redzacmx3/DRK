# Projektstart

## Stand des ersten Meilensteins

Implementiert: Go-Dienst, eingebettete HTML/JS-Oberfläche, explizite SQLite-Migrationen v1/v2, synthetische Betreiber-/Betriebsstätten-/Lagerortdaten und lesende Lagerort-API. HTTPS-Anmeldung mit Argon2id, widerrufbaren Sessions und Viewer-Rechten je Betriebsstätte schützt die Lagerortliste und direkte Objektzugriffe. Lokale CLI für Kontoanlage, Bereichsänderung, Kontosperre und Artikel-/Chargenanlage. Materialfluss mit Wareneingang, atomarer Umlagerung und Entnahme, Quellversionen, Idempotenz, Bewegungsjournal/Audit und Outbox ist umgesetzt. Die Oberfläche zeigt physische/nutzbare Mengen und erlaubt Buchungen je Rolle. SQLite-Treiber: mattn/go-sqlite3 mit CGO. Die Readiness prüft das Schema und Datenbankzugriff. Dockerfile und Compose-Entwicklungskonfiguration sind vorhanden.

Offen: Inventur/Gegenbuchung, weitere Rollen und Fachrechte, Packlisten/Scanner, PWA-Funktionen, Dokumente und vollständige Backup/Restore-Abnahme. Ein SQLite-Snapshot vor Migrationen ist über die CLI möglich. Die aktuelle Oberfläche ist nur für synthetische Entwicklungsdaten vorgesehen.

## Weitere Umsetzung

1. Go-Modul und Dienstgerüst mit eingebetteter Weboberfläche sowie getrennten Liveness-/Readiness-Endpunkten erstellen.
2. SQLite-Treiber wählen und versionierte, explizite Migrationen einführen. Fremdschlüssel pro Verbindung, WAL, Busy-Timeout und synchronous=FULL prüfen.
3. Betreiber, Betriebsstätten und Lagerorte modellieren; mit synthetischen Entwicklungsdaten arbeiten.
4. Serverseitige Sessions, CSRF/Origin-Prüfung und objektbezogene Rechte vor schreibenden Fachendpunkten implementieren.
5. Einen Materialfluss von Eingang über Umlagerung bis Entnahme umsetzen: atomare Buchungen, Idempotenz, Versionsprüfung und unabhängige Sperren.
6. Konsistente Datenbank-/Dokumentsicherung und Rücksicherung erproben, bevor reale Nachweise gespeichert werden.

## Nachweise für diesen Abschnitt

- Dienst startet und Readiness prüft die tatsächliche Datenbankverbindung.
- Gleichzeitige Entnahmen erzeugen keine negativen Bestände.
- Transfer wird vollständig oder gar nicht gebucht.
- Identischer Idempotenzschlüssel wiederholt die Antwort; abweichende Nutzlast wird abgelehnt.
- Unberechtigte Objektzugriffe und Schreibzugriffe ohne CSRF-Nachweis scheitern.
- Restore prüft Integrität, Fremdschlüssel und Dokumentreferenzen.

Danach folgen Geräteakte, Fristen, Packlisten, Scanner und Exporte gemäß Blueprint. Ein separates Frontend-Buildwerkzeug wird aktuell nicht benötigt; die Oberfläche wird direkt eingebettet. Der Produktionsbetrieb bleibt offen. Das Blueprint-Compose-Beispiel ist weiterhin konzeptionell; die Compose-Datei im Repository dient der Linux-Entwicklung.

## Vor Pilotbetrieb zu klären

Betreiber und Betriebsstätten, Produktumfang und Pflichtbewertungen, qualifizierte Zuständigkeiten, Kontrollregeln, Datenschutz/Aufbewahrung und betrieblicher Ersatzprozess. Diese Angaben blockieren den Start mit synthetischen Daten nicht.
