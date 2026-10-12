# Stand und Roadmap

Aktueller Stand: Schema 29. Diese Seite fasst den heutigen Funktionsumfang zusammen; ältere Meilensteine sind in der [Entwicklungshistorie](entwicklungshistorie.md) dokumentiert. Der [Blueprint](blueprint-v2.md) bleibt die konzeptionelle Grundlage und ist keine Behauptung vollständig umgesetzter Funktionen.

## Vorhanden

| Bereich | Implementiert |
|---|---|
| Betrieb | Ein Docker-Image, Compose für lokalen HTTP-Test und HTTPS, NAS-/Traefik-Anleitung |
| Verwaltung | Betreiber, Betriebsstätten, Lagerorte und Benutzer; separates Administratorrecht, Versionen und unveränderliche Änderungshistorie |
| Rechte | Sitzungen, Origin/CSRF, Viewer/Helfer/Materialwart/Geräteverantwortlicher je Betriebsstätte |
| Material | Eingang, Umlagerung, Entnahme, Gegenbuchung, Inventur, Chargen und Verfall, unveränderliches Journal |
| Personenbewertung | Gerätespezifische Pflichten, konkrete Kontozuordnung von Einweisungen, befristete Qualifikations- und Gebrauchsanweisungszugangsnachweise, eigene fachliche Endanwenderabnahme, Widerrufe; Gesamtfreigabe weiter ungeklärt |
| Produktpflichten | Fachlich dokumentierte Anwendbarkeit von acht besonderen Pflichtbereichen, freigegebene Produktgrundlage, Befristung, Widerruf, ausdrückliche Wartungs-/Inbetriebnahme-/Einweisungsbindung, benannte Person mit fachlicher Einweisungsteilabnahme, besondere Funktionsprüfung mit Betriebsort, Prüfprotokoll und ausdrücklicher fachlicher Teilabnahme sowie qualifizierte Herstellerwartungsabnahme mit bestätigter Fälligkeit und aktuellen Dokumentprüfständen; weitere Erfüllungsabnahmen offen |
| Geräte | Akten, Stammdaten, Inbetriebnahme, versionierte Fristen, Prüfungen, getrennte Sperren/Freigaben, Einweisung, Instandhaltung, Außerbetriebnahme, ergänzende Richtigstellung |
| Rückrufe | Produkt-/Chargen-/Gerätebereiche, geprüfte Treffer-Vorschau, unabhängige Sperren, Standortmaßnahmen und kontrollierter Gesamtabschluss |
| Störungen | Störungserfassung und separate Meldebewertung mit Behörden-/Herstellerreferenzen; keine automatische Behördenmeldung |
| Packlisten | Versionierte Material-/Geräte-/Zubehöranforderungen, digitale Bewertung, getrennte physische Material-/Gerätechecks und zusammengeführte Teilvoraussetzungen |
| Erfassung | Geräte-/Material-Excelvorlagen, Barcode-Etiketten, USB-Eingabe, CSV-Vorschau und kontrollierter Import |
| Scanner | Interne Codes, GTIN/PZN/REF, begrenzte GS1-Auswertung; optionale native Browserkamera |
| Dokumente/Export | Quarantäneanhänge, optionale ClamAV-Prüfung, getrennte Freigabe/Widerruf und Prüfhistorie, CSV-Übersicht, vollständige Geräteaktenarchive mit Prüfmanifest |
| Erinnerungen | Arbeitsübersicht, persistente Kontrollerinnerungen, optionaler TLS-SMTP-Transport mit Versuchshistorie und Unsicherheitsbehandlung |
| Sicherung | Konsistenter SQLite-Snapshot einschließlich Anhängen; synthetische Integritäts-/Restore-Prüfungen |

## Nächste Umsetzung

1. Vollständige Einsatzfreigabe: Weitere Pflichtnachweise strukturiert erfassen und die Erfüllung der bereits strukturiert bewerteten Produktpflichten fachlich abnehmen; vorhandene Material-, Funktions-, Qualifikations- und Gebrauchsanweisungsnachweise abschließend zusammenführen. Gesamtfreigabe weiterhin `not_assessed`.
2. Automatisierte verschlüsselte externe Backups, vollständige Wiederherstellung und Betriebsüberwachung.

## Weitere offene Funktionen

