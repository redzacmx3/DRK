# Projektstart

## Stand des ersten Meilensteins

Implementiert: Go-Dienst, eingebettete HTML/JS-Oberfläche, explizite SQLite-Migrationen v1–v15, synthetische Betreiber-/Betriebsstätten-/Lagerortdaten und lesende Lagerort-API. HTTPS-Anmeldung mit Argon2id, widerrufbaren Sessions und Viewer-Rechten je Betriebsstätte schützt die Lagerortliste und direkte Objektzugriffe. Lokale CLI für Kontoanlage, Bereichsänderung, Kontosperre und Artikel-/Chargenanlage. Materialfluss mit Wareneingang, atomarer Umlagerung und Entnahme, Quellversionen, Idempotenz, Bewegungsjournal/Audit und Outbox ist umgesetzt. Die Oberfläche zeigt physische/nutzbare Mengen und erlaubt Buchungen je Rolle. Vollständige Gegenbuchungen und versionierte Inventuren mit Pflichtbegründung, Nullzählung und unveränderlichem Journal sind ergänzt. Versionierte Material-Sollpacklisten mit Mindestrestlaufzeit, bereichsbezogener Materialbewertung und unveränderlichen physischen Prüfnachweisen sind umgesetzt. Eine vollständige Einsatzfreigabe einschließlich Gerätebewertung ist noch offen. GTIN/PZN/REF-Zuordnung mit Packungsumrechnung und Widerruf, begrenzter GS1-Parser, geschützte Scanauflösung, bestätigte Übernahme ins Buchungsformular und druckbare interne QR-Etiketten sind ergänzt. Scanner-/Texteingabe ist browsergetestet, optionale native Kameraerkennung bleibt hardwareabhängig. SQLite-Treiber: mattn/go-sqlite3 mit CGO. Die Readiness prüft das Schema und Datenbankzugriff. Dockerfile und Compose-Entwicklungskonfiguration sind vorhanden.

Offen: weitere Rollen und Fachrechte, Geräte-/Zubehörbewertung, Packlistenalternativen, erweiterte Scanner-/UDI-Felder und Geräte-/Serienverfolgung, PWA-Funktionen, Dokumente und vollständige Backup/Restore-Abnahme. Ein SQLite-Snapshot vor Migrationen ist über die CLI möglich. Die aktuelle Oberfläche ist nur für synthetische Entwicklungsdaten vorgesehen.

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

Danach folgen Geräteakte, Fristen, vollständige Geräte-/Zubehörpacklisten, erweiterte Scannerfelder und Exporte gemäß Blueprint. Ein separates Frontend-Buildwerkzeug wird aktuell nicht benötigt; die Oberfläche wird direkt eingebettet. Der Produktionsbetrieb bleibt offen. Das Blueprint-Compose-Beispiel ist weiterhin konzeptionell; die Compose-Datei im Repository dient der Linux-Entwicklung.

## Vor Pilotbetrieb zu klären

Betreiber und Betriebsstätten, Produktumfang und Pflichtbewertungen, qualifizierte Zuständigkeiten, Kontrollregeln, Datenschutz/Aufbewahrung und betrieblicher Ersatzprozess. Diese Angaben blockieren den Start mit synthetischen Daten nicht.

## Gerätefristen: Berechnungsgrundlage

Die Fristenengine ist mit Kalender-, Schaltjahr-, Berlin-Tageswechsel-, Ausnahme-, Anlass- und STK-Höchstfristtests ergänzt. Sie speichert im Ergebnis Regelversion, Basisdatum, Nachweisreferenz und berechneten Termin. Ungeklärte Anwendbarkeit oder fehlende Basis bleiben `UNKNOWN`; nachgewiesene Ausnahmen sind ausdrücklich `NOT_APPLICABLE`. Eine gültige Kontrollfrist hebt keine Gerätesperre auf.

Nächster Integrationsschritt: persistente Geräteakten, bereichsbezogene Geräteverantwortliche, versionierte Regeln und unveränderliche Prüfnachweise; anschließend Sperren und Oberfläche. Noch keine Geräte-API und keine vollständige Geräte-Einsatzbewertung. Keine Schemaänderung in diesem Schritt.


## Geräteakte: Registrierung und Bereichsrechte

Schema 7 ergänzt unveränderliche Geräteregistrierungen, Historieneinträge, Outbox und Wiederholschutz. Die Rolle `device_manager` erhält Geräteanlage innerhalb zugewiesener Betriebsstätten ohne Material-Schreibrechte. Inventarnummern sind eindeutig, Serien-/Losnummern nicht global eindeutig. Bereichsbezogene Geräteübersicht und direkter Registrierungsnachweis sind verfügbar; die Oberfläche liest die Geräteübersicht.

