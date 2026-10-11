# Entwicklungshistorie

Historische Meilensteine und damalige Prüfungen. Aussagen wie „als Nächstes“ oder „offen“ beziehen sich auf den jeweiligen Abschnitt, nicht auf den aktuellen Gesamtstand. Maßgeblich ist [Stand und Roadmap](stand-und-roadmap.md).

# Projektstart

## Stand des ersten Meilensteins

Implementiert: Go-Dienst, eingebettete HTML/JS-Oberfläche, explizite SQLite-Migrationen v1–v22, synthetische Betreiber-/Betriebsstätten-/Lagerortdaten und lesende Lagerort-API. HTTPS-Anmeldung mit Argon2id, widerrufbaren Sessions und Viewer-Rechten je Betriebsstätte schützt die Lagerortliste und direkte Objektzugriffe. Lokale CLI für Kontoanlage, Bereichsänderung, Kontosperre und Artikel-/Chargenanlage. Materialfluss mit Wareneingang, atomarer Umlagerung und Entnahme, Quellversionen, Idempotenz, Bewegungsjournal/Audit und Outbox ist umgesetzt. Die Oberfläche zeigt physische/nutzbare Mengen und erlaubt Buchungen je Rolle. Vollständige Gegenbuchungen und versionierte Inventuren mit Pflichtbegründung, Nullzählung und unveränderlichem Journal sind ergänzt. Versionierte Material-Sollpacklisten mit Mindestrestlaufzeit, bereichsbezogener Materialbewertung und unveränderlichen physischen Prüfnachweisen sind umgesetzt. Eine vollständige Einsatzfreigabe einschließlich Gerätebewertung ist noch offen. GTIN/PZN/REF-Zuordnung mit Packungsumrechnung und Widerruf, begrenzter GS1-Parser, geschützte Scanauflösung, bestätigte Übernahme ins Buchungsformular und druckbare interne QR-Etiketten sind ergänzt. Scanner-/Texteingabe ist browsergetestet, optionale native Kameraerkennung bleibt hardwareabhängig. SQLite-Treiber: mattn/go-sqlite3 mit CGO. Die Readiness prüft das Schema und Datenbankzugriff. Dockerfile und Compose-Entwicklungskonfiguration sind vorhanden.

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

## Versionierte Bestandsverzeichnis-Stammdaten (Schema 16)

Geräteart, Anschaffungsjahr, zuständiger Wirtschaftsakteur mit ausdrücklich gewählter Funktion/Name/Anschrift und betriebliche Zuordnung sind strukturiert ergänzt. Leere Angaben bleiben unbekannt. Änderungen mit Grund und Nachweis speichern Vorher-/Nachher-Werte, ohne Registrierung oder technische Nachweise zu überschreiben. Oberfläche lädt bestehende Werte, friert unbestätigte Anfragen ein und zeigt Historie; CSV und Geräteakten-ZIP berücksichtigen aktuelle Angaben und verbleibende Datenlücken. Die Stammdatenergänzung ist keine technische Freigabe. Nächster Schritt: versionierte Instandhaltungsnachweise mit Ausführenden, Datum, Ergebnis und Nachweisreferenz.

## Instandhaltungsnachweise (Schema 17)

Versionierte Nachweise für Instandhaltung, Wartung und Reparatur sind mit Datum, Ausführendem, Maßnahme, Ergebnis, Qualifikations-/Auftrags-/Nachweisreferenz und optionalem gerätegebundenem Quarantäneanhang ergänzt. Die Oberfläche unterstützt ausdrücklich gewählte Ergebnisse, eingefrorene Version und identischen Wiederholversuch. Aktenarchiv und CSV-Überblick enthalten die Ergänzungen.

