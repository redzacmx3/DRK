# DRK Warenbestand

Administrative Verwaltung von Medizinprodukten, Sanitätsmaterial und mobilen Packlisten. Fachliche Grundlage: [Architektur-Blueprint v2](docs/blueprint-v2.md). Seine rechtlichen Quellen wurden beim Umgebungsetup nicht erneut geprüft.

## Implementierter Entwicklungsstand

Modularer Go-Dienst mit eingebetteter HTML/JS-Oberfläche, SQLite und expliziten Migrationen. Synthetische Betreiber, Betriebsstätte und Lagerorte. HTTPS-Anmeldung, widerrufbare Sessions und Rechte je Betriebsstätte. Die Oberfläche zeigt Lagerorte und Materialbestand im zugewiesenen Bereich und bietet Wareneingang, Umlagerung, Entnahme, Gegenbuchung und Inventur. Das Bewegungsjournal zeigt frühere Einträge und deren Korrekturbezüge.

Noch offen: weitere Rollen/Aktionen, Geräteakte, vollständige PWA, Dokumentenspeicher, produktive Proxy-Konfiguration und vollständige Backup-/Restore-Verfahren. Nur synthetische Daten verwenden. Der Entwicklungsserver bindet ausschließlich an Loopback; Forwarded-Header werden nicht vertraut.

## Entwicklungsumgebung

Go 1.27.2 und GCC (CGO) werden benötigt. Gepinnte Abhängigkeiten einschließlich SQLite und Argon2id liegen in `vendor/`; Builds benötigen keinen Paketabruf.

```sh
source /workspace/drk-environment/env.sh # nur in dieser Codex-Umgebung
cd /workspace/DRK
go test ./...
go vet ./...
go build -mod=vendor -o bin/inventory ./cmd/inventory
mkdir -p var/tls
```

Ein lokales Entwicklungszertifikat erstellen (nur falls noch keines existiert):

```sh
openssl req -x509 -newkey rsa:2048 -noenc \
  -keyout var/tls/key.pem -out var/tls/cert.pem -days 30 \
  -subj /CN=localhost -addext subjectAltName=DNS:localhost,IP:127.0.0.1
chmod 600 var/tls/key.pem
```

Das Zertifikat im lokalen Entwicklungsbrowser bewusst als vertrauenswürdig einrichten; es ersetzt keine produktive PKI. Nach 30 Tagen erneuern. Private Schlüssel und Daten unter `var/` sind von Git und dem Image-Build ausgeschlossen. Vorhandene Zertifikate nicht blind überschreiben.

```sh
# Bei einer neuen Datenbank:
bin/inventory migrate --db var/inventory.db
bin/inventory seed-demo --db var/inventory.db # optional, wiederholbar

# Bei einer bestehenden Datenbank vor jeder Migration:
# bin/inventory backup --db var/inventory.db --out var/before-migration.db
# bin/inventory migrate --db var/inventory.db

bin/inventory serve --db var/inventory.db
# In einem zweiten Terminal:
bin/inventory healthcheck --ca var/tls/cert.pem
```

Den Dienst im lokalen Browser mit Hostnamen `localhost` und Port `8080` über HTTPS verwenden. Der exakte Browser-Ursprung muss `--origin` entsprechen (Standard: `https://localhost:8080`). Ein anderer Port verlangt einen passenden Origin-Parameter.

`backup` erstellt mit `VACUUM INTO` einen konsistenten SQLite-Snapshot, auch bei WAL. Ein neues Ziel wählen und seine Integrität prüfen. Das ist aktuell eine Datenbanksicherung; koordinierte Dokumentenarchive, verschlüsselte externe Sicherungen und produktive Restore-Abnahme sind noch offen. Niemals nur die laufende `.db`-Datei kopieren.

## Konten und Bereiche

Kein Standardkonto und kein Standardpasswort werden erzeugt. Konten werden ausschließlich lokal über die CLI angelegt. Passwörter (12–256 Bytes) kommen über stdin; niemals als Kommandozeilenargument oder im Repository speichern.

Beispiel für die synthetische Betriebsstätte in einer Bash-Sitzung:

```sh
read -r -s -p 'Neues Passwort: ' DRK_PASSWORD
printf '\n'
printf '%s\n' "$DRK_PASSWORD" | bin/inventory create-user \
  --db var/inventory.db --username viewer \
  --site 00000000-0000-4000-8000-000000000002
unset DRK_PASSWORD
```

```sh
# Viewer-Bereiche vollständig ersetzen (mehrere UUIDs mit Komma trennen):
bin/inventory set-sites --db var/inventory.db --username viewer --site SITE_UUID
# Alle Bereiche entziehen:
bin/inventory set-sites --db var/inventory.db --username viewer
# Konto sperren:
bin/inventory disable-user --db var/inventory.db --username viewer
```

Bereichsänderung und Kontosperre widerrufen vorhandene Sessions atomar. `--role` legt eine Rolle für die angegebenen Betriebsstätten fest; ohne Parameter ist es `viewer`. `set-sites` ersetzt sämtliche bisherigen Rollen/Bereiche dieses Kontos. Bestehende Viewer-Konten bleiben bei der Migration erhalten.

| Rolle | Lesen | Wareneingang/Umlagerung | Entnahme |
|---|---|---|---|
| `viewer` | Zugewiesene Betriebsstätten | Nein | Nein |
| `helper` | Zugewiesene Betriebsstätten | Nein | Ja |
| `material_manager` | Zugewiesene Betriebsstätten | Ja | Ja |

Inventur und Gegenbuchung sind ausschließlich für `material_manager` erlaubt.

```sh
bin/inventory set-sites --db var/inventory.db --username viewer \
  --site 00000000-0000-4000-8000-000000000002 --role material_manager
```

`create-user` unterstützt ebenfalls `--role`. Keine Web-Administration und keine automatische fachliche Qualifikation durch diese Rollen.

## Sessions und API

Passwörter: Argon2id mit zufälligem Salt, 64 MiB, drei Iterationen, Parallelität zwei. Session: 256-Bit-Zufallswert als `__Host-session` mit Secure, HttpOnly, Path=/ und SameSite=Lax; die Datenbank speichert nur den Hash. Laufzeit: acht Stunden absolut, 30 Minuten Inaktivität. Login rotiert eine bestehende Sitzung; Logout widerruft sie serverseitig.