Packlistenalternativen, FEFO-Auswahlhilfen, differenzierte Anbruch-/Verfallsregeln, erweiterte Geräte-/Serienzuordnung über Scans, weitere Such-/Blätterfunktionen, administrative MFA und Auditorrechte. Spätere Pilotverbesserungen: PWA/Offline, OIDC, erweiterte GS1/UDI-Felder, signierte Archive und Beschaffung. Mehrmandantenbetrieb/PostgreSQL und spezielle Fachmodule sind gesonderte Erweiterungen. Aufbereitung, Arzneimittel und andere besondere Kategorien benötigen bei Aufnahme in den Umfang eigene bestätigte Regeln.

## Noch erforderliche Abnahme

Echter ClamAV-Dienst mit aktuellen Signaturen und realer Scannerabnahme, NAS-/Dockhand-/Traefik-Betrieb, USB-/Kamerascanner, eigener SMTP-Anbieter und Postfachzustellung, repräsentative Lasttests und vollständige Rücksicherung. Rechtlichen Betreiber, Pflichtbewertungen, Qualifikationen, Zuständigkeiten, Datenschutz, Aufbewahrung und Ausfall-Ersatzverfahren festlegen. Cloud- und lokale Entwicklungsprüfungen sind keine produktive Freigabe.

## Ergänzende Wartungsprüfung

Unabhängige Gerätesperren haben auch bei korrigierten oder veralteten Wartungsnachweisen Vorrang; reine Richtigstellung bleibt ungeklärt. Fehlende / fremde Herstellergrundlagen werden vor Speicherung mit Bereichsfehler abgewiesen. Produktpflichten-Regressionsprüfungen mit Race-Prüfung, `go vet` und Programm-Build bestanden. Docker-Build und verifiziertes HTTPS bestätigten fehlende Herstellerunterlage ohne Mutation, reine Richtigstellung, zusätzliche unabhängige Sperre, Ablehnung positiver Abnahme, Neustart / historische Wiederholantwort und Aktenmanifest. Integrität / Fremdschlüssel und Datenerhalt in 75 Tabellen geprüft; Testkonto deaktiviert, Scannerkonfiguration wieder leer. Schema 29 unverändert.

## Zuletzt geprüft

Schema 29, fachliche Herstellerwartungsabnahme: explizite Entscheidung, Intervall-/Umfangs-/Befähigungsnachweise und bestätigte nächste Fälligkeit. Nachweisschnappschüsse binden Herstellergrundlage und Protokoll an ihre aktuellen Prüfversionen. Erneute Dokumentfreigabe erfordert erneute Abnahme; fehlgeschlagene Wartung bleibt nach Richtigstellung blockiert. Gesamtfreigabe bleibt `not_assessed`. Gezielte Go-Prüfungen, Browserregression und Docker-Build bestanden. Verifiziertes HTTPS prüfte Abnahme, Ablehnung, Dokumententzug / erneute Freigabe ohne automatische Reaktivierung, erneute Abnahme, unabhängige Gerätesperre, Rechte vor historischer Wiederholantwort, Neustartpersistenz und Geräteaktenmanifest. SQLite-Integrität / Fremdschlüssel geprüft; vorhandene Daten in 75 Tabellen unverändert erhalten und alle neuen Testkonten deaktiviert. Scannerprotokoll simuliert, Scannerkonfiguration anschließend leer; SMTP deaktiviert. Keine zusätzliche Migration. Vollständige Go-Suite mit Race-Prüfung, `go vet` und Programm-Build bestanden.

Rückrufbedienung und Berechtigungen: [Rückrufe](rueckrufe.md). Die Kombination aus Administratorrecht und Fachrolle erlaubt ausdrücklich globale Rückrufkoordination; Einzelmaßnahmen bleiben standortgebunden.

Personenbezogene Prüfung: [Bedienung, Rechte und Grenzen](personenbewertung.md). Sie erfasst konkrete Nachweise; sie bestätigt keine vollständige medizinische Nutzungsfreigabe.

Dokumentprüfung: [Bedienung, Scannerkonfiguration und Grenzen](dokumentpruefung.md).

Gebrauchsanweisungszugang und gemeinsame Bewertung: [Bedienung und Grenzen](gebrauchsanweisungszugang.md).

Besondere Produktpflichten: [Anwendbarkeit, Nachweise und Grenzen](produktpflichten.md).

Endanwenderabnahme: [Bedienung, Bindung und private Aktenbereiche](endanwenderbewertung.md).