Fehlschlag und Nacharbeitsbedarf erzeugen unabhängige Defekt- beziehungsweise Quarantänesperren; erfolgreiche Maßnahmen heben keine Sperren auf und setzen keine Kontrollfristen zurück. Ein neuer Nachweis veraltet physische Geräteprüfungen, ohne Materialprüfungen zu verändern. Alte Akten ohne neue Ereignisse behalten ihre bisherigen Prüfbasiswerte. Bestehende Installationen benötigen Backup und ausdrückliche Migration auf Schema 17.

Geprüft: Browserformular mit Anhangsauswahl, eingefrorenem Wiederholversuch und sicherer Textdarstellung; echte HTTPS-Compose-Migration von 16 auf 17 mit Backup und wiederholter Migration, unveränderten alten Beständen/Historien/Prüfbasiswerten, unabhängigen Sperren, ausdrücklicher Freigabe, gebundenem Dokumenthash, ZIP-Prüfsummen, Neustartpersistenz und Bereichsrechten. Hardware-/Produktivbetrieb bleibt separat zu prüfen.

## Außerbetriebnahme und Aufbewahrungsanzeige (Schema 18)

Versionierte Außerbetriebnahme mit Datum, Grund und Nachweis ergänzt. Historie, Anhänge, Kontrollfristen und unabhängige Sperren bleiben erhalten. Die Sollbewertung blockiert außer Betrieb genommene Geräte; einzelne Sperrenfreigaben reaktivieren sie nicht. Neue technische Nachweise sind gesperrt, administrative Ergänzungen bleiben möglich. Geräteakte, Übersicht und Exporte zeigen einen vorsorglichen Mindest-Aufbewahrungstermin fünf Kalenderjahre später (29. Februar konservativ bis 1. März einschließlich). Keine automatische Löschung, individuelle Buchpflicht oder vollständiger Datenschutz-/Aufbewahrungsplan abgeleitet. Wiederinbetriebnahme und ergänzende Fachkorrekturen bleiben weitere Schritte.

Geprüft: Go-Race-Suite, Vet/Build, zusätzliche Außerbetriebnahme-/positive-Physiknachweis-Tests, Browserformular mit exakter Wiederholung nach Verbindungsabbruch sowie echte HTTPS-Compose-Migration 17→18 mit Backup und Wiederholung. Alte Bestände, Historien und Prüfbasiswerte blieben unverändert; CSV/ZIP-Mindesttermin, keine Reaktivierung durch Sperrenfreigabe, Neustartpersistenz und Bereichsrechte bestanden.

## Ergänzende Richtigstellungen (Schema 19)

Unveränderliche Fachkorrekturen mit Bezug zur Ursprungsversion, richtiggestellten Angaben, verantwortlichem Akteur, Grund, Qualifikations-/Auftrags-/Nachweisreferenzen und optionalem gebundenem Quarantäneanhang ergänzt. Original-ID/Aktion/Akteur/Zeit/Payload-Prüfsumme werden festgehalten; technische Originalfelder bleiben erhalten. Jede Richtigstellung erzeugt eine eigene Quarantänesperre zur fachlichen Klärung. Mehrere Ergänzungen beziehen sich auf denselben Ursprung; keine Überschreibung oder automatische Frist-/Sperren-/Lebenszyklusänderung. Weitergehende technische Ersatznachweise und Wiederinbetriebnahme bleiben offen.

Oberfläche mit expliziter Ursprungsauswahl, mehrzeiligem Text und identischem eingefrorenem Wiederholversuch; Geräteakte/Archiv zeigen Ursprung und Korrektur, CSV nennt Anzahl. Nach Außerbetriebnahme weiterhin möglich, ohne Reaktivierung oder kürzere Aufbewahrung. Neue Geräte-Prüfbasis veraltet physische Gerätenachweise; Materialbasis bleibt unabhängig. Vor Update Backup und ausdrückliche Migration 19.

