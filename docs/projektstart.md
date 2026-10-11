# Projektstart

Aktueller Funktionsumfang und Prioritäten: [Stand und Roadmap](stand-und-roadmap.md). API- und Betriebsdetails: [README](../README.md). Die unveränderte konzeptionelle Grundlage bleibt [Blueprint v2](blueprint-v2.md). Historische Meilensteine und deren Nachweise stehen getrennt in der [Entwicklungshistorie](entwicklungshistorie.md).

## Installation und Erfassung

- [Docker-Schnellstart](schnellstart-docker.md): derselbe Container mit lokalem HTTP oder HTTPS.
- [NAS und Dockhand](nas-dockhand.md): Ugreen-NAS, Tailscale und Traefik.
- [Geräte-Ersterfassung](erstinventarisierung.md): Excel mit Barcode-Etiketten und CSV-Import.
- [Verbrauchsmaterial-Erfassung](verbrauchsmaterial-erfassung.md): eigene Excel-Vorlage, Erstbestand und spätere Zugänge.
- [Verwaltung](administration.md): Betreiber, Betriebsstätten, Lagerorte, Benutzer und getrennte Rechte.
- [Rückrufe](rueckrufe.md): geprüfte Treffer, Sperrwirkung, Einzelmaßnahmen und Gesamtabschluss.
- [SMTP-Erinnerungen](smtp-erinnerungen.md): optionale TLS-Zustellung; standardmäßig deaktiviert.

## Vor einem Pilotbetrieb

Betreiber und Betriebsstätten, Produktumfang und Pflichtbewertungen, qualifizierte Zuständigkeiten, Kontrollregeln, Datenschutz/Aufbewahrung und betrieblicher Ersatzprozess festlegen. Die Anwendung bestätigt derzeit keine vollständige Einsatzfreigabe. Nur synthetische Entwicklungsdaten verwenden, bis die fachliche und betriebliche Abnahme abgeschlossen ist.

Vor Updates ein neues konsistentes Backup erstellen und notwendige Schemaänderungen ausdrücklich migrieren. Vorhandene Datenvolumes und Sicherungen erhalten. Entwicklungsprüfungen ersetzen keine NAS-, Scanner-, Postfach- oder vollständige Restore-Abnahme.