Die Compose-Migration wurde mit vorherigem Snapshot und Erhalt der bisherigen Material-/Scannerhistorie geprüft. Weitere Integration: versionierte Kontrollregeln, Inbetriebnahme, Einweisungs-/Prüfnachweise und Fristberechnung je Gerät; anschließend unabhängige Sperren/Freigaben und Bearbeitungsoberfläche. Registrierte Geräte erscheinen bis dahin ausdrücklich ungeklärt.


## Inbetriebnahme und Kontrollregeln je Gerät

Schema 8 speichert versionierte Inbetriebnahme-/Kontrollereignisse mit Referenzen für Nachweis, Qualifikation und betrieblichen Auftrag. Die Geräteakte berechnet STK/MTK/elektrische Fristen beim Lesen, hält ungeklärte Pflichten separat und erteilt weiterhin keine vollständige Einsatzfreigabe. Die Oberfläche erfasst Nachweise mit eingefrorener Geräteversion und identischem Wiederholversuch bei Verbindungsverlust.

Geprüft sind Grenzfristen, Ausnahmen, fehlerhafte Regeln, veraltete/parallele Änderungen, Historienunveränderlichkeit, Transaktionsrollback, Bereichsrechte, Browserabläufe und ein echter Compose-Schema-7-zu-8-Übergang mit Backup und unveränderten Material-/Scannerhistorien. Nächster Schritt: wiederkehrende Prüfungen mit neuen Fristbasen und unabhängige Defekt-/Rückrufsperren; anschließend ausdrückliche Freigaben, Einweisungen und Dokumentanhänge.


## Wiederkehrende Prüfungen und einzelne Sperrfreigaben

Schema 9 erweitert die Gerätehistorie um Prüfungen, Defekt-/Rückruf-/Quarantänemeldungen und ausdrückliche Freigaben einzelner Sperren. Bestandene Prüfungen setzen eine neue Fristbasis ausschließlich für die geprüfte Kontrollart, fehlgeschlagene Prüfungen erzeugen eigene Sperren. Auch nach bestandener Prüfung bleiben diese sowie unabhängige Rückruf-/Defektsperren bestehen. Der Gerätestatus nennt aktive Sperren, Fristen und weiterhin ungeklärte vollständige Einsatzbereitschaft.

Prüfdatum, Prüfer/Firma, Methode/Protokoll, Ergebnis, Qualifikations-/Auftragsreferenzen und Berechnung werden unveränderlich dokumentiert. Helfer melden Defekte ohne Prüf- oder Freigaberecht. Die Oberfläche unterstützt die Abläufe mit geladenem Versionsstand und identischen Wiederholungen. Geprüft: Rechte, Fristbasen, unabhängige Sperren, exakte Folgefristen, Anlassfristen, Zurückdatierung, Rollback, Browserabläufe sowie Compose-Upgrade mit vorherigem Snapshot und Erhalt der bestehenden Geräte-/Materialhistorie.

Nächste Schritte: Einweisungsnachweise, Dokumentanhänge, vollständige Geräte-/Zubehör-Sollbewertung und Einsatzbereitschaft. Der Qualifikations-/Dokumentinhalt wird noch nicht unabhängig geprüft; Produktionsabnahme und Benachrichtigungen bleiben offen.


## Einweisungsnachweise und Dokumentenspeicher

Schema 10 ergänzt strukturierte Einweisungen mit Personen, Datum, Gerät-/Bauartgeltung, belegter Übertragbarkeit und Unterlagenversion. Der Nachweis bindet die feste Dokument-ID und SHA-256 an diese Geräteakte. Die Oberfläche unterstützt Upload, ausdrücklich gewählten Anhang und Einweisung mit Version-/Wiederholschutz.

Anhänge bis 4 MiB liegen für den Entwicklungsstand atomar als SQLite-BLOB samt Metadaten/Audit/Outbox vor. Damit sichert der bestehende Datenbanksnapshot auch Dateien. Uploads werden begrenzt auf Format/Signatur beziehungsweise Decode geprüft, verbleiben aber mangels Malware-Scanner in Quarantäne. Nur berechtigte Geräteverantwortliche erhalten einen ausdrücklichen Prüfdownload; keine Freigabe oder automatische positive Einsatzbewertung. Dieses begrenzte Speichermodell weicht bewusst vom konzeptionellen separaten Dateispeicher des unveränderten Blueprints ab.

