# Stand und Roadmap

Aktueller Stand: Schema 31. Diese Seite fasst den heutigen Funktionsumfang zusammen; ältere Meilensteine sind in der [Entwicklungshistorie](entwicklungshistorie.md) dokumentiert. Der [Blueprint](blueprint-v2.md) bleibt die konzeptionelle Grundlage und ist keine Behauptung vollständig umgesetzter Funktionen.

## Vorhanden

| Bereich | Implementiert |
|---|---|
| Betrieb | Ein Docker-Image, Compose für lokalen HTTP-Test und HTTPS, NAS-/Traefik-Anleitung |
| Verwaltung | Betreiber, Betriebsstätten, Lagerorte und Benutzer; separates Administratorrecht, Versionen und unveränderliche Änderungshistorie |
| Rechte | Sitzungen, Origin/CSRF, Viewer/Helfer/Materialwart/Geräteverantwortlicher je Betriebsstätte |
| Material | Eingang, Umlagerung, Entnahme, Gegenbuchung, Inventur, Chargen und Verfall, unveränderliches Journal |
| Personenbewertung | Gerätespezifische Pflichten, konkrete Kontozuordnung von Einweisungen, befristete Qualifikations- und Gebrauchsanweisungszugangsnachweise, eigene fachliche Endanwenderabnahme, Widerrufe; Gesamtfreigabe weiter ungeklärt |
| Produktpflichten | Fachlich dokumentierte Anwendbarkeit von acht besonderen Pflichtbereichen, freigegebene Produktgrundlage, Befristung, Widerruf, ausdrückliche Wartungs-/Inbetriebnahme-/Einweisungsbindung, benannte Person mit fachlicher Einweisungsteilabnahme, besondere Funktionsprüfung mit Betriebsort, Prüfprotokoll und ausdrücklicher fachlicher Teilabnahme sowie qualifizierte Herstellerwartungsabnahme mit bestätigter Fälligkeit und aktuellen Dokumentprüfständen; Buchvollständigkeit, Benutzerzugang und Aufbewahrung mit schnappschussgebundener fachlicher Teilabnahme; getrennte STK-/MTK-/elektrische Teilabnahmen mit aktueller Kontrollregel, konkreter Prüfung und Protokoll; produktspezifische externe IT-Sicherheitsabnahme mit Softwarestand und Maßnahmenstatus; besondere Anwendergruppen mit ausdrücklich bewerteten Bedingungen und Ausnahmegrundlagen; Eignung des Aufbereitungsprozesses mit Validierung und Herstellervorgaben; konkrete Zyklen und separate Ergebnisfreigaben mit unabhängiger Aufbereitungssperre; strukturierte weitere Einzelpflichten mit Nachweisen / Vollständigkeit und fachlicher Teilabnahme; separate fachliche Erfüllungsabnahme aller acht Bereiche mit aktuellen Teilnachweisen; gesamte Einsatzfreigabe offen |
| Geräte | Akten, Stammdaten, Inbetriebnahme, versionierte Fristen, Prüfungen, getrennte Sperren/Freigaben, Einweisung, Instandhaltung, Außerbetriebnahme, ergänzende Richtigstellung |
| Rückrufe | Produkt-/Chargen-/Gerätebereiche, geprüfte Treffer-Vorschau, unabhängige Sperren, Standortmaßnahmen und kontrollierter Gesamtabschluss |
| Störungen | Störungserfassung und separate Meldebewertung mit Behörden-/Herstellerreferenzen; keine automatische Behördenmeldung |
| Packlisten | Versionierte Material-/Geräte-/Zubehöranforderungen, digitale Bewertung, getrennte physische Material-/Gerätechecks und zusammengeführte Teilvoraussetzungen |
| Erfassung | Geräte-/Material-Excelvorlagen, Barcode-Etiketten, USB-Eingabe, CSV-Vorschau und kontrollierter Import |
| Scanner | Interne Codes, GTIN/PZN/REF, begrenzte GS1-Auswertung; optionale native Browserkamera |
| Dokumente/Export | Quarantäneanhänge, optionale ClamAV-Prüfung, getrennte Freigabe/Widerruf und Prüfhistorie, CSV-Übersicht, vollständige Geräteaktenarchive mit Prüfmanifest |
| Erinnerungen | Arbeitsübersicht, persistente Kontrollerinnerungen, optionaler TLS-SMTP-Transport mit Versuchshistorie und Unsicherheitsbehandlung |
| Sicherung | Konsistenter SQLite-Snapshot einschließlich Anhängen; Restic-Job für verschlüsselte externe Sicherung und isolierte, manifest-/integritätsgeprüfte Wiederherstellung |