| Endpoint | Verhalten |
|---|---|
| `GET /health/live` | Prozess erreichbar |
| `GET /health/ready` | Schema und Datenbankzugriff geprüft |
| `POST /api/v1/auth/login` | HTTPS und exakter Origin erforderlich; JSON mit username/password |
| `GET /api/v1/me` | Identität, Rollen/Bereiche und CSRF-Token für die aktuelle Sitzung |
| `POST /api/v1/auth/logout` | Origin und `X-CSRF-Token` erforderlich |
| `GET /api/v1/locations` | Nur zugewiesene Betriebsstätten |
| `GET /api/v1/locations/{id}` | Gleiche Objektprüfung; fremde IDs liefern 404 |
| `GET /api/v1/stock/catalog` | Gemeinsamer Artikel-/Chargenkatalog der Installation, maximal 500 Chargen |
| `GET /api/v1/stock/balances` | Standortbezogene Mengen, nutzbare Mengen und Versionen; maximal 500 Zeilen |
| `POST /api/v1/stock/receipts` | Wareneingang für Materialwarte |
| `POST /api/v1/stock/transfers` | Umlagerung; Materialwart-Rechte an Quelle und Ziel erforderlich |
| `POST /api/v1/stock/consumptions` | Entnahme für Helfer/Materialwarte |
| `POST /api/v1/stock/inventories` | Physische Zählung, Basisversion und Differenz |
| `POST /api/v1/stock/movements/{id}/reversal` | Vollständige Gegenbuchung mit Originalbezug |
| `GET /api/v1/stock/movements` | Bereichsbezogenes Journal; limit 1–100, offset 0–100000, X-Has-More |

Anmeldung ist auf zehn Versuche pro Minute je direkter Client-IP und zwei gleichzeitige Passwortprüfungen begrenzt. Der Limiter ist pro Prozess und wird beim Neustart zurückgesetzt; verteilter Missbrauchsschutz und produktive Proxy-Auswertung sind noch offen. Keine dauerhafte Browserablage von Sitzungs- oder CSRF-Token durch die Oberfläche.

## Materialfluss

`seed-demo` ergänzt einen synthetischen Artikel und eine Charge; es erzeugt oder erhöht **keine Bestände**. Weitere Stammdaten lassen sich zunächst lokal anlegen:

```sh
bin/inventory create-product --db var/inventory.db --name 'Demo-Verband' --unit 'Stück'
# Ausgegebene Produkt-UUID einsetzen:
bin/inventory create-batch --db var/inventory.db --product PRODUCT_UUID \
  --lot DEMO-CHARGE --expires-on 2028-12-31
```

Verfallsdaten werden derzeit als exakter Tag oder als unbekannt erfasst. Ohne `--expires-on` bleibt das Datum ungeklärt. Packungsumrechnung, Hersteller-Barcodes und Scanner sind noch nicht implementiert; Mengen werden ganzzahlig in der ausdrücklich festgelegten Basiseinheit gebucht.

Jede Stock-POST-Operation verlangt Session, exakten Origin, `X-CSRF-Token` und einen `Idempotency-Key` (16–100 ASCII-Buchstaben/Ziffern/Unterstriche/Bindestriche). Beispielkörper:

```json
{"batch_id":"BATCH_UUID","to_location_id":"LOCATION_UUID","quantity":10}
```

Für Umlagerung zusätzlich `from_location_id` und die aktuelle `expected_version` des Quellbestands angeben. Für Entnahme nur Quelle, Charge, Menge und Quellversion; kein Ziel. `state` ist optional, Standard `available`; Umlagerungen können auch `quarantined` oder `damaged` bewegen. Mengen pro Buchung: 1 bis 1.000.000.000 Basiseinheiten, Notiz maximal 500 Bytes.

Bestand, Bewegungszeilen, Akteur-/Zeit-Audit, Outbox-Ereignis und gespeicherte Antwort entstehen in derselben IMMEDIATE-Transaktion. Identische Wiederholung liefert die ursprüngliche Antwort ohne zweite Buchung; gleicher Schlüssel mit anderem Inhalt liefert 409. Fehlende Menge oder veraltete Quellversion liefert ebenfalls 409. Buchungsrechte und gültige Sitzung werden innerhalb der Transaktion erneut geprüft. Source und Ziel werden bei Fehlern vollständig zurückgerollt.

Physische Menge und nutzbare Menge sind getrennt: gesperrte, abgelaufene, beschädigte, quarantänisierte oder datumsungeklärte Ware ist nicht entnehmbar. Verfall wird bei Lesen/Buchen nach Europe/Berlin berechnet, ohne Nachtjob. Ein bekannter Grenztag bleibt bis zu dessen Ende gültig. Nicht nutzbarer Wareneingang wird bei gewünschtem Zustand `available` in Quarantäne gebucht. Eine Umlagerung verändert den physischen Ort und hebt keine Sperre auf.

Die Oberfläche behält bei einer unbestätigten Antwort dieselbe Nutzlast und denselben Schlüssel für Wiederholungen im Arbeitsspeicher. Bis zur Klärung nicht neu laden; es gibt noch keinen dauerhaften Offline-Entwurf. Nach bestätigter Buchung werden aktuelle Mengen/Versionen neu geladen. Lesende Antworten enthalten einen UTC-Datenstand; die Anzeige verwendet Europe/Berlin. Die Bestandsanzeige ist auf 500 Zeilen begrenzt.

Journal- und Audit-Einträge sind über Anwendung und SQLite-Trigger unveränderlich. Korrekturen erfolgen durch neue Einträge; keine historischen Einträge manuell ändern. Outbox-Ereignisse werden gespeichert, aber noch nicht von einem Benachrichtigungsworker zugestellt. Artikel/Chargen sind gemeinsame Stammdaten der einzigen Betreiberinstallation; Standortbestände und Buchungen sind bereichsbezogen geschützt.

## Gegenbuchung und Inventur