Geprüft: gezielte Race-Tests für Ursprungsbindung/Hash, mehrzeilige sichere Texte, atomaren Rollback, Bereichsrechte, unabhängige Korrektursperren und unveränderte Technik-/Materialbasis; Browserauswahl mit deutschen Fachbezeichnungen und exaktem Wiederholversuch; echte HTTPS-Compose-Migration 18→19 mit Backup/Wiederholung und Erhalt alter Bestände/Historien/Prüfbasiswerte, gebundenem Anhang, Richtigstellung nach Außerbetriebnahme, ZIP-Prüfsummen/CSV-Zählung, Neustartpersistenz und Berechtigungsprüfung.

Die vollständige Go-Race-Suite, Vet und Build bestanden ebenfalls. Lokale Sicherung und wiederholte Migration auf 19 durchgeführt. Produktivbetrieb und neue Cloud-Wiederherstellung bleiben separat zu prüfen.

## Störungen und Meldebezüge (Schema 20)

Strukturierte Störungen, wiederholte gleichartige Bedienungsfehler und Verdacht auf schwerwiegendes Vorkommnis mit Datum, Beschreibung, Folgen/geschützter Fallreferenz und Nachweis ergänzt. Helfer erfassen Störungen, Geräteverantwortliche dokumentieren die separate Meldebewertung mit Status, Verantwortung, Abschlussentscheidung sowie getrennten Behörden-/Herstellerreferenzen und erfolgten Meldezeitpunkten. Herstellerkontakt ist keine Behördenmeldung; keine automatische externe Zustellung oder rechtliche Entscheidung. Der externe unverzügliche Meldeprozess darf nicht auf App-Freigaben warten.

Jede Störung erzeugt eine unabhängige Defekt-/Quarantänesperre. Offene Meldebewertungen blockieren die Gerätebewertung auch nach Freigabe einzelner Sperren; ein dokumentierter Abschluss hebt Sperren nicht auf. Versionierte Ereignisse mit Akteur/Zeit, Nachweisen, optionalen quarantänisierten Anhängen und exakten Wiederholantworten bleiben erhalten. Meldestatus wird aus der Historie projiziert; leere Werte können bestehende externe Meldebezüge nicht entfernen. Nachträge nach Außerbetriebnahme reaktivieren nichts. Oberfläche mit vorgefüllten Bezügen und Ortszeit-Erfassung, komplette Aktenarchive und zusätzliche CSV-Zählspalten ergänzt. Vor Update Backup und ausdrückliche Migration 20.

Geprüft: vollständige Go-Race-Suite, Vet/Build, gezielte Tests für Helferrechte/Manager-Meldungen, getrennte Behörden-/Herstellerbezüge, Zeit-/Datumsprüfung, Abschlussreferenz, atomaren Rollback, unveränderliche Historie, Sperren und unabhängige Materialbasis. Browser prüft sichere Texte, Ortszeit-Übertragung, Vorbelegung und Erhalt ursprünglicher Meldezeitpunkte, eingefrorene Felder und identische Wiederholungen. Echte HTTPS-Compose-Migration 19→20 mit Backup/Wiederholung erhielt alte Bestände, Historien und Prüfbasiswerte; Störung/Meldung/Abschluss ohne automatische Sperrenfreigabe, Verbot leerer alter Bezüge, CSV/ZIP-Prüfsummen, Neustartpersistenz und Bereichsrechte bestanden. Lokales Backup und wiederholte Migration bestanden ebenfalls. Hardware-/Produktivbetrieb und externe Meldepflichtbewertung bleiben separat zu prüfen.

## Geräte-Arbeitsübersicht

Die Oberfläche bündelt überfällige und anstehende Kontrollen, ungeklärte Kontrollpflichten, fehlende Inbetriebnahmenachweise, aktive Sperren und offene Meldebewertungen im eigenen berechtigten Bereich. Zeitraum: heute, 7, 30, 60 oder 365 Tage; Kategorien sind filterbar. Fälligkeit am heutigen Berliner Kalendertag bleibt fristgerecht und erscheint als heute fällig. Zeitumstellungen verändern die Tagesabstände nicht. Zähler zählen einzelne Hinweise; ein Gerät kann mehrere Hinweise haben.

