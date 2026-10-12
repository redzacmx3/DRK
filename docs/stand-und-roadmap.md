# Stand und Roadmap

Aktueller Stand: Schema 28. Diese Seite fasst den heutigen Funktionsumfang zusammen; ältere Meilensteine sind in der [Entwicklungshistorie](entwicklungshistorie.md) dokumentiert. Der [Blueprint](blueprint-v2.md) bleibt die konzeptionelle Grundlage und ist keine Behauptung vollständig umgesetzter Funktionen.

## Vorhanden

| Bereich | Implementiert |
|---|---|
| Betrieb | Ein Docker-Image, Compose für lokalen HTTP-Test und HTTPS, NAS-/Traefik-Anleitung |
| Verwaltung | Betreiber, Betriebsstätten, Lagerorte und Benutzer; separates Administratorrecht, Versionen und unveränderliche Änderungshistorie |
| Rechte | Sitzungen, Origin/CSRF, Viewer/Helfer/Materialwart/Geräteverantwortlicher je Betriebsstätte |
| Material | Eingang, Umlagerung, Entnahme, Gegenbuchung, Inventur, Chargen und Verfall, unveränderliches Journal |
| Personenbewertung | Gerätespezifische Pflichten, konkrete Kontozuordnung von Einweisungen, befristete Qualifikations- und Gebrauchsanweisungszugangsnachweise, Widerrufe; Gesamtfreigabe weiter ungeklärt |
| Produktpflichten | Fachlich dokumentierte Anwendbarkeit von acht besonderen Pflichtbereichen, freigegebene Produktgrundlage, Befristung, Widerruf, ausdrückliche Wartungs-/Inbetriebnahme-/Einweisungsbindung, benannte Person mit fachlicher Einweisungsteilabnahme, besondere Funktionsprüfung mit Betriebsort, Prüfprotokoll und ausdrücklicher fachlicher Teilabnahme sowie aktuelle Nachweisvorprüfung im Archiv; Erfüllungsabnahme noch offen |
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

1. Vollständige Einsatzfreigabe: Weitere Pflichtnachweise strukturiert erfassen und die Endanwenderanforderungen sowie die Erfüllung der bereits strukturiert bewerteten Produktpflichten fachlich abnehmen; vorhandene Material-, Funktions-, Qualifikations- und Gebrauchsanweisungsnachweise abschließend zusammenführen. Gesamtfreigabe weiterhin `not_assessed`.
2. Automatisierte verschlüsselte externe Backups, vollständige Wiederherstellung und Betriebsüberwachung.

## Weitere offene Funktionen

Packlistenalternativen, FEFO-Auswahlhilfen, differenzierte Anbruch-/Verfallsregeln, erweiterte Geräte-/Serienzuordnung über Scans, weitere Such-/Blätterfunktionen, administrative MFA und Auditorrechte. Spätere Pilotverbesserungen: PWA/Offline, OIDC, erweiterte GS1/UDI-Felder, signierte Archive und Beschaffung. Mehrmandantenbetrieb/PostgreSQL und spezielle Fachmodule sind gesonderte Erweiterungen. Aufbereitung, Arzneimittel und andere besondere Kategorien benötigen bei Aufnahme in den Umfang eigene bestätigte Regeln.

## Noch erforderliche Abnahme

Echter ClamAV-Dienst mit aktuellen Signaturen und realer Scannerabnahme, NAS-/Dockhand-/Traefik-Betrieb, USB-/Kamerascanner, eigener SMTP-Anbieter und Postfachzustellung, repräsentative Lasttests und vollständige Rücksicherung. Rechtlichen Betreiber, Pflichtbewertungen, Qualifikationen, Zuständigkeiten, Datenschutz, Aufbewahrung und Ausfall-Ersatzverfahren festlegen. Cloud- und lokale Entwicklungsprüfungen sind keine produktive Freigabe.

## Zuletzt geprüft

Schema 28, fachliche Teilabnahme der Einweisung der benannten Person: finale vollständige Go-Tests mit Race-Prüfung, `go vet`, Programm-/Docker-Build und Chromium-Prüfungen bestanden. Ausdrückliche Entscheidung und Bestätigung, Abnahmedatum, Abnahmekompetenz / Beauftragung sowie Nachweise zu Ausbilder, benannter Person und Einweisungsumfang geprüft. Der Server bindet die Teilabnahme an konkrete Kontozuordnung und Unterlagenprüfstand; Neuzuordnung oder erneute Unterlagenfreigabe reaktivieren die alte Abnahme nicht automatisch. Fehlende Angaben, fehlende aktuelle Bindung, Richtigstellung und unabhängige Sperren verhindern positive Teilabnahme. Ablehnung blockiert die Vorprüfung. Ausgewähltes Konto oder Ereignis ändern setzt die Browserbestätigung zurück; unbestätigte Antworten behalten ihre vollständige ursprüngliche Anfrage.

Verifiziertes HTTPS bestätigte Abnahme, Ablehnung, bewusste Erneuerung, getrennten Unterlagenentzug, fehlende automatische Reaktivierung, Packlisten-Sperrkonsolidierung, Rechte vor alter Wiederholantwort, Neustartpersistenz und sichere Archive samt Manifestprüfsummen. Ein aufgedeckter Vorrangfehler bei Richtigstellung plus zusätzlicher Gerätesperre wurde behoben und durch einen Regressionstest abgesichert; die vollständige Race-Suite und der gesamte HTTPS-Ablauf wurden danach erneut bestanden. Eine zunächst zu breite Sperranpassung wurde anhand bestehender Tests auf die unabhängigen Sperren begrenzt; Richtigstellungsquarantäne und Nachweisunklarheit behalten ihre bisherigen getrennten Bedeutungen. Die finale Lese-Testhilfe wurde für `rules: null` in Widerrufen korrigiert und erneut bestanden. Das finale Image zeigt den aktuellen Bericht und Abnahmehistorie; Viewer erhalten keine Kontenauswahl.

Neue lokale / Docker-Backups, Schema 28, SQLite-Integrität, Fremdschlüssel, unveränderte frühere Daten in 72 Tabellen und vollständige Testkontobereinigung bestätigt. Bei vollem Speicher nur wiederherstellbaren Buildcache entfernt; Images, Volumes und Sicherungen erhalten. Scannerkonfiguration leer, SMTP deaktiviert; ClamAV-Protokoll simuliert. Endanwenderanforderungen und weitere Produktpflichten bleiben offen; Gesamtfreigabe `not_assessed`.

Rückrufbedienung und Berechtigungen: [Rückrufe](rueckrufe.md). Die Kombination aus Administratorrecht und Fachrolle erlaubt ausdrücklich globale Rückrufkoordination; Einzelmaßnahmen bleiben standortgebunden.

Personenbezogene Prüfung: [Bedienung, Rechte und Grenzen](personenbewertung.md). Sie erfasst konkrete Nachweise; sie bestätigt keine vollständige medizinische Nutzungsfreigabe.

Dokumentprüfung: [Bedienung, Scannerkonfiguration und Grenzen](dokumentpruefung.md).

Gebrauchsanweisungszugang und gemeinsame Bewertung: [Bedienung und Grenzen](gebrauchsanweisungszugang.md).

Besondere Produktpflichten: [Anwendbarkeit, Nachweise und Grenzen](produktpflichten.md).