Eine Gegenbuchung kehrt alle Bewegungszeilen eines Originals einmalig um. Grund und aktuelle Versionen **aller** betroffenen Bestände sind erforderlich. Sie kann scheitern, wenn die ursprünglich eingegangene Ware zwischenzeitlich entnommen/umgelagert wurde. Gegenbuchungen selbst sowie Zählungen ohne Differenz werden nicht erneut gegengebucht; eine erneute fachliche Korrektur erhält einen neuen Inventurnachweis. Auch gesperrte Mengen können administrativ korrigiert werden; dabei werden keine Sperren aufgehoben.

```json
{"expected_versions":[{"location_id":"LOCATION_UUID","state":"available","version":3}],"note":"Doppelten Wareneingang korrigieren"}
```

In der Oberfläche zuerst Inventur wählen, Charge/Ort/Zustand festlegen und **Zählung beginnen**. Der Basisstand wird dann eingefroren; ein Neuladen ändert diese Version nicht. Gezählt wird ausdrücklich die physische Menge in diesem Zustand, einschließlich Null. Danach Grund und Zählmenge bestätigen. Hat sich der Bestand zwischenzeitlich geändert, wird die Buchung mit 409 abgelehnt und eine neue Zählung verlangt.

```json
{"batch_id":"BATCH_UUID","location_id":"LOCATION_UUID","state":"available","expected_version":3,"counted_quantity":7,"note":"Physisch sieben Stück gezählt"}
```

Für einen noch nicht vorhandenen Bestand ist die Basisversion 0. Jeder Zählnachweis speichert Basisversion, vorherige Menge, Zählmenge, Akteur und Zeit; die Differenz wird atomar gebucht. Auch ohne Differenz entstehen Nachweis, Audit, Outbox und Idempotenzantwort, aber keine künstliche Mengenbewegung. Die Bestandsversion erhöht sich zur Abgrenzung der Zählung. Positive ungeklärte/gesperrte Mehrmengen müssen in Quarantäne erfasst werden; Quarantäne oder Beschädigung werden durch Zählungen nicht aufgehoben.

Das Journal bleibt unverändert. `reversed_by` wird aus ergänzenden Gegenbuchungen berechnet. Journal und Gegenbuchung prüfen alle betroffenen Betriebsstätten; ein Transfer in einen fremden Bereich wird im Journal nur bei Leserechten für beide Bereiche angezeigt. Die Oberfläche zeigt 50 Einträge je Seite und erlaubt das Blättern.

Migration 4 erweitert das Bewegungsjournal durch den dokumentierten SQLite-Tabellenumbau. Dabei wird die Fremdschlüsselprüfung auf einer eigenen Migrationsverbindung kurzzeitig deaktiviert, vor Commit geprüft und vor Rückgabe wieder aktiviert. IDs, bestehende Bewegungszeilen, Audit, Outbox und Idempotenzantworten bleiben erhalten. Vorher wie oben einen konsistenten Snapshot erstellen und laufende Dienste für das Upgrade stoppen.

Optionaler Browser-Regressionscheck (Node.js 22+ und Chromium, keine npm-Abhängigkeiten):

```sh
node scripts/check-inventory-ui.mjs
# Abweichendes Chromium über CHROMIUM_BIN angeben.
```

Dieser Check prüft die Oberfläche mit simulierter API: ausdrücklichen Zähl-/Prüfstart, eingefrorene Inventur-/Packlisten-/Prüfbasis trotz Neuladen, erneuten Start nach Konflikt, Nullzählung, identische Wiederholung nach verlorener Prüf-/Kennungsantwort und explizite Scannerübernahme mit Packungsumrechnung ohne automatische Buchung. Kamera-Hardware wird durch diesen simulierten Test nicht verifiziert. Die Go-Tests prüfen die echte SQLite-Transaktion, Rechte, Migration und konkurrierende Korrekturen. Der Browsercheck ersetzt keinen HTTPS-/Compose-Funktionstest.

## Material-Sollpacklisten und physische Prüfung

Materialwarte veröffentlichen pro Lagerort unveränderliche Sollversionen; Helfer und Materialwarte dokumentieren physische Prüfungen, Viewer lesen. Eine neue Version ersetzt die bisherige Sollversion, erhält aber frühere Inhalte und Nachweise. Konkrete Geräte und Zubehörmengen können inzwischen als digitale Sollanforderungen erfasst werden. Physische Geräte-/Funktionsprüfungen, genehmigte Artikelalternativen und vollständige Einsatzfreigabe folgen separat; die aktuelle Bewertung ist ausdrücklich `material_only`, `overall_readiness` bleibt `not_assessed`.

In der Oberfläche Lagerort wählen, **Sollversion bearbeiten**, Sollpositionen mit Menge in Basiseinheiten und Mindestrestlaufzeit in Tagen hinzufügen und mit Begründung speichern. Die Basisversion bleibt während der Bearbeitung fest. Je Lagerort gilt genau die höchste veröffentlichte Version. Mengen: 1–1.000.000.000, Restlaufzeit: 0–3650 Tage, 1–100 eindeutige Artikelpositionen, Prüfgültigkeit: ausdrücklich 1–168 Stunden (Oberflächenvorschlag 24). Aktuell wählt die Oberfläche Artikel aus dem Chargenkatalog; Artikel-/Chargenanlage erfolgt weiterhin per CLI.

| Endpunkt | Funktion |
|---|---|
| `GET /api/v1/packlists` | Aktuelle Sollversionen im eigenen Bereich |
| `GET /api/v1/packlists?history=true` | Auch frühere Sollversionen, höchstens 100 Einträge |
| `POST /api/v1/packlists` | Neue Sollversion veröffentlichen, nur Materialwart |
| `GET /api/v1/packlists/{id}` | Unveränderliche Sollversion mit Positionen |
| `GET /api/v1/packlists/{id}/readiness` | Aktuelle Materialbewertung mit Gründen, Datenbasis und letztem physischem Check |
| `POST /api/v1/packlists/{id}/checks` | Physisches Ergebnis mit Begründung und eingefrorener Datenbasis dokumentieren |
| `GET /api/v1/packlists/{id}/checks` | Letzte 50 Prüfnachweise samt unverändert gespeichertem Datenstand |