## Nächste Umsetzung

1. Vollständige Einsatzfreigabe: Vorhandene Material-, Funktions-, Qualifikations-, Gebrauchsanweisungs- und neue fachliche Produktpflichten-Erfüllungsnachweise abschließend für den konkreten Einsatz zusammenführen und ausdrücklich abnehmen. Gesamtfreigabe weiterhin `not_assessed`.
2. Restic-Sicherungsjob auf NAS / externem Speicher einrichten, vollständigen Wiederanlauf praktisch abnehmen und Betriebsüberwachung ergänzen. Verschlüsselte Sicherung und isolierte Rücksicherung sind als ausführbare Host-Jobs vorhanden.

## Weitere offene Funktionen

Packlistenalternativen, FEFO-Auswahlhilfen, differenzierte Anbruch-/Verfallsregeln, erweiterte Geräte-/Serienzuordnung über Scans, weitere Such-/Blätterfunktionen, administrative MFA und Auditorrechte. Spätere Pilotverbesserungen: PWA/Offline, OIDC, erweiterte GS1/UDI-Felder, signierte Archive und Beschaffung. Mehrmandantenbetrieb/PostgreSQL und spezielle Fachmodule sind gesonderte Erweiterungen. Aufbereitung, Arzneimittel und andere besondere Kategorien benötigen bei Aufnahme in den Umfang eigene bestätigte Regeln.

## Noch erforderliche Abnahme

Echter ClamAV-Dienst mit aktuellen Signaturen und realer Scannerabnahme, NAS-/Dockhand-/Traefik-Betrieb, USB-/Kamerascanner, eigener SMTP-Anbieter und Postfachzustellung, repräsentative Lasttests und vollständige Rücksicherung. Rechtlichen Betreiber, Pflichtbewertungen, Qualifikationen, Zuständigkeiten, Datenschutz, Aufbewahrung und Ausfall-Ersatzverfahren festlegen. Cloud- und lokale Entwicklungsprüfungen sind keine produktive Freigabe.

## Zuletzt geprüft

Schema 30, gerätebezogene Aufbereitungszyklen: Bedarf, Durchführung mit Abschlusszeitpunkt / Protokoll, getrennte Ergebnisfreigabe mit bestätigter Nutzungs-/Lagergültigkeit, Ablehnung und Widerruf. Unabhängige Aufbereitungssperre, aktuelle Prozess-/Unterlagenbindung, keine pauschale Steril- oder gesamte Einsatzfreigabe. Vollständige Go-Suite mit Race-Prüfung, `go vet`, Programm-/Docker-Build und Chromiumregression bestanden. Geprüft wurden Bedarfssperre / Verweigerung allgemeiner Sperrfreigabe, Abschlusszeitpunkt nach aktuellem Bedarf, eindeutige Referenz, separate Ergebnisfreigabe / Ablauf, neue Bedarfe, Fehlzyklen, Ablehnung und Widerruf, aktuelle Prozess-/Unterlagenbindung, unveränderliche Historie, atomarer Rollback, aktuelle Rechte vor historischer Wiederholantwort, sichere Archive und Zusammenspiel von Buch, Prozess und Zyklus. Verifiziertes HTTPS prüfte Migration 29 auf 30 zweimal, Protokollupload nach Prozessbewertung / Zyklus, ausdrückliche Freigabekriterien / Gültigkeit, Zyklusprotokollentzug / erneute Prüfung ohne Reaktivierung, frische Durchführung / Freigabe, unabhängige Defektsperre, Neustartpersistenz und unverändertes Bestandsjournal. Lokale und Compose-Datenbanken nach Sicherung ausdrücklich / wiederholbar migriert; Integrität / Fremdschlüssel geprüft und Daten in allen 75 bisherigen Tabellen erhalten, drei Tabellen ergänzt. Temporäres Konto deaktiviert; Scannerkonfiguration anschließend leer und SMTP deaktiviert, ClamAV-Protokoll simuliert. Externe Prozessparameter / reale Kompetenz / Sterilgutkriterien bleiben fachlich zu prüfen. Nur wiederherstellbarer Buildcache zur Speicherentlastung entfernt.


Rückrufbedienung und Berechtigungen: [Rückrufe](rueckrufe.md). Die Kombination aus Administratorrecht und Fachrolle erlaubt ausdrücklich globale Rückrufkoordination; Einzelmaßnahmen bleiben standortgebunden.

Personenbezogene Prüfung: [Bedienung, Rechte und Grenzen](personenbewertung.md). Sie erfasst konkrete Nachweise; sie bestätigt keine vollständige medizinische Nutzungsfreigabe.