`GET /api/v1/devices/attention?days=60` akzeptiert 0–365 Tage und liefert den aktuellen Stand aus einer gemeinsamen Datenbanksicht. Meldebewertungen und Sperren stehen zuerst. Außer Betrieb genommene Geräte erhalten keine Kontrolltermine oder Inbetriebnahmehinweise, behalten aber offene Sperren und Meldeaufgaben. Über 1000 berechtigte Geräte, 8 MiB oder bei Zeitüberschreitung wird die gesamte Übersicht abgelehnt; es gibt keine still gekürzte Übersicht. Zwei parallele Übersichten/Aktenexporte sind gemeinsam zugelassen.

Diese Arbeitsübersicht ist rein lesend und benötigt keine eigene Migration. Der ergänzte Erinnerungsworker verwendet Schema 21; Optionale E-Mail-Zustellung ist inzwischen mit Schema 22 ergänzt. Die Anzeige bestätigt keine Einsatzfreigabe. Nach Änderungen aktualisiert sie sich; verspätete Antworten dürfen einen neueren Zeitraum oder eine Abmeldung nicht überschreiben.

Geprüft: Go-Race-Suite, Vet/Build, Bereichsrechte, unveränderte Ereignisse, Kalendergrenzen einschließlich Zeitumstellungen, Grenze gegen Teilanzeigen sowie Browserfilter und verspätete Antworten. HTTPS-Compose-Test mit geprüftem Zertifikat bestätigte vier Zeiträume, alle Leserrollen, leeren Bereich und unveränderte Historien/Bestände.

## Automatische Kontrollerinnerungen (Schema 21)

Ein Hintergrundlauf im bestehenden Dienst startet sofort und anschließend stündlich. Er erzeugt dauerhaft gespeicherte Erinnerungen bei 60/30/7 Tagen vor Fälligkeit, am Fälligkeitstag und nach Überschreitung. Berliner Kalendertage bleiben maßgeblich. Nach ausgefallenen Läufen wird einmal die aktuell dringlichste Schwelle erfasst; versäumte Vorwarnungen werden nicht nachträglich als Serie erzeugt. Je Gerät, Kontrolltyp, belegtem Kontrollzyklus und Schwelle gibt es höchstens einen Eintrag samt eigener Outbox. Wiederholungen, konkurrierende Läufe und Neustarts erzeugen keine Doppelhinweise. Ein einziger Überfälligkeitshinweis bleibt bestehen, ohne tägliche Wiederholungen.

Neue Prüf-/Regelnachweise können einen neuen Kontrollzyklus bilden, auch bei gleichem Fälligkeitstag. Administrative Grund-/Stammdatenänderungen allein tun dies nicht. Frühere Einträge und Outbox bleiben erhalten; ihr aktiver Zustand wird aus aktuellen Akten abgeleitet. Außerbetriebnahme, unbekannte oder ausgenommene Kontrollpflichten erzeugen keine Kontrollerinnerungen. Ein Fehler verwirft den gesamten Lauf einschließlich Outbox und Erfolgszeitpunkt. Geräteversionen, Fachhistorien, Sperren und Prüfbasiswerte werden durch den Worker nicht geändert.

Die Oberfläche zeigt Erzeugungszeit, Schwelle, ursprüngliche Geräteversion und den letzten erfolgreichen Lauf. Bei mehr als zwei Stunden ohne bestätigten Lauf erscheint ein Hinweis. `GET /api/v1/devices/reminders` liefert nur aktive Erinnerungen im berechtigten Bereich und prüft die aktuelle Akte beim Lesen: veraltete Kontrollzyklen und außer Betrieb genommene Geräte verschwinden sofort, auch bevor der Worker wieder läuft. Aktuelle Fristen und Resttage werden beim Lesen berechnet. Die Arbeitsübersicht bleibt unabhängig verfügbar und bestätigt keine Einsatzfreigabe. Der SMTP-Transport ist inzwischen optional ergänzt; ohne ausdrückliche SMTP-Konfiguration bleibt die Zustellung deaktiviert.