Die begrenzten Listen melden weitere Einträge über `X-Has-More: true`; Blättern dieser Packlisten-/Prüfnachweislisten ist noch nicht implementiert. Die Oberfläche nennt die Grenze. Direkte Objektzugriffe bleiben bereichsbezogen geschützt.

Beispiel für eine Veröffentlichung (JSON, mit Session, CSRF, Origin und `Idempotency-Key`):

```json
{"location_id":"LOCATION_UUID","expected_version":0,"title":"Sanitätskoffer – Material","check_valid_hours":24,"note":"Sollstand festgelegt","items":[{"product_id":"PRODUCT_UUID","quantity":5,"min_remaining_days":30}]}
```

`expected_version` ist 0 bei der ersten Veröffentlichung, danach die bisher höchste Version; konkurrierende Bearbeitungen liefern 409. Dieselbe Benutzer-/Schlüsselkombination darf auch nicht für Materialbuchungen oder einen anderen Packlistenvorgang wiederverwendet werden. Version/Nachweis, Akteur-/Zeit-Ereignis, eigene Packlisten-Outbox und gespeicherte Idempotenzantwort werden gemeinsam in einer kurzen IMMEDIATE-Transaktion geschrieben. Migration 5 ergänzt diese Tabellen, ohne bestehende Bestandsdaten zu ändern; vor Upgrade Snapshot und Dienststopp wie oben.

Nutzbar zählt nur Ware im Zustand `available`, ohne globale Sperre und mit bekanntem, gültigem Verfall mindestens am Berliner Tagesdatum plus Mindestrestlaufzeit. Die Grenze ist einschließlich: 30 Tage am 10.10.2026 erfordern mindestens 09.11.2026. Beschädigte, quarantänisierte, gesperrte, abgelaufene oder zu kurz haltbare Mengen zählen physisch, erfüllen aber keinen Sollbestand. Unbekannte kritische Verfallsdaten verhindern eine positive Materialbewertung, auch wenn andere Chargen rechnerisch die Sollmenge erreichen. Die Antwort nennt physische, nutzbare, ausgeschlossene und ungeklärte Mengen sowie Fehlmengen; es erfolgt keine Reservierung oder automatische Entnahme.

Ausreichender Bestand ergibt zunächst `needs_check`. Vor einer physischen Prüfung ausdrücklich **Physische Prüfung beginnen**; dieser Schritt friert den Datenstand ein. Die Bedienperson prüft tatsächlich Inhalt, Mengen und Verpackungsintegrität und wählt ein positives oder negatives Ergebnis mit Notiz. Ein negatives Ergebnis ändert keine Bestände und hebt keine Sperren auf. Für festgestellte Differenzen die Inventur beziehungsweise den vorgesehenen Sperrworkflow verwenden; eine positive physische Bestätigung überstimmt niemals Fehlmengen oder ungeklärte Materialdaten.

```json
{"basis_hash":"HASH_AUS_READINESS","physical_passed":true,"note":"Inhalt, Mengen und Verpackungsintegrität physisch geprüft"}
```

`ready` gilt ausschließlich für die Materialbewertung und verlangt erfüllte Sollpositionen sowie einen aktuellen positiven physischen Nachweis. Datenbasis enthält Sollversion, Berliner Kalendertag und sämtliche relevanten Chargen-/Bestandsdaten einschließlich Versionsnummer, Zustand, Sperre und Verfall. Eine Bestandsbuchung (auch später gegengebucht), Änderung relevanter Sperr-/Verfallsdaten, neue Sollversion, Tageswechsel oder Ablauf der Prüfgültigkeit verlangt erneute Prüfung. Die Gültigkeitsgrenze ist exklusiv. Der Server berechnet dies bei jedem Lesen; es ist kein Nachtjob erforderlich. Eine beim Speichern veränderte Grundlage liefert 409, ohne Nachweis zu schreiben. Unveränderlich gespeicherte frühere Datenstände ermöglichen die Nachvollziehbarkeit. Bei einer verlorenen Antwort exakt denselben Request samt Schlüssel erneut senden; die gespeicherte Antwort ist historisch, für die aktuelle Bewertung anschließend neu lesen.

## Scanner, Verpackungskennungen und QR-Etiketten

Handscanner mit Tastatureingabe beziehungsweise Code zum Einfügen werden unterstützt. Enter löst den Code auf, ohne Material zu buchen. Optional nutzt **Mit Kamera scannen** die native Browser-API `BarcodeDetector` und die ausdrücklich gestartete Kamera. Dafür sind HTTPS, Kameraberechtigung und eine unterstützte Browserimplementierung erforderlich. Unterstützte Kameratypen werden zur Laufzeit geprüft; ohne Unterstützung bleibt die Tastatur-/Texteingabe verfügbar. Eine physische Kamera beziehungsweise ein konkretes Scannergerät ist noch nicht als Hardwareintegration abgenommen. Kamerabilder werden lokal ausgewertet, nicht zum Server hochgeladen; gesendet wird nur der erkannte begrenzte Text. Abmelden, Verlassen/Verbergen der Seite oder Kamera beenden stoppt die Aufnahme.

| Endpunkt | Funktion |
|---|---|
| `POST /api/v1/scanner/resolve` | Text auflösen, keine Buchung oder Stammdatenanlage |
| `GET /api/v1/product-identifiers` | Aktive und widerrufene Verpackungskennungen, maximal 500; optional `product_id` |
| `POST /api/v1/product-identifiers` | GTIN/PZN/REF mit expliziter Packungsmenge zuordnen, nur Materialwart |
| `POST /api/v1/product-identifiers/{id}/revoke` | Falsche Zuordnung mit Begründung widerrufen, nur Materialwart |
| `GET /api/v1/labels/{kind}/{id}` | Geschütztes druckbares QR-PNG für `location`, `product` oder `batch` |

Scanner-Auflösung benötigt Session, Origin und CSRF, aber keinen Idempotenzschlüssel, da sie keine Änderung schreibt. Texte: 1–1024 UTF-8-Bytes; JSON-Body maximal 4096 Bytes. `format` kann `auto`, `gtin`, `pzn`, `gs1`, `ref` oder `internal` sein. Unbekannte fremde URLs bleiben Daten; es erfolgt keine Navigation, externe Produktabfrage oder automatische Stammdatenanlage.