Dokumentprüfung: [Bedienung, Scannerkonfiguration und Grenzen](dokumentpruefung.md).

Gebrauchsanweisungszugang und gemeinsame Bewertung: [Bedienung und Grenzen](gebrauchsanweisungszugang.md).

Besondere Produktpflichten: [Anwendbarkeit, Nachweise und Grenzen](produktpflichten.md).

Endanwenderabnahme: [Bedienung, Bindung und private Aktenbereiche](endanwenderbewertung.md).


Aufbereitungszyklen: [Bedienung, Freigabebindung und Upgrade auf Schema 30](aufbereitungszyklen.md).

Verschlüsselte Backups: [Einrichtung, Zeitplanung und isolierte Wiederherstellung](verschluesselte-backups.md).

Restic-Ergänzung: Echte verschlüsselte Sicherung / isolierte Rücksicherung einer Datenbankkopie mit bestätigten WAL-Daten geprüft; alle Tabellen und BLOBs verglichen. Vorhandenes Ziel, fehlende Quelle, inkonsistentes Manifest und falsches Passwort verweigert; vollständige Repository-Datenprüfung bestanden. Keine Änderung der aktiven Datenbank, des Schemastands oder der laufenden Anwendung. Externes Speicherziel, NAS-Zeitplanung und vollständiger Wiederanlauf bleiben praktisch abzunehmen.

Weitere Einzelpflichten: [Bedienung, Fachabnahme und Grenzen](weitere-produktpflichten.md).

Weitere Einzelpflichten geprüft: Vollständige Go-Suite mit Race-Prüfung, `go vet`, Programm-/Docker-Build und erweiterte Chromiumregression bestanden. Verifiziertes HTTPS prüfte vollständigen Pflichtumfang, konkrete erfüllte / begründet nicht anwendbare Einzelpflichten, Verweigerung unerfüllter / impliziter / doppelter Angaben ohne Speicherung, Personen- und Dokumentprüfstandsbindung, unabhängige Defektsperre / Ablehnung, aktuelle Rechte vor Wiederholantwort, sichere Geräteakten mit vollständigem SHA-Manifest und Neustartpersistenz. Bestandsjournal unverändert; alle bisherigen Daten in 78 Tabellen erhalten (Sitzungen und Erinnerungsworker separat veränderlich). Schema 30 / Integrität / Fremdschlüssel bestätigt; neues Vorabbackup und verifizierter Abschlusssnapshot vorhanden. Temporäres Konto deaktiviert, Scannerkonfiguration wieder leer; SMTP deaktiviert, ClamAV-Protokoll simuliert. Keine zusätzliche Migration; übergeordnete Pflichtabnahme / Gesamtfreigabe bleiben offen.

Fachliche Erfüllungsabnahme: [Bedienung, Bindungen und Upgrade auf Schema 31](erfuellungsabnahme.md).

Erfüllungsabnahme (Schema 31) geprüft: Vollständige Go-Suite mit Race-Prüfung, `go vet`, Programm-/Docker-Build und erweiterte Chromiumregression bestanden. Vorprüfung und ausdrückliche vollständige Abnahme aller acht Bereiche, fehlende / unqualifizierte Teilnachweise, bekannte Kontrollausnahmen, unbekannte / überschrittene Kontrollpflichten, atomarer Rollback, gemeinsamer Schlüsselraum, Befristung, Dokumentprüfstand, neue Anwendbarkeitsrevision, unabhängige Sperre, Ablehnung / Widerruf und aktuelle Rechte vor historischer Antwort geprüft. Packlisten zeigen aktuelle Produktpflichten-Erfüllung getrennt von Material und konkreten Endanwendern; Gesamtfreigabe bleibt offen. Verifiziertes HTTPS prüfte Migration 30 auf 31 zweimal, Vorabbackup, acht Bereichsbestätigungen, unveränderte Anwendbarkeits-Neubewertung / Dokumententzug ohne Reaktivierung, erneute Fachabnahme, Archivmanifest, Neustartpersistenz und unverändertes Bestandsjournal. Lokale und Compose-Datenbanken nach Sicherung ausdrücklich migriert, Integrität / Fremdschlüssel geprüft; alte Daten in 78 Tabellen erhalten (Sitzungen und Erinnerungsworker separat veränderlich), drei Tabellen ergänzt. Restic-Rücksicherung inklusive WAL / sämtlicher Tabellen auch mit Schema 31 bestanden. Temporäres Konto deaktiviert; Scannerkonfiguration wieder leer, SMTP deaktiviert, ClamAV-Protokoll simuliert. Nur regenerierbarer Docker-Buildcache entfernt.