Geprüft: Typ-/Größen-/Dateinamenprüfung, unveränderliche Dateien/Prüfsummen, Bereichsrechte/Quarantäne, atomare Fehlerbehandlung, Einweisungs-/Bauart-/Dokumentbindung, Browser-Wiederholung, Compose-Migration mit Historienerhalt und separater Snapshot-Restore mit Inhalt/Hash/Fremdschlüsseln. Weiter offen: Malwareprüfung/Dokumentfreigabe, vollständige Geräte-/Zubehör-Sollbewertung und Einsatzbereitschaft, Korrektur-/Aufbewahrungsverfahren und Produktionsabnahme.


## Geräte-/Zubehör-Soll und digitale Bewertung

Schema 11 ergänzt konkrete Geräte und zugeteilte Zubehörmengen in unveränderlichen Sollversionen. Zubehörmengen werden über alle Geräte addiert und müssen durch die Material-Sollposition gedeckt sein. Die Oberfläche erfasst Geräte/Zubehör und zeigt eine zusätzliche aktuelle Bewertung mit konkreten Sperr-, Frist-, Nachweis- und Bestandsgründen.

Materialprüfungen bleiben ausdrücklich Materialprüfungen; technische Änderungen veralten ihren Hash nicht. Der neue Assessment-Endpunkt kombiniert ihren aktuellen Stand mit aktuellen Geräteakten, ohne vollständige Einsatzfreigabe zu behaupten. Physische Geräte-/Funktionsprüfung und konkrete Nutzer-/Einweisungspflichtbewertung bleiben offen. Leeres Geräte-Soll ist keine bestätigte Ausnahme.

Geprüft: Mengen-/Duplikat-/Ortsschranken, atomare Veröffentlichung, Wiederholantwort, unabhängige Defekt-/Prüf-/Freigabebewertung, Bereichsrechte, Browserformulare und Compose-Migration mit Erhalt sämtlicher Material-/Geräte-/Dokumenthistorien sowie bisheriger Material-Prüfhashes. Nächster Schritt: versionierte physische Geräte-/Zubehör-/Funktionsprüfnachweise und deren Gültigkeitsbasis.


### Physische Geräteprüfung in Sollpacklisten

Schema 12 speichert unveränderliche Geräteprüfnachweise, Akteur, Zeitpunkt, Prüfnotiz, Ergebnisse und Datenbasis atomar mit eigener Outbox und exakter Wiederholantwort. `GET /api/v1/packlists/{id}/assessment` liefert `device_basis_hash` und `last_device_check`; `GET .../device-checks` liefert die letzten 50 Nachweise (weitere Einträge: `X-Has-More`). Die Oberfläche zeigt aktuelle/veraltete Nachweise und Historie.

`POST /api/v1/packlists/{id}/device-checks` benötigt `basis_hash`, `note`, sowie genau einen `results`-Eintrag pro Sollgerät mit `device_id` und den ausdrücklichen booleschen Ergebnissen `present`, `functional`, `accessories_complete`. Helper und Materialverantwortliche dürfen innerhalb ihres Bereichs operative Sicht-/Funktionsprüfungen dokumentieren; dies ersetzt keine STK/MTK, fachliche Kontrollberechtigung oder Einweisung. Auch ohne Zubehör-Soll muss dessen Anwendbarkeit bei der Prüfung ausdrücklich bestätigt werden. CSRF, Sitzung, Bereich und gemeinsamer Benutzer-/Idempotenzschlüsselraum werden geprüft. Verlorene Antworten lassen sich mit identischem Schlüssel und Inhalt wiederholen; bei geänderter Basis muss neu geprüft werden.

Die Datenbasis umfasst Sollversion, Materialbasis mit relevanten Bestandsversionen, Berliner Tagesdatum sowie Geräteakten einschließlich Versionsstand, Fristen, Sperren und Einweisungen. Bestands-/Geräteänderungen, Tageswechsel, ersetzte Sollversion oder Ablauf der konfigurierten Prüfgültigkeit machen den Nachweis veraltet. Ein aktueller negativer Nachweis blockiert die Bewertung und nennt fehlendes Gerät, Funktionsabweichung oder unvollständiges Zubehör. Ein aktueller positiver Nachweis kann `physically_checked` ergeben; bestehende digitale Sperren, überfällige Kontrollen und ungeklärte Daten bleiben wirksam. Die Materialprüfung bleibt separat. `overall_readiness` bleibt stets `not_assessed`: Nutzerqualifikation, Anwendbarkeit rechtlicher Pflichten und vollständige Einsatzfreigabe sind weiterhin offen. Diese operative Prüfung ist kein Nachweis einer gesetzlichen Funktionsprüfung nach § 11 MPBetreibV.