```json
{"text":"(01)04006381333931(10)DEMO-2026(17)281231","format":"auto"}
```

EAN-8, UPC-A, EAN-13 und GTIN-14 werden mit Modulo-10-Prüfziffer validiert und als 14-stellige GTIN mit führenden Nullen gespeichert. Unterschiedliche echte Verpackungs-GTINs werden nicht zusammengelegt. UPC-E-Kompression wird nicht expandiert; Scanner dafür auf Ausgabe der vollständigen UPC-A konfigurieren. PZN-7/PZN-8 wird mit der PZN-Prüfziffer validiert, PZN-7 auf acht Ziffern aufgefüllt. `PZN:` und der führende Bindestrich eines PZN-Code-39 werden erkannt. Eine achtstellige Ziffernfolge kann beide Prüfziffernregeln erfüllen: dann ausdrücklich GTIN oder PZN wählen, auch wenn nur eine Zuordnung im Katalog existiert. Hersteller-REF wird nur bei ausdrücklicher Formatwahl verwendet und bleibt hinsichtlich Groß-/Kleinschreibung exakt.

GS1 unterstützt AI `01` (GTIN), `10` (Charge), `17` (Verfall) und `21` (Seriennummer), mit ASCII-Gruppentrenner 29/FNC1-Ausgabe und Klammerdarstellung `(01)...(10)...`. Variable Felder brauchen den Trenner, wenn weitere Felder folgen; es werden keine Verfallsfelder aus dem Chargentext geraten. Bei verdächtigem ungetrenntem Verfallssuffix wird Klärung verlangt. AIM-Präfixe für GS1 (`]d2`, `]C1`, `]Q3`), EAN (`]E0`) und gebräuchliche nicht-GS1-Symbologien werden berücksichtigt. Unbekannte oder doppelte AIs und ungültige Felder liefern keine Teilauflösung. Der GS1-Text braucht eine GTIN; weitere AIs/UDI-Datenstrukturen folgen später.

GS1 `YYMMDD` bleibt als Originalangabe erhalten. Ein zweistelliges Jahr erhält kein geratenes Jahrhundert: der Chargenabgleich vergleicht gegen den ausdrücklich hinterlegten vollständigen Grenztag. Tag `00` wird als Monatspräzision erkannt, aber ohne bestätigte Hersteller-/Produktregel nicht in einen nutzbaren Grenztag umgerechnet. Seriennummern werden angezeigt; individuelle Geräte-/Serienverfolgung ist noch offen, deshalb keine direkte Buchungsübernahme für solche Scans. Widerspruch zwischen gescanntem und hinterlegtem Verfall verhindert ebenfalls die Übernahme. Ein Scan hebt weder Sperren noch Verfall auf.

Unbekannte validierte Verpackungskennung in der Oberfläche auflösen; Materialwart wählt ausdrücklich Artikel, geprüfte Basiseinheiten je Packung und Begründung. Artikel müssen derzeit zuvor mit Charge per CLI angelegt sein, damit sie im Oberflächenkatalog erscheinen. Beispiel-Zuordnung:

```json
{"product_id":"PRODUCT_UUID","kind":"gtin","value":"4006381333931","pack_quantity":10,"note":"Packungsinhalt zehn Stück am Material geprüft"}
```

`pack_quantity` ist ausdrücklich 1–1.000.000.000; sie wird weder aus Barcode noch Artikelname angenommen. Die Zuordnung gehört zum gemeinsamen Artikelkatalog dieser einzigen Betreiberinstallation. Ein aktiver Code gehört eindeutig zu einem Artikel und einer Packungsumrechnung. Fehlerhafte Bindung zuerst widerrufen und dann mit neuer ID korrekt zuordnen. Ursprüngliche Bindung und Ereignissnapshots bleiben erhalten; reguläres Update/Delete ist ausgeschlossen, nur einmaliger aktiver→widerrufener Übergang erlaubt. Zuordnung/Widerruf, Akteur-/Zeit-Ereignis, eigene Katalog-Outbox und exakte Idempotenzantwort entstehen atomar. Alle schreibenden Module teilen den Benutzer-/Schlüsselraum. Migration 6 ergänzt Tabellen und Indizes, bestehende Mengen und Packlisten bleiben unverändert.

EAN/PZN allein bestimmt keine Charge. Vor Übernahme Artikel, Charge, Verfall und Packungsumrechnung am tatsächlichen Material bestätigen, Buchungsaktion und Quell-/Zielort ausdrücklich wählen. Packungsanzahl wird anhand der registrierten Umrechnung in Basiseinheiten vorbereitet, mit Überlauf-/Mengengrenze. Das Buchungsformular muss danach separat bestätigt werden; seine bisherigen Versions-, Bereichs- und Sperrprüfungen bleiben maßgeblich. Bei einer unveränderten Scanner-Vorauswahl werden `identifier_id` und `pack_count` mitgesendet. Der Server prüft in der Buchungstransaktion die noch aktive Kennung, deren Artikel und `quantity = pack_count × pack_quantity`; widerrufene/fremde Kennungen liefern 409, falsche/zu große Umrechnungen 400. Der verwendete Verpackungsnachweis wird unveränderlich mit der Bewegung gespeichert und im bereichsbezogenen Journal mit Kennung, Packungsanzahl und ursprünglichem Faktor angezeigt. Werden Artikel/Quelle/Aktion oder Basismenge im Buchungsformular bewusst geändert, nennt die Oberfläche den Wechsel zur manuellen Basismenge. Bereits bestätigte Buchungen lassen sich trotz späterem Kennungswiderruf mit ihrem ursprünglichen Schlüssel wiederholen. Interne Artikel-/Chargen-QRs enthalten keine Packungsgröße; die Basismenge ist dann manuell anzugeben. Ein unbekannter Code oder eine unbekannte Charge erzeugt keine automatische Anlage.

