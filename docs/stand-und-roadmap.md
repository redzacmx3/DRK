# Stand und Roadmap

Aktueller Stand: Schema 23. Diese Seite fasst den heutigen Funktionsumfang zusammen; ältere Meilensteine sind in der [Entwicklungshistorie](entwicklungshistorie.md) dokumentiert. Der [Blueprint](blueprint-v2.md) bleibt die konzeptionelle Grundlage und ist keine Behauptung vollständig umgesetzter Funktionen.

## Vorhanden

| Bereich | Implementiert |
|---|---|
| Betrieb | Ein Docker-Image, Compose für lokalen HTTP-Test und HTTPS, NAS-/Traefik-Anleitung |
| Verwaltung | Betreiber, Betriebsstätten, Lagerorte und Benutzer; separates Administratorrecht, Versionen und unveränderliche Änderungshistorie |
| Rechte | Sitzungen, Origin/CSRF, Viewer/Helfer/Materialwart/Geräteverantwortlicher je Betriebsstätte |
| Material | Eingang, Umlagerung, Entnahme, Gegenbuchung, Inventur, Chargen und Verfall, unveränderliches Journal |
| Geräte | Akten, Stammdaten, Inbetriebnahme, versionierte Fristen, Prüfungen, getrennte Sperren/Freigaben, Einweisung, Instandhaltung, Außerbetriebnahme, ergänzende Richtigstellung |
| Störungen | Störungserfassung und separate Meldebewertung mit Behörden-/Herstellerreferenzen; keine automatische Behördenmeldung |
| Packlisten | Versionierte Material-/Geräte-/Zubehöranforderungen, digitale Bewertung und getrennte physische Material-/Gerätechecks |
| Erfassung | Geräte-/Material-Excelvorlagen, Barcode-Etiketten, USB-Eingabe, CSV-Vorschau und kontrollierter Import |
| Scanner | Interne Codes, GTIN/PZN/REF, begrenzte GS1-Auswertung; optionale native Browserkamera |
| Dokumente/Export | Geschützte Quarantäneanhänge, CSV-Übersicht, vollständige Geräteaktenarchive mit Prüfmanifest |
| Erinnerungen | Arbeitsübersicht, persistente Kontrollerinnerungen, optionaler TLS-SMTP-Transport mit Versuchshistorie und Unsicherheitsbehandlung |
| Sicherung | Konsistenter SQLite-Snapshot einschließlich Anhängen; synthetische Integritäts-/Restore-Prüfungen |

## Nächste Umsetzung

1. Übergreifende Rückrufverwaltung: Produkte/Chargen/Serienbereiche, Standorttreffer und Maßnahmenabschluss.
2. Vollständige Einsatzbewertung: konkrete Nutzer-/Einweisungspflichten und notwendige fachliche Nachweise zusammenführen. Aktuell bleibt die Gesamtfreigabe `not_assessed`.
3. Malwareprüfung und kontrollierte Dokumentfreigabe.
4. Automatisierte verschlüsselte externe Backups, vollständige Wiederherstellung und Betriebsüberwachung.

## Weitere offene Funktionen

Packlistenalternativen, FEFO-Auswahlhilfen, differenzierte Anbruch-/Verfallsregeln, erweiterte Geräte-/Serienzuordnung über Scans, weitere Such-/Blätterfunktionen, administrative MFA und Auditorrechte. Spätere Pilotverbesserungen: PWA/Offline, OIDC, erweiterte GS1/UDI-Felder, signierte Archive und Beschaffung. Mehrmandantenbetrieb/PostgreSQL und spezielle Fachmodule sind gesonderte Erweiterungen. Aufbereitung, Arzneimittel und andere besondere Kategorien benötigen bei Aufnahme in den Umfang eigene bestätigte Regeln.

## Noch erforderliche Abnahme

Echter NAS-/Dockhand-/Traefik-Betrieb, USB-/Kamerascanner, eigener SMTP-Anbieter und Postfachzustellung, repräsentative Lasttests und vollständige Rücksicherung. Rechtlichen Betreiber, Pflichtbewertungen, Qualifikationen, Zuständigkeiten, Datenschutz, Aufbewahrung und Ausfall-Ersatzverfahren festlegen. Cloud- und lokale Entwicklungsprüfungen sind keine produktive Freigabe.

## Zuletzt geprüft

Schema 23: vollständige Go-Tests mit Race-Prüfung, `go vet`, Programm- und Docker-Build sowie Chromium-Prüfungen der Oberfläche. Bestehende lokale und Compose-Datenbanken nach neuen Backups zweimal migriert. Über verifiziertes HTTPS organisatorische Änderungen, ausdrückliche Mehrfachrollen, Passwortwechsel, Sitzungswiderruf und dauerhafte Wiederholantworten einschließlich Neustart geprüft. Bestände, Journale, Akten, Anhänge und frühere Material-/Geräteprüfgrundlagen erhalten; Snapshot-Integrität und Fremdschlüssel geprüft. Temporäre Administratorkonten wieder entberechtigt und deaktiviert. Das ersetzt keine der oben genannten produktiven Abnahmen.