Konfiguration in allen Compose-Dateien:

```dotenv
DRK_DEVICE_REMINDERS=true
DRK_REMINDER_DAYS=60,30,7
```

Zulässig sind 1–12 unterschiedliche positive Schwellen bis 365 Tage, getrennt durch Komma. Heute und überfällig bleiben zusätzlich enthalten. `false` stoppt die automatische Erzeugung, löscht aber keine gespeicherten Hinweise. CLI: `inventory remind-devices --db PATH --reminder-days 60,30,7` für einen ausdrücklichen Einmallauf; `serve --device-reminders=false` zum Abschalten. Nicht gleichzeitig mit abweichenden Schwellen betreiben. Grenzen: 1000 Geräte je atomarem Worker-Lauf, 3000 aktive berechtigte Erinnerungen je Übersicht, 8 MiB und zehn Sekunden. Bei Überschreitung keine Teilverarbeitung; letztes erfolgreiches Ergebnis bleibt erhalten.

Bestehende Installation: Dienst stoppen, neues konsistentes Backup anlegen und ausdrücklich auf Schema 21 migrieren, dann starten. `init` aktualisiert bestehende Datenbanken weiterhin nicht automatisch. Sicherungen enthalten auch Erinnerungen/Outbox; keine Volumes löschen.

Geprüft: vollständige Go-Race-Suite, Vet/Build, Schwellen-/Kalendergrenzen, Nachholen ohne Meldeserie, konkurrierende Läufe, deduplizierte Outbox, atomarer Rollback, Start/Abbruch, unveränderliche Nachweise, Bereichsrechte und Verbot von Teilverarbeitung. Browser prüft sichere Texte, Warnung bei altem Erfolgszeitpunkt, Fehlerbereinigung und verspätete Antworten nach Abmeldung. Echte HTTPS-Compose-Migration 20→21 mit Backup und Wiederholung erhielt Bestände, Historien, Anhänge, physische Nachweise und Geräte-/Materialprüfbasiswerte. Automatischer Start, Wiederholung ohne weitere Outbox-Einträge, Snapshot-Integrität, sofortiges Ausblenden nach Außerbetriebnahme und Neustartpersistenz bestanden. Lokales Backup, wiederholte Migration und Einmalläufe bestanden ebenfalls.

## Optionale SMTP-Zustellung (Schema 22)

Der bestehende Container kann Kontrollerinnerungen verschicken, wenn eine SMTP-Konfiguration ausdrücklich bereitgestellt wird. Standard: deaktiviert. TLS oder STARTTLS mit geprüftem Serverzertifikat ist erforderlich; HTTP im lokalen Browser ändert die SMTP-Sicherheit nicht. Empfänger werden ausdrücklich je Betriebsstätte festgelegt, ohne automatische Übernahme von Konto-Adressen. Details und Compose-Overlay: [SMTP-Anleitung](smtp-erinnerungen.md).

Zustellungen haben stabile Message-IDs, Empfängerbindung, kurze Datenbank-Leases und unveränderliche Versuchshistorien. SMTP läuft außerhalb der SQLite-Transaktionen. Aktuelle Kontrollzyklen, Schwellen, Außerbetriebnahme und Empfängerkonfiguration werden vor jedem Versandversuch geprüft. Vorbereitete überholte Hinweise werden verworfen. Eine Änderung während bereits laufender SMTP-Übertragung kann deren Annahme nicht atomar zurücknehmen.