Interne QR-Etiketten enthalten nur `drk:v1:location:UUID`, `drk:v1:product:UUID` beziehungsweise `drk:v1:batch:UUID`; Namen, Kennwörter und Rechte sind nicht kodiert. Lagerortauflösung und Etikettenzugriff prüfen den Standortbereich; gemeinsame Artikel-/Chargenstammdaten benötigen mindestens eine der bestehenden Leserollen. QR-Bilder werden lokal serverseitig mit der eingecheckten, versionierten Go-Bibliothek erzeugt, ohne externen QR-Dienst. In **Interne QR-Etiketten** Objekt wählen, anzeigen und drucken oder Kennung kopieren. Ein Lagerortscan kann dessen Materialbewertung öffnen und verändert keinen Bestand.

## Docker Compose (Linux-Entwicklung)

Zuerst das Entwicklungszertifikat wie oben unter `var/tls/` erstellen. Danach:

```sh
docker compose build
# Bei bestehendem Volume vor der Migration:
# docker compose run --rm inventory backup --db /data/inventory.db --out /data/before-migration.db
docker compose run --rm inventory migrate --db /data/inventory.db
docker compose run --rm inventory seed-demo --db /data/inventory.db
docker compose up -d --wait --wait-timeout 45
docker compose exec -T inventory /app/inventory healthcheck --ca /certs/cert.pem
```

Konto im Container anlegen (Passwort wie oben verdeckt einlesen):

```sh
printf '%s\n' "$DRK_PASSWORD" | docker compose run --rm -T inventory create-user \
  --db /data/inventory.db --username viewer \
  --site 00000000-0000-4000-8000-000000000002
unset DRK_PASSWORD
```

Das Image läuft als UID/GID 1000, SQLite liegt im persistenten Volume `inventory_data`. Entwicklungszertifikate werden schreibgeschützt eingebunden und müssen für UID 1000 lesbar sein. Linux-Hostnetzwerk hält den Dienst auf Host-Loopback; dies ist kein produktives Proxy-Deployment. Für Docker Desktop muss Hostnetzwerk ausdrücklich unterstützt/aktiviert sein. `docker compose down` erhält Daten; `down -v` löscht das Volume.

In dieser Cloud bei einem schreibgeschützten Standard-Docker-Verzeichnis `DOCKER_CONFIG=/tmp/drk-docker-config` setzen. Installations-/Startanweisungen sind im Codex-Konfigurationsentwurf gespeichert; Veröffentlichung der Umgebung und GitHub-Push sind getrennte Vorgänge.

## Nächste Umsetzung

Als Nächstes Geräteakten und Fristen ergänzen; vollständige Geräte-/Zubehörbewertung und Packlistenalternativen bleiben offen. Vor Pilotfreigabe die [Blueprint-Abnahmekriterien](docs/blueprint-v2.md) erfüllen.

### Grundlage der Gerätefristen

`internal/app/deadlines.go` berechnet einzelne Kontrollfristen anhand ausdrücklich bestätigter, versionierter Regeln mit Nachweisreferenz. Unterstützt werden Monatsende nach Intervall, Jahresende nach ganzjährigem Intervall und exakte Termine. STK-Regeln verwenden Monatsende und höchstens 24 Monate; kürzere oder anlassbezogene Termine können mit eigener Nachweisreferenz eingetragen werden, verlängern aber niemals die berechnete Grenze. Produktbezogene Anwendbarkeit und Ausnahmen werden nicht aus Gerätenamen abgeleitet.

Die Zustände sind `UNKNOWN`, `NOT_APPLICABLE`, `CURRENT`, `DUE_SOON` und `OVERDUE`. Eine Frist gilt bis einschließlich des fachlichen Tages in `Europe/Berlin`; die Vorwarnschwelle ist ein expliziter Parameter. Fehlendes Basisdatum bleibt ungeklärt. Die Funktion beurteilt keine Gerätesperren und erteilt keine Einsatzfreigabe.

Die Berechnungsgrundlage ist getestet. Registrierte Geräteakten sind inzwischen vorhanden; versionierte Kontrollregeln und Inbetriebnahme sind integriert. Die Anbindung weiterer Prüfungen folgt. Datenbankschema: Version 11.


### Registrierte Geräteakten

Geräteverantwortliche (`device_manager`) können über `POST /api/v1/devices` Geräte im zugewiesenen Bereich registrieren. Pflichtfelder: `location_id`, `inventory_number`, `name`, `manufacturer`, `model`, `serial_or_lot`, `note`. Mutationen benötigen gültige Session, exakten HTTPS-Origin, CSRF und einen Idempotenzschlüssel. Inventarnummern sind im derzeit gemeinsamen Betreiberbestand eindeutig; Serien-/Losnummern dürfen mehrfach vorkommen. Eine Rollenänderung über `set-sites` widerruft Sessions; die CLI ersetzt weiterhin sämtliche Zuweisungen dieses Kontos.

`GET /api/v1/devices` liefert bis zu 500 bereichsbezogene Geräte (`X-Has-More` bei weiteren Einträgen); `GET /api/v1/devices/{id}/book` liefert die aktuelle Geräteakte einschließlich berechneter Fristen. Viewer, Helfer, Materialwarte und Geräteverantwortliche lesen innerhalb ihrer Bereiche. Die Oberfläche zeigt eine Geräteübersicht und erfasst Inbetriebnahme-/Kontrollregeln; die erstmalige Geräteanlage erfolgt derzeit über die API. Registrierung, Actor, Snapshot, Outbox und Wiederholantwort werden in einer Transaktion gespeichert. Der Idempotenzschlüsselraum wird mit den anderen Modulen geteilt.

Eine neue Registrierung ist keine Inbetriebnahme: `lifecycle=NOT_COMMISSIONED`, `readiness=not_assessed`, STK/MTK/elektrische Kontrolle jeweils `UNKNOWN`. Einweisungen und begrenzte Dokumentanhänge sind umgesetzt; Dokumentprüfung/Freigabe, Korrekturen und vollständige Medizinproduktebuchführung bleiben offen. Wiederkehrende Prüfungen und einzelne Sperren/Freigaben sind unten beschrieben. Die Rolle allein bestätigt keine Qualifikation. Geräte bleiben aus der bisherigen Material-Sollbewertung ausgeschlossen.


### Inbetriebnahme und versionierte Kontrollregeln

