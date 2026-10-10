# Geräte und Equipment schnell erfassen

Die [Excel-Datei](vorlagen/Geraete-Ersterfassung.xlsx) kann schon vor dem Programmeinsatz offline verwendet werden. Sie enthält 100 feste Inventarnummern mit passenden Code-39-Barcodes. Keine Makros, zusätzliche Barcode-Schriftart oder Internetverbindung nötig. Diese GitHub-Datei ist eine konkrete Erfassungsrunde: jede Kopie und jeder erneute Download enthält dieselben Nummern. Für weitere Gegenstände erzeugt das Programm eine **neue** Vorlage mit neuen Nummern. Die Datei nicht mit anderen Nummern weiterverwenden, sonst passen ihre Bilder nicht mehr zu den Nummern.

1. Excel-Datei unter einem eigenen Namen speichern und im Blatt **Inventar** pro Gegenstand eine Zeile ausfüllen. Inventarnummern stehen schon fest; sie sind vor versehentlichen Änderungen geschützt. Nur **Bezeichnung und Standort** ergänzen, um starten zu können.
2. **Art** (Gerät / Equipment), **Zustand** und bekannte Hersteller-, Modell- oder Seriennummern ergänzen. Unbekanntes leer lassen. Bemerkungen sind optional. Gleichartige Gegenstände erhalten jeweils eine eigene Zeile und Inventarnummer.
3. Im Blatt **Etiketten** die passenden Barcodes mit tatsächlicher Größe / 100 % drucken. Erst ein Etikett mit dem eigenen Scanner testen, dann am passenden Gegenstand anbringen. Blatt und Druckbereich lassen sich auf die benötigten Etiketten beschränken; die Druckfläche ist für A4 ausgelegt, kein festes Etikettenpapier vorgeschrieben.
4. Ein USB-Handscanner arbeitet als Tastatur: Code 39 aktivieren, Start-/Stoppsterne nicht mitsenden und als Abschluss Enter konfigurieren. Im Blatt **Scanner** Zelle **B3** auswählen und scannen. Grunddaten erscheinen darunter. Für einen bereits vorhandenen Code beim Erfassen die Zelle **Fremdcode** auswählen und scannen. Keine automatische Buchung oder neue Zeile entsteht dadurch. Doppelte Fremdcodes sind zulässig, aber die Suche zeigt sie als mehrdeutig an; die eigene Inventarnummer bleibt die Einzelkennung.
5. Standorte frei als Namen eintragen, z. B. „Fahrzeug 1“. Optional im Blatt **Standorte** sammeln, dann stehen sie als Auswahl bereit. Es werden keine Standorte im Programm automatisch erzeugt.
6. Für den Import im Blatt **Inventar** bleiben und **Speichern unter → CSV UTF-8 (durch Trennzeichen getrennt)** wählen. Nur dieses Blatt exportieren. Die ursprüngliche XLSX-Datei zusätzlich aufbewahren, weil CSV weder Etiketten noch Suchformeln enthält.
7. Als Geräteverantwortliche im Programm **Geräteakten → CSV-Erstinventarisierung** die CSV wählen und prüfen. Vorschau ansehen, nötige Standortzuordnungen wählen und danach die Vorschau erneut prüfen. Erst bei null Fehlern ausdrücklich **Geprüften Import bestätigen** drücken.

Alle Eingabespalten sind als Text formatiert, damit z. B. `000123` als Seriennummer erhalten bleibt. Standortnamen müssen eindeutig zu einem bestehenden Lagerort im berechtigten Bereich passen oder in der Vorschau ausdrücklich zugeordnet werden. Ein Roh-UUID als Standort ist ebenfalls zulässig.

## Was der Import übernimmt

| Spalte | Pflicht | Bedeutung |
| --- | --- | --- |
| Inventarnummer | Ja | Eigene eindeutige Einzelkennung; in der Excel-Vorlage schon vergeben |
| Bezeichnung | Ja | Verständlicher Name |
| Standort | Ja | Standortname, später einem Programm-Lagerort zuordenbar |
| Art | Nein | Gerät, Equipment oder ungeklärt |
| Zustand | Nein | ungeklärt, unauffällig, defekt oder Quarantäne |
| Hersteller / Modell / Seriennummer | Nein | Soweit bekannt, keine Werte raten |
| Fremdcode | Nein | Vorhandener Hersteller-/Verpackungscode als Zusatzinformation |
| Bemerkung | Nein | Kurze Beobachtung oder Beschreibung |

Fremdcodes werden als unveränderte Zusatzinformation gespeichert. Sie werden in diesem Schritt weder als Herstellerkennung ausgewertet noch automatisch anderen Geräten zugeordnet. Hersteller-Seriennummern allein sind ebenfalls keine globale Dublettenkennung. „Unauffällig“ ist eine Beobachtung und keine Freigabe. **Defekt und Quarantäne erzeugen eine eigenständige aktive Sperre**, die später im vorhandenen Freigabeablauf ausdrücklich bearbeitet werden muss. Equipment erhält keine automatisch angenommene Ausnahme von medizinprodukterechtlichen Pflichten.

