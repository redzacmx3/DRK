# Verbrauchsmaterial schnell erfassen

Die [Excel-Vorlage](vorlagen/Verbrauchsmaterial-Ersterfassung.xlsx) funktioniert vor dem Programmstart und später für weitere Erfassungen. Sie enthält 100 feste Erfassungsnummern, Barcode-Etiketten und eine Suche für USB-Handscanner. Keine Makros, zusätzliche Barcode-Schriftarten oder Internetdienste nötig.

## Vier Angaben ausfüllen

Im Blatt **Inventar** reichen Bezeichnung, Standort, Menge und Basiseinheit. Die Erfassungsnummer steht bereits fest. Eine Zeile erfasst einen Artikel einer Charge an einem Lagerort. Unterschiedliche Chargen oder Lagerorte bekommen getrennte Zeilen.

Beispiel: `Kompresse steril` · `Materiallager` · `20` · `Stück`. Menge immer in Basiseinheiten eingeben: zwei Packungen à zehn Stück ergeben **20 Stück**, nicht zwei Stück. Positive ganze Zahlen; für Nullbestände keine neue Zeile erfassen.

Charge, Verfall, Fremdcode und Bemerkung sind optional. Verfall als Text **JJJJ-MM-TT** eingeben. Unbekannte Werte leer lassen; keine erfundenen Chargen oder Datumsannahmen. Fehlende Charge, fehlender/abgelaufener Verfall oder eine bereits gesperrte Charge führt beim Import zu **Quarantäne**. Die Mengen erscheinen physisch im Bestand und werden nicht als nutzbar gewertet. Bei unbekannter Charge wird eine technische Kennung `UNGEKLAERT-…` mit dauerhaftem Herkunftsverweis angelegt; sie ist keine Herstellercharge.

## Codes scannen und Etiketten verwenden

Einen vorhandenen GTIN-/PZN-/Herstellercode in die Zelle **Fremdcode** scannen. Textformat erhält führende Nullen. Ein Fremdcode kann in mehreren Chargen vorkommen. Das Blatt **Scanner** sucht nach diesem Code oder einer Erfassungsnummer; B3 auswählen und mit dem USB-Handscanner eingeben. Mehrdeutige Codes werden angezeigt.

Die erzeugten Code-39-Etiketten enthalten **Erfassungsnummern dieser Liste**, keine Produkt-, Packungs- oder Chargenkennung. Optional auf dem Erfassungsblatt oder einem passenden Lagerbehälter verwenden. Herstellerkennzeichnungen nicht überkleben. Druck auf 100 % / tatsächliche Größe; zunächst ein Etikett testen.

Der CSV-Import hinterlegt Fremdcodes als Herkunftsinformation. Er erstellt **keine automatische GTIN-/PZN-/REF-Zuordnung oder Packungsumrechnung**. Dafür gibt es im Programm die ausdrückliche Barcode-Zuordnung mit geprüfter Packungsgröße. Ein Scan oder Excel-Suchergebnis bucht keine Menge.

## CSV übernehmen

1. XLSX unter eigenem Namen behalten. Nur das Blatt **Inventar** als **CSV UTF-8** exportieren; Semikolon oder Komma wird unterstützt.
2. Als Materialwart im Programm **Material-CSV: Erstbestand und Zugang** öffnen, CSV auswählen und Importart wählen.
3. **Erstbestand** nur für Artikel verwenden, deren Bestand am Zielort noch nicht erfasst wurde. Vorhandene Bestandszeilen, auch mit Menge null, schließen einen erneuten Erstbestand aus. Für eine Bestandszählung die Bestandskorrektur verwenden.
4. **Späteren Zugang** nur für tatsächliche neue Lieferungen verwenden. Neue Lieferungen benötigen neue, bisher unbenutzte Erfassungsnummern. Zugang wird zum vorhandenen Bestand addiert.
5. Vorschau prüfen, freie Standortnamen ausdrücklich vorhandenen Lagerorten zuordnen und erneut prüfen. Die Vorschau nennt Mengen, Einheiten, neue Artikel/Chargen und Quarantäne. Erst dann bestätigen.

Namen werden vorhandenen Artikeln eindeutig zugeordnet. Basiseinheiten müssen exakt passen; keine automatische Umrechnung. Für mehrdeutige Artikel die optionale **Artikel-ID** aus dem Materialkatalog ergänzen. Die Artikel-ID ist der technische `product_id`, nicht die Chargen-ID. Unbekannte Namen legen neue Artikel an, sichtbar in der Vorschau. Vorhandene Chargen müssen dasselbe Verfallsdatum haben; Stammdaten werden nicht überschrieben.

Eine fehlerhafte Zeile verhindert den gesamten Import. Änderungen an Beständen, Katalog, Chargen oder Berechtigungen seit der Vorschau erfordern eine neue Prüfung. Vorschau und Übernahme sind getrennt. Bei unklarer Antwort dieselben Daten erneut senden; bis zur Klärung die Seite nicht neu laden.

## Wiederholungen und weitere Erfassungen

Die Erfassungsnummer schützt dauerhaft vor Doppelbuchung. Gleiche Nummer mit gleichen Daten wird übersprungen, auch nach Entnahme oder Gegenbuchung. Geänderte Menge, Standort, Artikel oder Importart zur selben Nummer wird abgelehnt. Erfasste Zeilen und ihre Nummern nicht umschreiben; Fehler über dokumentierte Bestandskorrektur/Gegenbuchung im Programm bearbeiten.

Die GitHub-Datei ist **eine konkrete leere Erfassungsrunde**. Kopieren oder erneut herunterladen wiederholt dieselben Nummern. Für weitere Runden im Programm eine neue Vorlage herunterladen oder `bin/inventory stock-template --out NEUER-PFAD.xlsx` verwenden. Die CLI erstellt eine neue Datei ohne Datenbankzugriff und verweigert das Überschreiben vorhandener Dateien. Excel wird nach dem Import nicht mit späteren Programmbewegungen zurücksynchronisiert.

## API und geprüfte Grenzen

`POST /api/v1/stock-import/preview` erwartet `csv`, `locations` (optional) und `mode` (`initial` oder `receipt`). `POST /api/v1/stock-import/commit` erwartet dieselben Angaben plus `basis_hash` der Vorschau. Sitzung, exakter HTTPS-Origin, CSRF und Idempotenzschlüssel sind erforderlich. Materialwarte dürfen nur ihre zugewiesenen Lagerorte verwenden. Höchstens 1 MiB, 500 erfasste / 2.000 vorbereitete Zeilen. Artikel/Charge, Zugang, Journal/Audit/Outbox, unveränderliche Herkunftskennung und Wiederholantwort werden gemeinsam atomar gespeichert (Schema 14).

Go-/Browser-Tests prüfen Buchungen, Quarantäne, Wiederholungen nach Entnahme/Storno, veraltete Vorschauen, Rechte und vollständigen Rollback bei Journalfehlern. XLSX-Struktur und eingebettete Barcodes werden unabhängig geprüft. Excel Desktop, physischer USB-Handscanner und tatsächlicher Etikettendruck wurden hier nicht ausgeführt; diese bitte vor Ort testen.