Schema 8 ergänzt `POST /api/v1/devices/{id}/commission` und `POST /api/v1/devices/{id}/rules`. Beide benötigen `expected_version` aus der aktuellen Geräteakte sowie `evidence`, `qualification`, `assignment` und `note` als ausdrücklich eingetragene Referenzen. Die Geräteverantwortlichenrolle allein genügt nicht als Qualifikationsnachweis; eine unabhängige Dokumentprüfung ist noch nicht implementiert. `commission` benötigt außerdem `performed_on` (striktes Tagesdatum, nicht zukünftig); eine zweite Inbetriebnahme wird abgelehnt. Danach lautet der Lebenszyklus `IN_SERVICE`, während die vollständige Einsatzbereitschaft weiterhin `not_assessed` bleibt.

`rules` benötigt genau drei Einträge in `rules` für `STK`, `MTK`, `electrical`. Jeder enthält `applicability` (`required`, `exempt`, `unknown`) und einen eigenen `evidence`-Verweis. Bei erforderlicher Kontrolle: `mode` und `interval_months` beziehungsweise `exact_date`. STK verwendet Monatsende mit höchstens 24 Monaten, MTK Jahresende mit ganzjährigem Intervall. Kürzere Termine sind als `earlier_date` mit `earlier_evidence` zulässig. Ausnahmen/ungeklärte Kontrollpflichten enthalten keine Frist. `basis_date` und `version` werden ausschließlich serverseitig aus Inbetriebnahme und Geräteversion gesetzt; bestandene spätere Prüfungen bilden die neue Basis; eine Regelrevision erhält diese Basis.

Der Server speichert Regelstand, Basis und berechnetes Ergebnis unveränderlich im Ereignis. `GET /api/v1/devices/{id}/events` liefert die letzten 500 Kontrollereignisse mit Actor, Zeit, Referenzen und ursprünglichem Berechnungsergebnis (`X-Has-More` bei weiteren Ereignissen). Die aktuelle Geräteakte berechnet zeitabhängige Zustände bei jedem Lesen neu, ohne Nachtjob. Alte Regelstände und Wiederholantworten bleiben erhalten. Änderungen mit veralteter Geräteversion liefern 409; Ereignis, Outbox und Idempotenzantwort entstehen atomar.

Die Oberfläche verlangt vor der Bearbeitung das Laden der Geräteakte und hält diese Basis fest. Unbestätigte Antworten werden mit identischem Inhalt/Schlüssel wiederholt, auch nach erneuter Anmeldung mit demselben Konto. Diese Entwürfe liegen nur im Arbeitsspeicher; kein Offline- oder Wiederanlaufversprechen nach Schließen der Seite. Eine vollständige Einsatzfreigabe bleibt offen. Einzelne Sperren können inzwischen ausdrücklich geschlossen werden; automatische Ausnahmen aus Gerätenamen und Benachrichtigungszustellung gibt es weiterhin nicht.


### Wiederkehrende Prüfungen und unabhängige Sperren

Schema 9 erweitert die unveränderlichen Kontrollereignisse. Alle folgenden Mutationen verwenden dieselben Session-/Origin-/CSRF-/Idempotenz- und Geräteversionsprüfungen. Die Oberfläche unterstützt Prüfungen, Sperrmeldung und das ausdrückliche Schließen einer geladenen Sperre.

| Endpoint | Pflichtangaben zusätzlich zu `expected_version`, `evidence`, `note` |
| --- | --- |
| `POST /api/v1/devices/{id}/inspections` | `qualification`, `assignment`, `control_type`, `performed_on`, `result` (`passed`/`failed`), `examiner`, `method` |
| `POST /api/v1/devices/{id}/defects` | `block_reason` (`defect`/`recall`/`quarantine`) |
| `POST /api/v1/devices/{id}/releases` | `qualification`, `assignment`, `block_id` einer aktiven Sperre |

Prüfungen und Freigaben benötigen `device_manager` im Gerätebereich. Helfer dürfen ausschließlich `defect` melden; Rückruf/Quarantäne bleiben Geräteverantwortlichen vorbehalten. `method` kann die Methode/Messwerte oder eine konkrete Protokollreferenz dokumentieren; `examiner` benennt Prüfer/Firma. Referenzen werden erfasst, eine unabhängige Prüfung von Qualifikation oder Dokumentinhalt bleibt offen.

Prüfungen benötigen eine ausdrücklich erforderliche Kontrollregel. Ihr Tagesdatum darf weder zukünftig noch älter als die aktuelle Fristbasis oder der jüngste Prüfnachweis dieser Kontrollart sein. Bestandene Prüfungen setzen die neue Basis nur für ihre Kontrollart. Monats-/Jahresfristen werden serverseitig berechnet; bei einer exakten Terminregel ist `next_due_on` ausdrücklich anhand des Nachweises einzutragen. Das ursprüngliche Berechnungsergebnis wird gespeichert, aktuelle Zustände werden beim Lesen neu berechnet. Eine bestehende kürzere Anlassfrist bleibt erhalten, bis sie mit eigener Regelversion neu bewertet wird.

Ein fehlgeschlagenes Prüfergebnis verändert die Fristbasis nicht und erzeugt eine separate `inspection_failed`-Sperre. Defekt/Rückruf/Quarantäne und jeder einzelne Prüfversagensnachweis bleiben als getrennte Einträge in `active_blocks` sichtbar. Eine bestandene Prüfung, eine Regeländerung oder eine Ausnahme schließt keine Sperre. `releases` schließt ausschließlich die benannte Sperre mit eigenen Nachweisen; eine Prüfversagenssperre setzt zusätzlich einen später gespeicherten bestandenen und noch fristgültigen Nachweis derselben Kontrollart voraus. Frühere Nachweise und historische Wiederholantworten bleiben erhalten. Das Schließen aller Sperren erteilt weiterhin keine vollständige Einsatzfreigabe (`readiness=not_assessed`).


### Einweisungen und geschützte Dokumentanhänge