## Schnelle Erstinventarisierung (Schema 13)

[Excel-Vorlage und Anleitung](erstinventarisierung.md) erlauben eine vorbereitende Erfassung von Geräten und Equipment mit wenigen Pflichtdaten, festen Inventarnummern, gedruckten Code-39-Etiketten und USB-Scanner-Suche. Pro neuer Vorlage werden neue Nummern erzeugt, ohne Makros, Barcode-Font oder externe Dienste. CSV-Vorschau mit Standortzuordnung, Dubletten-/Konfliktprüfung, eingefrorener Datenbasis und atomarem Commit ist implementiert. Gleiche importierte Zeilen werden übersprungen, vorhandene Daten nie überschrieben. Importierte Defekte/Quarantäne bleiben unabhängige Sperren. CSV erteilt keine Einsatzfreigabe; Materialimport folgt als eigener Schritt.

## Verbrauchsmaterial-Erfassung (Schema 14)

Separate Excel-Vorlage mit festen Erfassungsnummern, Code-39-Etiketten, USB-Scanner-Suche und Textfeldern. CSV-Vorschau mit ausdrücklicher Standortzuordnung und Importart Erstbestand/Zugang. Erstbestand nur für noch nicht erfasste Artikelbestände am Zielort, keine automatische Bestandskorrektur. Neue Artikel und Chargen werden ausdrücklich angezeigt; abweichende Einheiten/Verfallsdaten und mehrdeutige Namen werden abgelehnt. Unbekannte Charge oder Verfall wird quarantänisiert. Ein Import persistiert Artikel/Charge, Bestandszugang, Journal/Audit/Outbox und dauerhafte Herkunftskennung atomar. Gleiche Zeilen bleiben auch nach Entnahme/Gegenbuchung ohne erneute Buchung. Berechtigung: Materialwart im zugewiesenen Bereich. Anleitung: [Verbrauchsmaterial-Erfassung](verbrauchsmaterial-erfassung.md).

## Geräte-Grunddatenänderung (Schema 15)

Versionierte Ergänzung/Korrektur von Bezeichnung, Hersteller, Modell und Serien-/Losnummer durch Geräteverantwortliche. Unveränderliche Registrierung und gemeinsame Geräteereignisse mit Vorher-/Nachher-Werten, Akteur, Zeit, Nachweis und Begründung. Bei Identifikationsänderung nach technischen Nachweisen entsteht eine eigene Quarantänesperre; keine automatische technische Freigabe. UI mit vorgeladenen Werten, Änderungshistorie und unverändertem Wiederholschlüssel bei unklarer Antwort. CSV-Wiederimporte setzen ergänzte Grunddaten nicht zurück.

## Lesender Geräteübersichts-Export

Geschützter CSV-Download aus einem konsistenten Datenbank-Snapshot mit aktuellem Grunddatenstand, Version, Betriebsstätten-/Betreiberbezug, Fristen, Sperren und Anzahl der Einweisungsnachweise. Oberfläche enthält einen direkten Downloadlink. Formelähnliche Zellen und numerische Identifikationsnummern erhalten Textschutz. Keine Migration; begrenzter Überblick, kein vollständiger Medizinproduktebuch-Export oder Einsatzfreigabenachweis. Weiterer Schritt: vollständiger lesbarer Gerätebuch-Export mit Fachhistorie und Nachweismanifest.

## Geräteaktenarchiv mit Nachweismanifest

ZIP-Export pro Gerät mit offline lesbarer/druckbarer HTML-Akte, vollständigen strukturierten Fachdaten und SHA-256-Manifest. Registrierung, Geräteereignisse in Versionsfolge, aktuelle Regeln/Fristen/Sperren/Einweisungen, gerätebezogene Importherkunft und Anhangsmetadaten/-ereignisse stammen aus demselben Datenbankstand. Quarantäne-Dateien bleiben ausgeschlossen; weder technische Freigabe noch bestätigte rechtliche Vollständigkeit. Grenzen führen zum Abbruch statt zu einer gekürzten Historie. Nächste fachliche Schritte: fehlende Bestandsverzeichnisangaben und versionierte Instandhaltungsnachweise.