Importierte Akten bleiben vorläufig: keine fingierten Inbetriebnahmen, Kontrollfristen, Einweisungen oder Qualifikationen. Fehlende Angaben bleiben leer bzw. ungeklärt. Die CSV enthält keine Patienten- oder Gesundheitsdaten.

## Wiederholungen und weitere Importe

CSV UTF-8 mit oder ohne BOM, Komma oder Semikolon wird unterstützt. Höchstens 1 MiB und 500 erfasste Gegenstände je Datei. Unbenutzte vorbereitete Zeilen mit ausschließlich Inventarnummer werden ignoriert. CSV-Kopfzeile beibehalten; minimale eigene Dateien dürfen nur Inventarnummer, Bezeichnung und Standort enthalten. Unbekannte oder doppelte Spalten, ungültige Zeilen und doppelte Inventarnummern werden gemeldet.

Gleiche zuvor importierte Daten werden übersprungen. Abweichende Angaben zu einer vorhandenen Nummer werden als Konflikt angezeigt; dieser Import aktualisiert **keine** bestehenden Akten. Neue Nummern in späteren Dateien werden neu übernommen. Ergänzung oder Korrektur bereits importierter Grunddaten benötigt einen eigenen späteren, versionierten Nachpflegeablauf; dieses Importmodul aktualisiert vorhandene Akten auch bei zuvor leeren Feldern nicht. Die Excel-Datei kann vor dem ersten Import weiter vervollständigt werden. Identische Nummern, die sich nur in Groß-/Kleinschreibung unterscheiden, gelten beim Import ebenfalls als Dublette. Es werden entweder alle gültigen neuen Zeilen zusammen übernommen oder keine; es gibt keinen stillen Teilimport. Historische Prüfungen, Sperren, Anhänge und Materialbuchungen bleiben bestehen.

Falls die Antwort verloren geht, **erneut senden**: derselbe Schlüssel und dieselben Daten wiederholen die ursprüngliche Antwort ohne doppelte Geräte. Vorher die Seite nicht neu laden. Bei einer eindeutig abgelehnten, veralteten Vorschau die Daten erneut prüfen.

## Neue Vorlage ohne laufenden Webdienst erzeugen

Die Entwickler-CLI kann eine weitere leere XLSX-Datei mit neuen Nummern erzeugen, ohne Datenbankzugriff:

```sh
bin/inventory device-template --out /tmp/Geraete-naechste-Erfassung.xlsx
```

Ein vorhandenes Ziel wird nicht überschrieben. Im Programm liefert `GET /api/v1/device-import/template.xlsx` bei jedem Download eine neue Datei; Geräteverantwortlichenrechte sind erforderlich. Die öffentliche Beispieldatei in diesem Repository hat keine echten Bestandsdaten. Die Erfassung und die endgültige Betriebsfreigabe sind getrennte Schritte.

## Technischer Ablauf und geprüfte Grenzen

`POST /api/v1/device-import/preview` erwartet `{ "csv": "...", "locations": { "CSV-Name": "Lagerort-UUID" } }` und liefert Zeilenergebnisse, Standortauswahl, Summen und `basis_hash`. Der Schreibschritt `POST /api/v1/device-import/commit` enthält dieselben Daten plus `basis_hash`. Beide Endpunkte verwenden Sitzung, Origin/CSRF und einen `Idempotency-Key`; die Vorschau verändert keine Daten. Nur Geräteverantwortliche dürfen ihre Lagerorte verwenden. Der Commit berechnet die Vorschau erneut in derselben Transaktion und lehnt Änderungen seit der Vorschau ab. Registrierung, Herkunftsdaten, gegebenenfalls Sperren, Audit/Outbox und Wiederholantwort werden atomar gespeichert (Schema 13).

XLSX-Struktur, geschützte Nummern, Textspalten und alle eingebetteten Barcodes werden überprüft; Barcodes wurden unabhängig auf ihre jeweiligen Nummern dekodiert. Ein physischer USB-Scanner und tatsächlicher Etikettendruck sind hardwareabhängig und müssen vor Ort geprüft werden. Die Excel-Suchformeln verwenden normale INDEX/MATCH/COUNTIF-Funktionen; die Desktop-Anwendung Excel wurde hier nicht ausgeführt.

Die Python-Datei `scripts/generate-intake-template.py` erzeugt nur das eingebettete leere Grundgerüst. Dafür wird XlsxWriter benötigt; Go-Build, Webdienst und CLI brauchen weder Python noch Office. Das Programm ersetzt Nummern und Barcodebilder pro neuer Vorlage und liefert eine normale `.xlsx`-Datei.

Verbrauchsmaterialien erhalten im nächsten Schritt eine eigene Tabelle und einen eigenen Import mit Mengen, Basiseinheiten, Chargen und Verfall.