Schema 10 ergänzt `POST /api/v1/devices/{id}/instructions`. Ein Geräteverantwortlicher übermittelt `expected_version`, `performed_on`, `instructor`, `trainee`, `instruction_scope` (`device` oder `model`), `document_version`, `document_id` sowie die üblichen `evidence`, `qualification`, `assignment`, `note`. Bauartbezogene Einweisung benötigt zusätzlich `transfer_evidence`; die Übertragbarkeit wird ausdrücklich dokumentiert und nicht aus ähnlichen Gerätenamen abgeleitet. Personen werden derzeit als Namen/Referenzen erfasst, unabhängig von App-Konten. Die Dokument-ID muss zu genau dieser Geräteakte gehören. Das unveränderliche Ereignis hält auch Dokumentmetadaten und SHA-256 fest. Die Geräteübersicht zeigt Einweisungen; Rechte oder medizinische Nutzungsfreigaben entstehen dadurch nicht.

`POST /api/v1/devices/{id}/documents` benötigt Geräteverantwortlichenrechte, Session/Origin/CSRF, Idempotenz und `expected_version`. JSON enthält `filename`, `media_type`, `content` (Base64) und `note`. Erlaubt sind PDF, PNG, JPEG und UTF-8-Text bis 4 MiB. Dateinamen dürfen keine Pfadtrenner/Steuerzeichen enthalten; Erweiterung und deklarierter Typ müssen passen. PDF erhält eine begrenzte Header-/Endsignaturprüfung, Bilder eine begrenzte Dimensions- und vollständige Decodeprüfung, Text eine UTF-8-/Steuerzeichenprüfung. Diese Prüfungen sind keine Malware- oder Fachprüfung.

Alle Uploads bleiben unveränderlich `quarantined`: ein Scanner, eine Dokumentfreigabe und ihre Nachweise fehlen noch. `GET /api/v1/devices/{id}/documents` liefert bereichsbezogene Metadaten (höchstens 500, gegebenenfalls `X-Has-More`). `GET /api/v1/devices/{id}/documents/{document}/download?review=true` erlaubt ausschließlich Geräteverantwortlichen im Gerätebereich den ausdrücklichen Prüfdownload; ohne `review=true` bleibt der Download gesperrt. Der Server prüft die SHA-256, liefert ausschließlich als Attachment mit `application/octet-stream`, `nosniff`, Sandbox und ohne Cache. Es gibt keinen öffentlichen Dateipfad oder Inline-Preview. Auch referenzierte Einweisungsanhänge bleiben ungeprüft; `readiness` bleibt `not_assessed`.

Für diesen begrenzten Entwicklungsstand liegen Datei-BLOB, Metadaten, Audit, Outbox und Wiederholantwort atomar in SQLite. Das ersetzt zunächst den im Blueprint konzeptionell getrennten Dateispeicher. Neue Uploads erhalten neue IDs; bestehende Nachweise werden nicht überschrieben. Der bestehende konsistente SQLite-Snapshot umfasst daher auch alle Anhänge. Ein synthetischer Restore-Test prüft Inhalt/Hash, Geräte-/Einweisungsbezug und Datenbankintegrität. Externer Objektspeicher, produktive Volumenplanung, Aufbewahrungs-/Korrekturverfahren, Scanneranbindung und vollständige Betriebs-Restore-Abnahme bleiben offen.

Die Oberfläche hält Datei und Wiederholschlüssel bei einer unbestätigten Antwort im Arbeitsspeicher, sperrt parallel neue Gerätenachweise und wiederholt identisch. Nach erfolgreichem Upload muss der Anhang für die Einweisung ausdrücklich ausgewählt werden. Eine geladene Geräteversion wird nicht stillschweigend aktualisiert. Ein Seitenneustart verwirft ungesicherte Entwürfe.


### Geräte und Zubehör in Sollversionen

Schema 11 ergänzt unveränderliche Geräteanforderungen je Sollpackliste. `POST /api/v1/packlists` akzeptiert optional `devices`: bis zu 25 Einträge mit `device_id` und `accessories` (bis zu 25 Artikel pro Gerät, jeweils `product_id`, `quantity`). Geräte müssen am Sollort registriert sein; doppelte Geräte-/Zubehörpositionen werden abgewiesen. Zubehör muss zugleich als Material-Sollposition enthalten sein. Alle gerätebezogenen Mengen desselben Artikels werden addiert und dürfen dessen Material-Sollmenge nicht überschreiten. Ein gemeinsamer Bestand wird damit nicht mehrfach als eigenes Zubehör zugeteilt. Die Oberfläche bietet Geräteauswahl und Zubehörpositionen; Bearbeitungsbasis, Wiederholschutz und Rechte der Sollversion bleiben unverändert.

`GET /api/v1/packlists/{id}/assessment` liefert eine aktuelle, bereichsbezogene digitale Bewertung mit `scope=material_and_devices`, separatem `material`, `devices` und konkreten Gründen. Fehlende Inbetriebnahme, unbekannte/überfällige Kontrollen, aktive Sperren, ungeprüfte Einweisungsanhänge und Zubehör-Sollabweichungen werden sichtbar. Zubehörbewertung verwendet konservativ den nutzbaren Bestand und die Restlaufzeit der zugehörigen Material-Sollposition; physische Befestigung/Zuordnung ist damit nicht bestätigt. Materialabweichungen und aktive technische Sperren verhindern eine positive Gesamtinterpretation.

Der bestehende `/readiness`- und `/checks`-Ablauf bleibt ausdrücklich `material_only`. Eine Defektmeldung oder Geräteprüfung verändert den Material-Prüfhash nicht. Der neue Assessment-Endpunkt beurteilt den aktuellen Gerätestand zusätzlich, auch wenn Material bereits `ready` ist. Leere Geräteanforderungen bestätigen keine Ausnahme, sondern nennen fehlende Geräte-Sollkonfiguration. `overall_readiness` bleibt in jedem Fall `not_assessed`: die physische Anwesenheits-/Zubehör-/Funktionsprüfung und vollständige Einweisungs-/Nutzerbewertung fehlen noch. Eine ausdrücklich geschlossene Sperre allein ersetzt diese Prüfungen nicht.

Bestehende Sollversionen und ihre Material-Prüfhashes bleiben bei der Migration unverändert; ohne Gerätepositionen wird kein neues Feld in ihre kanonische Datenbasis aufgenommen. Neue Geräteanforderungen werden mit der neuen Sollversion atomar samt Audit/Outbox und Wiederholantwort veröffentlicht.