SMTP-Annahme ist keine Bestätigung der Postfachzustellung. Temporäre Fehler werden mit 1/5/15/60 Minuten Abstand bis insgesamt fünf Versuchen wiederholt. Dauerhafte Ablehnung oder Zertifikatsfehler verlangen manuelle Klärung. Verlorene DATA-Antworten, abgebrochene Prozesse oder abgelaufene Versand-Leases bleiben als unklar stehen; keine automatische Wiederholung mit möglicher Doppelmail. Ein SMTP-Erfolg mit anschließend verlorener Datenbankbestätigung kann ebenfalls unklar werden. Exakt-einmalige externe Zustellung wird nicht zugesichert.

`GET /api/v1/devices/reminder-mail` und die Oberfläche zeigen die letzten 200 Zustellungen im berechtigten Bereich, Status, Versuche und Message-ID, ohne Empfängeradressen oder Zugangsdaten auszugeben. Weitere Einträge werden ausdrücklich angekündigt. Der Status bleibt auch nach Außerbetriebnahme sichtbar. Wiederaufnahme erfolgt über die lokale CLI; bei unklarer Annahme ist nach externer Prüfung eine ausdrückliche Bestätigung des Doppelmail-Risikos erforderlich. SMTP-Protokolltexte, Passwörter und Mailinhalte werden nicht geloggt. Die bisherigen Bewegungs-/Prüf-Outboxen werden nicht per E-Mail versendet. Bestände, Fristen, Sperren und Prüfnachweise bleiben von Zustellfehlern unabhängig.

Bestehende Installation vor Update sichern, ausdrücklich auf Schema 22 migrieren und anschließend starten. Sicherungen umfassen Warteschlange, Versuchshistorien und Empfängerbindungen; SMTP-Dateien/Secrets separat geschützt sichern.

Geprüft: vollständige Go-Race-Suite, Vet/Build, echte lokale TLS-/STARTTLS-Verbindungen einschließlich Anmeldung, geprüfte Zertifikate, fehlendes STARTTLS, Header-/Konfigurationsprüfung, 4xx/5xx, verlorene DATA-Antwort und QUIT-Abbruch nach Annahme. Warteschlange prüft Rollback, konkurrierende Worker, abgelaufene Leases, begrenzte Wiederholungen, stabile Message-IDs, manuelle Freigabe und Empfänger-/Fristenänderungen. Browser prüft Versandstatus, sichere Texte, Fehlerbereinigung und verspätete Antworten nach Abmeldung. HTTPS-Compose-Test mit lokalem TLS-SMTP-Testserver bestand einschließlich Backup/Migration 21→22, Standard ohne Versand, aktivem Overlay, Wiederholschutz, Neustartpersistenz, Snapshot-Integrität und Bereichsrechten. Frühere Bestände, Dokumente, Nachweise und Geräte-/Materialprüfbasiswerte blieben unverändert. Lokale Backup-/Migrationsprüfung bestand. Die Entwicklungsinstanz läuft wieder ohne SMTP-Konfiguration; reale SMTP-Konten und Postfachzustellung sind nicht geprüft.


## Verwaltung (Schema 23) – damalige Prüfung

Schema 23: vollständige Go-Tests mit Race-Prüfung, `go vet`, Programm- und Docker-Build sowie Chromium-Prüfungen der Oberfläche. Bestehende lokale und Compose-Datenbanken nach neuen Backups zweimal migriert. Über verifiziertes HTTPS organisatorische Änderungen, ausdrückliche Mehrfachrollen, Passwortwechsel, Sitzungswiderruf und dauerhafte Wiederholantworten einschließlich Neustart geprüft. Bestände, Journale, Akten, Anhänge und frühere Material-/Geräteprüfgrundlagen erhalten; Snapshot-Integrität und Fremdschlüssel geprüft. Temporäre Administratorkonten wieder entberechtigt und deaktiviert. Das ersetzt keine der oben genannten produktiven Abnahmen.
