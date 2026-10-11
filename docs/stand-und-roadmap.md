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
| Produktpflichten | Fachlich dokumentierte Anwendbarkeit von acht besonderen Pflichtbereichen, freigegebene Produktgrundlage, Befristung, Widerruf, ausdrückliche Wartungs-/Inbetriebnahme-/Einweisungsbindung, benannte Person und aktuelle Nachweisvorprüfung im Archiv; Erfüllungsabnahme noch offen |
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

1. Vollständige Einsatzfreigabe: Besondere Funktionsprüfung am Betriebsort und weitere Pflichtnachweise strukturiert erfassen und die Erfüllung der bereits strukturiert bewerteten Produktpflichten fachlich abnehmen; vorhandene Material-, Funktions-, Qualifikations- und Gebrauchsanweisungsnachweise abschließend zusammenführen. Gesamtfreigabe weiterhin `not_assessed`.
2. Automatisierte verschlüsselte externe Backups, vollständige Wiederherstellung und Betriebsüberwachung.

## Weitere offene Funktionen

Packlistenalternativen, FEFO-Auswahlhilfen, differenzierte Anbruch-/Verfallsregeln, erweiterte Geräte-/Serienzuordnung über Scans, weitere Such-/Blätterfunktionen, administrative MFA und Auditorrechte. Spätere Pilotverbesserungen: PWA/Offline, OIDC, erweiterte GS1/UDI-Felder, signierte Archive und Beschaffung. Mehrmandantenbetrieb/PostgreSQL und spezielle Fachmodule sind gesonderte Erweiterungen. Aufbereitung, Arzneimittel und andere besondere Kategorien benötigen bei Aufnahme in den Umfang eigene bestätigte Regeln.

## Noch erforderliche Abnahme

Echter ClamAV-Dienst mit aktuellen Signaturen und realer Scannerabnahme, NAS-/Dockhand-/Traefik-Betrieb, USB-/Kamerascanner, eigener SMTP-Anbieter und Postfachzustellung, repräsentative Lasttests und vollständige Rücksicherung. Rechtlichen Betreiber, Pflichtbewertungen, Qualifikationen, Zuständigkeiten, Datenschutz, Aufbewahrung und Ausfall-Ersatzverfahren festlegen. Cloud- und lokale Entwicklungsprüfungen sind keine produktive Freigabe.

## Zuletzt geprüft

Schema 28, Anlage-1-Nachweisverknüpfung: vollständige Go-Tests mit Race-Prüfung, ergänzende finale Nachweis-/Archivtests mit Race-Prüfung, `go vet`, Programm-/Docker-Build und Chromium-Prüfungen bestanden. Ereignistyp und Gerätezugehörigkeit, ausdrückliche benannte Person samt Benennungsnachweis, aktuelle Einweisungskontozuordnung, fehlende Zuordnung, Widerruf, inaktives Konto, Unterlagenfreigabeentzug, Richtigstellung und unabhängige Gerätesperren geprüft. Der organisatorische Vorprüfungsbericht veröffentlicht keine fremden privaten Qualifikations- oder Zuordnungshistorien; Kontenauswahl nur für Geräteverantwortliche, Auswahlfelder nach Abmeldung vollständig geleert. Verifiziertes HTTPS prüfte neue Verknüpfungen, separate Unterlagenbindung, Sperrkonsolidierung, Rechte vor Wiederholantwort, Neustartpersistenz und aktuellen JSON-/HTML-Export. Finales Image prüfte ausdrücklich, dass eine Richtigstellung eine unabhängige Kontosperre nicht beseitigt. Neue lokale und Compose-Backups erzeugt; Schema unverändert 28, frühere Fachdaten in 72 Tabellen erhalten. SQLite-Integrität, Fremdschlüssel und Testkontobereinigung bestanden. Eine anfangs veraltete Geräteversion in der Testvorbereitung wurde korrigiert und die vollständige Suite danach erfolgreich wiederholt. Nur wiederherstellbaren Docker-Buildcache bei vollem Speicher entfernt; Volumes und Sicherungen erhalten. Scannerkonfiguration leer, SMTP deaktiviert; ClamAV-Protokoll simuliert. Inbetriebnahme ist keine besondere Funktionsprüfung am Betriebsort; diese und fachliche Eignungs-/Erfüllungsabnahme sowie weitere Pflichtbereiche bleiben offen, Gesamtfreigabe `not_assessed`.

Rückrufbedienung und Berechtigungen: [Rückrufe](rueckrufe.md). Die Kombination aus Administratorrecht und Fachrolle erlaubt ausdrücklich globale Rückrufkoordination; Einzelmaßnahmen bleiben standortgebunden.

Personenbezogene Prüfung: [Bedienung, Rechte und Grenzen](personenbewertung.md). Sie erfasst konkrete Nachweise; sie bestätigt keine vollständige medizinische Nutzungsfreigabe.

Dokumentprüfung: [Bedienung, Scannerkonfiguration und Grenzen](dokumentpruefung.md).

Gebrauchsanweisungszugang und gemeinsame Bewertung: [Bedienung und Grenzen](gebrauchsanweisungszugang.md).

Besondere Produktpflichten: [Anwendbarkeit, Nachweise und Grenzen](produktpflichten.md).
