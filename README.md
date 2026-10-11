# DRK Warenbestand

Einfacher HTTP/HTTPS-Teststart: [Docker-Schnellstart](docs/schnellstart-docker.md).

Administrative Verwaltung von Medizinprodukten, Sanitätsmaterial und mobilen Packlisten. Fachliche Grundlage: [Architektur-Blueprint v2](docs/blueprint-v2.md). Seine rechtlichen Quellen wurden beim Umgebungsetup nicht erneut geprüft.

## Implementierter Entwicklungsstand

Modularer Go-Dienst mit eingebetteter HTML/JS-Oberfläche, SQLite und expliziten Migrationen. Synthetische Betreiber, Betriebsstätte und Lagerorte. HTTPS-Anmeldung, widerrufbare Sessions und Rechte je Betriebsstätte. Die Oberfläche zeigt Lagerorte und Materialbestand im zugewiesenen Bereich und bietet Wareneingang, Umlagerung, Entnahme, Gegenbuchung und Inventur. Das Bewegungsjournal zeigt frühere Einträge und deren Korrekturbezüge.

Geräteakten mit Kontrollen, Sperren, Einweisungen und Anhängen in Quarantäne sind vorhanden. Sollpacklisten erfassen Geräte und Zubehör sowie getrennte physische Material- und Geräteprüfnachweise. Noch offen: weitere Rollen/Aktionen und Geräteabläufe, Nutzerqualifikation und vollständige Einsatzfreigabe, Malwareprüfung und Dokumentfreigabe, vollständige PWA, produktive Proxy-Konfiguration und vollständige Backup-/Restore-Verfahren. Nur synthetische Daten verwenden. Der Entwicklungsserver bindet standardmäßig an Loopback; die separate NAS-Konfiguration aktiviert eine Netzwerkbindung ausdrücklich. Forwarded-Header werden nicht vertraut.

## Schnelle Erstinventarisierung

[Excel-Vorlage für Geräte und Equipment](docs/vorlagen/Geraete-Ersterfassung.xlsx) mit 100 festen Inventarnummern, Code-39-Etiketten und USB-Scanner-Suche. [Kurzanleitung](docs/erstinventarisierung.md). Im Programm: CSV UTF-8 prüfen, Standorte zuordnen und atomar importieren. Wiederholungen werden übersprungen; bestehende Akten werden nicht überschrieben. Fehlende Grunddaten bleiben ungeklärt, Defekt/Quarantäne erzeugen echte Sperren. Neue Vorlagen mit eigenen Nummern lassen sich im Programm oder mit `bin/inventory device-template --out NEUER-PFAD.xlsx` erzeugen. [Excel-Vorlage für Verbrauchsmaterial](docs/vorlagen/Verbrauchsmaterial-Ersterfassung.xlsx) und [Anleitung](docs/verbrauchsmaterial-erfassung.md) sind ebenfalls verfügbar: Erstbestand oder späterer Zugang in Basiseinheiten, mit Vorschau, Chargen/Verfall und dauerhafter Erfassungskennung gegen Doppelbuchung.

## NAS und Dockhand

Für den Ugreen DXP2800: [Installationsanleitung](docs/nas-dockhand.md), [NAS-Compose-Datei](compose.nas.yaml) und [Traefik-Beispiel](deploy/traefik-nas.example.yaml). Das Image wird aus dem GitHub-Checkout auf dem NAS gebaut. Browser und Backend verwenden HTTPS; Zugriff vom VPS erfolgt über Tailscale. Getestete NAS-Konfiguration für synthetische Testdaten, kein abgeschlossener produktiver Betrieb.

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

Die Berechnungsgrundlage ist getestet. Registrierte Geräteakten sind inzwischen vorhanden; versionierte Kontrollregeln und Inbetriebnahme sind integriert. Die Anbindung weiterer Prüfungen folgt. Datenbankschema: Version 22.


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


### Physische Geräteprüfung in Sollpacklisten

Schema 12 speichert unveränderliche Geräteprüfnachweise, Akteur, Zeitpunkt, Prüfnotiz, Ergebnisse und Datenbasis atomar mit eigener Outbox und exakter Wiederholantwort. `GET /api/v1/packlists/{id}/assessment` liefert `device_basis_hash` und `last_device_check`; `GET .../device-checks` liefert die letzten 50 Nachweise (weitere Einträge: `X-Has-More`). Die Oberfläche zeigt aktuelle/veraltete Nachweise und Historie.

`POST /api/v1/packlists/{id}/device-checks` benötigt `basis_hash`, `note`, sowie genau einen `results`-Eintrag pro Sollgerät mit `device_id` und den ausdrücklichen booleschen Ergebnissen `present`, `functional`, `accessories_complete`. Helper und Materialverantwortliche dürfen innerhalb ihres Bereichs operative Sicht-/Funktionsprüfungen dokumentieren; dies ersetzt keine STK/MTK, fachliche Kontrollberechtigung oder Einweisung. Auch ohne Zubehör-Soll muss dessen Anwendbarkeit bei der Prüfung ausdrücklich bestätigt werden. CSRF, Sitzung, Bereich und gemeinsamer Benutzer-/Idempotenzschlüsselraum werden geprüft. Verlorene Antworten lassen sich mit identischem Schlüssel und Inhalt wiederholen; bei geänderter Basis muss neu geprüft werden.

Die Datenbasis umfasst Sollversion, Materialbasis mit relevanten Bestandsversionen, Berliner Tagesdatum sowie Geräteakten einschließlich Versionsstand, Fristen, Sperren und Einweisungen. Bestands-/Geräteänderungen, Tageswechsel, ersetzte Sollversion oder Ablauf der konfigurierten Prüfgültigkeit machen den Nachweis veraltet. Ein aktueller negativer Nachweis blockiert die Bewertung und nennt fehlendes Gerät, Funktionsabweichung oder unvollständiges Zubehör. Ein aktueller positiver Nachweis kann `physically_checked` ergeben; bestehende digitale Sperren, überfällige Kontrollen und ungeklärte Daten bleiben wirksam. Die Materialprüfung bleibt separat. `overall_readiness` bleibt stets `not_assessed`: Nutzerqualifikation, Anwendbarkeit rechtlicher Pflichten und vollständige Einsatzfreigabe sind weiterhin offen. Diese operative Prüfung ist kein Nachweis einer gesetzlichen Funktionsprüfung nach § 11 MPBetreibV.

### Geräte-Grunddaten vervollständigen und korrigieren

Schema 15 ergänzt `POST /api/v1/devices/{id}/details`. Geräteverantwortliche ändern `details` mit `name`, `manufacturer`, `model`, `serial_or_lot`, aktueller `expected_version`, `evidence` und `note`. Unbekannte Identifikationsangaben bleiben leer; Bezeichnung ist erforderlich. Inventarnummer und Lagerort bleiben fest. Jede Änderung wird mit Vorher-/Nachher-Werten, Akteur, Zeit, gemeinsamer Geräteversion, Outbox und exakter Wiederholantwort gespeichert. Registrierung und frühere Nachweise bleiben unverändert; Akte und Übersicht zeigen die jüngsten Grunddaten. Details erzeugen weder Inbetriebnahme noch Prüfungen/Einweisungen oder Freigaben.

Wird Hersteller, Modell oder Serien-/Losnummer nach einer Inbetriebnahme, Prüfung oder Einweisung geändert, erzeugt das Ereignis eine eigene Quarantänesperre. Kontrollregeln und Nachweise bleiben historisch erhalten, müssen vor einer ausdrücklichen Freigabe geprüft werden. Eine reine Bezeichnungsänderung erzeugt keine neue Sperre. Geräte-Prüfbasis und Version ändern sich; physische Prüfnachweise müssen neu bewertet werden. Die Oberfläche lädt die aktuellen Werte und zeigt die Änderungshistorie. Ein unveränderter CSV-Wiederimport überspringt das ursprüngliche Objekt und setzt spätere Ergänzungen nicht zurück.

### Geräteübersicht als CSV

In der Geräteübersicht öffnet „Geräteübersicht als CSV exportieren“ den geschützten Download `GET /api/v1/devices/export.csv`. Exportiert wird ein konsistenter SQLite-Lesestand innerhalb der aktuell freigegebenen Betriebsstätten: Betreiber, Betriebsstätte, Lagerort, Geräteidentifikation, aktuelle Grunddaten/Version, Inbetriebnahme, STK-/MTK-/elektrische Zustände und Fristen, aktive Sperren, Anzahl der Einweisungsnachweise sowie Bewertungshinweise. Alle vorhandenen Geräte-Leserollen dürfen diesen Überblick herunterladen; ein fremder Bereich ist ausgeschlossen. Ohne Sitzung gibt es keinen Download.

UTF-8 mit BOM, Semikolon und CRLF dient dem Öffnen in Excel. Inventar- und Serien-/Losnummern erhalten ein führendes Apostroph als Textschutz; unbekannte Werte bleiben leer. Formelähnliche Textzellen werden ebenfalls mit Apostroph geschützt. Dies ist eine Auswertung und keine Importvorlage: nicht als Erstinventarisierungsdatei zurückimportieren. Zeitpunkt im CSV und `X-Data-As-Of`, Geräteversion und `X-Export-Rows` beschreiben den Stand. Spätere Änderungen aktualisieren eine bereits heruntergeladene Datei nicht.

Grenzen: 1000 Geräte, 8 MiB, zehn Sekunden Erzeugungszeit und zwei gleichzeitige Exporte pro Server. Bei Überschreitung entsteht keine teilweise CSV-Datei. Keine Schemaänderung. Dieser Überblick ersetzt weder einen vollständigen Medizinproduktebuch-Export noch eine Einsatzfreigabe; vollständige Fachhistorien, Personen-/Qualifikationsbewertung und Nachweisarchive sind weitere Schritte.

### Geräteakte mit Fachhistorie exportieren

Jedes Gerät in der Übersicht bietet „Geräteakte mit Historie exportieren“. `GET /api/v1/devices/{id}/export.zip` liefert einen konsistenten SQLite-Lesestand als ZIP mit `geraeteakte.html` (offline lesbar und über den Browser druckbar), `akte.json` (strukturierte Daten) und `manifest.json` (Dateigrößen/SHA-256, Geräte-ID/-version, Datenstand und Anhangsmetadaten). HTML benötigt keine externen Dateien, Skripte oder Dienste; eingegebene Texte werden escaped. Die Fachereignisse stehen in Geräteversionsfolge mit Ereignis-ID, Akteur-ID, Zeit und vollständigem gespeichertem Nachweis. Die unveränderte Registrierung, aktuelle Kontrollregeln/Fristen/Sperren/Einweisungen, gerätebezogene Erstinventarisierungs-Herkunft und Anhangserfassungen sind enthalten. Eine Herkunfts-Prüfsumme bindet nur die erfasste Importzeile; andere Geräte oder vollständige Importdateien werden nicht mit exportiert.

Alle Geräte-Leserollen dürfen Akten innerhalb ihres Bereichs exportieren. Fremde oder unbekannte IDs ergeben dieselbe 404-Antwort. Anhänge in Quarantäne werden nicht als Dateien verpackt: Metadaten, SHA-256 und ihre gebundenen Nachweisreferenzen bleiben sichtbar; der gesonderte Prüfdownload bleibt Geräteverantwortlichen vorbehalten. SHA-256 ermöglicht Konsistenzprüfungen, ist keine Signatur oder unabhängige Herkunftsbestätigung. Personenangaben aus Einweisungen bleiben Teil der geschützten Akte; eine heruntergeladene Kopie unterliegt anschließend der Verantwortung des Empfängers.

Dieser Export enthält vollständig die gespeicherten Fachdaten **dieser Geräteakte**, einschließlich Historien über die 500 Einträge der Bildschirmansicht hinaus. Er bestätigt keine vollständige rechtliche Dokumentation: bisher nicht strukturierte Pflichtangaben und Abläufe werden im Export ausdrücklich als Lücken benannt. Keine automatische Einsatzfreigabe, keine Aufhebung von Sperren oder Quarantäne. Grenze je Historienbereich: 2000 Einträge, insgesamt 8 MiB unkomprimierte Exportdateien, zehn Sekunden Erzeugungszeit. Die zwei gemeinsamen Exportplätze gelten auch für CSV. Bei Überschreitung wird das gesamte Archiv abgelehnt; keine stille Kürzung. Dieser Export erfordert keine eigene Schemaänderung; aktueller Gesamtstand: Version 22.

### Bestandsverzeichnis-Stammdaten (Schema 16)

„Bestandsverzeichnis-Stammdaten pflegen“ ergänzt Geräteart, Anschaffungsjahr, zuständigen Wirtschaftsakteur mit Funktion/Name/Anschrift und betriebliche Zuordnung. Der Geräteverantwortliche erfasst eine Nachweisreferenz und Begründung; Vorher-/Nachher-Werte bleiben in der unveränderlichen Fachhistorie erhalten. Der aktuelle Stand und historische Anschriften erscheinen in Akte, CSV und ZIP-Export. Ungeklärte Angaben bleiben leer; Anschaffungsjahr ist das Erwerbsjahr und darf nicht in der Zukunft liegen. Die Funktion `manufacturer`, `authorized_representative` oder `importer` muss bei Namens-/Adressangaben ausdrücklich gewählt werden. Herstellername in der Geräteidentifikation und Wirtschaftsakteur sind getrennt; keine automatische Ableitung. Der begrenzte Entwicklungsstand erfasst einen zuständigen Akteur pro Geräteversion, keine vollständige Liste aller Wirtschaftsakteure.

`POST /api/v1/devices/{id}/master-data` benötigt `expected_version`, `evidence`, `note` und `master_data` mit `asset_type`, `acquisition_year` (Textjahr oder leer), `actor_role`, `actor_name`, `actor_address`, `operational_assignment`. Es ersetzt diese sechs Felder gemeinsam aus der geladenen Geräteversion. Bestehende Daten daher vorher laden; leere Werte können ausdrücklich frühere Angaben entfernen und bleiben nachvollziehbar. Separate Identifikationsänderungen bleiben im bisherigen Grunddatenablauf. Gemeinsame Geräteversion/Outbox/Wiederholschutz, Bereichsrechte und atomare Fehlerbehandlung gelten weiterhin. Ein unveränderter CSV-Wiederimport überschreibt diese Ergänzungen nicht.

Keine automatische Inbetriebnahme, Kontrollpflichtbestätigung oder Einsatzfreigabe. Ein Stammdatenereignis verändert die Geräte-Prüfbasis und veraltet bisherige physische Geräteprüfungen; Material-Prüfbasis bleibt unabhängig. Neue Anschriften allein erzeugen keine technische Quarantänesperre und heben vorhandene Sperren nicht auf. Altakten ohne Ergänzung und ältere Wiederholantworten bleiben unverändert. Schema-15-Installationen vor dem Update sichern und ausdrücklich auf Schema 16 migrieren; der einfache Docker-Start migriert bestehende Datenbanken nicht automatisch.

### Instandhaltungsnachweise (Schema 17)

Geräteverantwortliche dokumentieren in der Geräteakte ausgeführte Instandhaltung, Wartung oder Reparatur mit Datum, ausführender Person/Firma, Maßnahme und ausdrücklich gewähltem Ergebnis. `POST /api/v1/devices/{id}/maintenance` benötigt `expected_version`, `maintenance_kind` (`maintenance`, `servicing`, `repair`), `performed_on`, `examiner` (Ausführender), `method` (Maßnahme), `result` (`completed`, `follow_up_required`, `failed`) sowie `evidence`, `qualification`, `assignment`, `note`. Auch vorbereitende Maßnahmen vor der Inbetriebnahme können dokumentiert werden; zukünftige Termine sind keine ausgeführten Nachweise.

Ein optionales `document_id` muss zu dieser Geräteakte gehören; ID, Metadaten und SHA-256 werden fest gebunden. Anhänge bleiben in Quarantäne. Geräteversion, Akteur, Zeitpunkt, Outbox und exakte Wiederholantwort werden atomar gespeichert. Die Geräteakte und ihr ZIP-Archiv enthalten die Historie und Nachweise; der CSV-Überblick nennt deren Anzahl.

`failed` erzeugt eine eigene Defektsperre, `follow_up_required` eine eigene Quarantänesperre. `completed` hebt keine Sperre auf, bestätigt keine Kontrollpflicht und setzt keine STK-/MTK-/elektrische Frist neu. Jede Sperre benötigt eine ausdrückliche separate Freigabe. Ein neuer Nachweis veraltet physische Geräteprüfungen; Materialprüfungen bleiben unabhängig. Identifikationsänderungen nach einem Instandhaltungsnachweis erzeugen technische Quarantäne. Bestehende Akten ohne solche Einträge behalten ihre bisherigen Prüfbasiswerte.

Bestehende Installationen vor dem Update sichern und ausdrücklich auf Schema 17 migrieren. Der einfache Docker-Start migriert vorhandene Datenbanken weiterhin nicht automatisch. Eine Nachweisreferenz ist noch keine unabhängige Prüfung von Qualifikation, Dokumentinhalt oder rechtlicher Vollständigkeit.

### Außerbetriebnahme und Mindestaufbewahrung (Schema 18)

Geräteverantwortliche dokumentieren `POST /api/v1/devices/{id}/decommission` mit `expected_version`, `performed_on`, `evidence` und `note`. Die Oberfläche bietet dafür eine eigene Aktion. Das Datum muss gültig, nicht zukünftig und mindestens so spät wie bereits dokumentierte Inbetriebnahme, Prüfungen, Einweisungen und Instandhaltungen sein. Eine zweite Außerbetriebnahme wird abgelehnt; identische alte Anfragen behalten ihre gespeicherte Antwort.

Die Geräteakte bleibt lesbar und exportierbar. `lifecycle=DECOMMISSIONED` blockiert die Geräte-Sollbewertung unabhängig von einzelnen Sperren oder positiven physischen Prüfnachweisen. Neue Inbetriebnahmen, Kontrollregeln, Prüfungen, Einweisungen und Instandhaltungen werden abgelehnt. Administrative Grund-/Stammdatenänderungen, Sperrmeldungen und ausdrücklich dokumentierte Freigaben einzelner Sperren bleiben möglich; eine Sperrenfreigabe reaktiviert das Gerät niemals. Wiederinbetriebnahme und ergänzende Korrektur fehlerhafter Fachnachweise benötigen einen gesonderten künftigen Ablauf.

`decommission.retain_through` nennt einen vorsorglichen Mindesttermin fünf Kalenderjahre nach Außerbetriebnahme, einschließlich dieses Tages. Für den 29. Februar wird konservativ der 1. März des fünften Folgejahres verwendet. Der Termin wird auch bei Equipment angezeigt und bestätigt keine individuelle Buchpflichtbewertung. Längere Fachfristen, Aufbewahrungssperren und Datenschutz-/Löschregeln bleiben zu prüfen. Es gibt keine automatische Löschung. HTML-/JSON-Akte und CSV zeigen Datum und Mindesttermin; gemeinsame Version, Audit/Outbox, Wiederholschutz und Bereichsrechte bleiben erhalten. Material-Prüfbasis bleibt unabhängig, ältere physische Geräteprüfungen werden veraltet.

Vor dem Update einer bestehenden Installation eine neue Sicherung erstellen und ausdrücklich auf Schema 18 migrieren; vorhandene Datenbanken werden beim einfachen Docker-Start nicht automatisch aktualisiert.

### Ergänzende Richtigstellungen von Fachnachweisen (Schema 19)

Geräteverantwortliche erfassen `POST /api/v1/devices/{id}/corrections` mit `expected_version`, `correction` (`target_version`, `statement`), `evidence`, `qualification`, `assignment`, `note` und optionalem `document_id`. Die mehrzeilige Richtigstellung umfasst höchstens 2000 Zeichen; die gemeinsame Anfragegrenze bleibt 8 KiB. Qualifikations-/Auftragsreferenzen belegen die verantwortliche Bearbeitung, ohne unabhängige Prüfung dieser Angaben. Die Oberfläche bietet Ursprungsauswahl aus der geladenen Fachhistorie, Text und optionalen Anhang; bei mehr als 500 Einträgen sind ältere Ursprungsversionen weiterhin über API und vollständiges Aktenarchiv zugänglich.

Zulässige Ursprünge sind Inbetriebnahme, Kontrollregeln, Prüfung, Sperrmeldung, Sperrenfreigabe, Einweisung, Instandhaltung und Außerbetriebnahme derselben Geräteakte. Grund-/Stammdaten haben eigene Änderungsabläufe. Weitere Richtigstellungen beziehen sich erneut auf den ursprünglichen Fachnachweis; Richtigstellungen selbst werden nicht als Ursprung gewählt. Ursprungs-ID, Geräteversion, Aktion, Akteur, Erfassungszeit und SHA-256 des unveränderten Original-Payloads werden fest gebunden. Optionale Anhänge müssen zur Akte gehören; ihre Metadaten/Prüfsummen bleiben gebunden und Inhalte in Quarantäne. Version, Ereignis, Outbox und exakte Wiederholantwort werden atomar gespeichert.

Jede Richtigstellung erzeugt eine unabhängige Quarantänesperre zur fachlichen Klärung. Sie ergänzt richtiggestellte Angaben, ändert aber keine strukturierten früheren Ergebnisse, Fristbasen, Regeln, Einweisungen oder Lebenszyklen automatisch. Eine technische Umsetzung benötigt den passenden gesonderten Fachablauf und eine ausdrückliche Freigabe jeder betroffenen Sperre; weitergehende Ersatznachweis-/Wiederinbetriebnahmeverfahren sind noch offen. Auch nach Außerbetriebnahme können Richtigstellungen ergänzt werden, ohne das Gerät zu reaktivieren oder den Aufbewahrungstermin zu verkürzen. Alte Wiederholungen erzeugen weder neue Einträge noch erneut freigegebene Sperren.

Die physische Geräte-Prüfbasis ändert sich; Materialprüfungen bleiben unabhängig. HTML/JSON/ZIP zeigen Originalhistorie und Richtigstellungen samt Ursprung/Hash; CSV ergänzt deren Anzahl. Bestehende Installationen vor dem Update sichern und ausdrücklich auf Schema 19 migrieren. Keine automatische Migration vorhandener Datenbanken beim einfachen Docker-Start.

### Störungen und Meldebezüge (Schema 20)

`POST /api/v1/devices/{id}/incidents` benötigt `expected_version`, `performed_on`, `incident` mit ausdrücklich gewähltem `kind` (`malfunction`, `repeated_operating_error`, `suspected_serious_incident`), `description`, `consequences`, außerdem `evidence`, `note` und optionales gerätegebundenes `document_id`. Helfer und Geräteverantwortliche dürfen Störungen in ihrem Bereich dokumentieren; dies bestätigt keine fachliche Meldepflichtbewertung. Unbekannte Folgen ausdrücklich benennen. Keine Patientenangaben erfassen, sondern Referenzen zum geschützten Fachverfahren verwenden. Datum gültig/nicht zukünftig; Texte auf jeweils 500 UTF-8-Bytes begrenzt. Störungen erzeugen eine eigene Defektsperre, wiederholte Bedienungsfehler und Verdachtsfälle eine eigene Quarantänesperre. Initialer Meldestatus: `ASSESSMENT_REQUIRED`.

`POST /api/v1/devices/{id}/reports` ist Geräteverantwortlichen vorbehalten. Eingaben: `expected_version`, `report` mit `incident_version`, ausdrücklich gewähltem `status` (`ASSESSMENT_REQUIRED`, `REPORT_PENDING`, `REPORTED`, `FOLLOW_UP`, `CLOSED`), `responsible`, `assessment_reference`, `authority`, `authority_reference`, `authority_reported_at`, `manufacturer_contact`, `manufacturer_reference`, `manufacturer_reported_at`; außerdem `evidence`, `qualification`, `assignment`, `note` und optionaler Anhang. Leere Meldebezüge bleiben unbekannt. Eine dokumentierte Meldung benötigt jeweils Empfänger, Referenz und RFC3339-Zeitpunkt mit Zeitzone, nicht zukünftig und nicht vor dem Berliner Störungstag. Die Oberfläche erfasst Ortszeit und sendet den entsprechenden UTC-Zeitpunkt; gespeicherte Angaben werden bei neuer Bearbeitung vorgeladen.

`REPORTED` erfordert eine tatsächliche dokumentierte Behördenmeldung; Herstellerkontakt allein genügt nicht. `CLOSED` benötigt eine ausdrückliche Bewertungs-/Entscheidungsreferenz, auch bei begründet nicht meldepflichtiger Störung. Vorher dokumentierte Behörden-/Herstellerbezüge dürfen nicht durch leere Werte entfernt werden. Statusänderungen sind belegte manuelle Einträge, keine automatische rechtliche Entscheidung oder extern verifizierte Zustellung. Externe unverzügliche Meldungen selbst veranlassen; die Anwendung verschickt nichts und verlangt keine interne Freigabe vor der externen Meldung. Die unabhängige Qualifikations-/Meldepflichtbewertung bleibt offen.

Jede Meldestatusänderung ergänzt unveränderliche Historie, verantwortlichen Akteur/Zeitpunkt und Nachweis. Offene Meldebewertungen blockieren die Geräte-Sollbewertung unabhängig von einzeln freigegebenen Sperren. Abschluss hebt keine Sperre auf und bestätigt keine Einsatzbereitschaft. Nach Außerbetriebnahme sind Nachträge möglich, ohne Reaktivierung oder verkürzte Aufbewahrung. Störungen und Meldeereignisse können separat richtiggestellt werden. Version/Outbox/Wiederholschutz/Bereichsrechte und atomarer Rollback gelten; Anhänge bleiben in Quarantäne. Physische Geräteprüfungen veralten, Materialbasis bleibt unabhängig. Oberfläche/HTML/JSON/ZIP enthalten Meldebezüge und Historie; CSV nennt Störungen und offene Bewertungen.

Bestehende Installationen benötigen vor Update eine neue Sicherung und ausdrückliche Migration auf Schema 20; vorhandene Datenbanken werden beim einfachen Docker-Start nicht automatisch aktualisiert.

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

Der bestehende Container kann Kontrollerinnerungen verschicken, wenn eine SMTP-Konfiguration ausdrücklich bereitgestellt wird. Standard: deaktiviert. TLS oder STARTTLS mit geprüftem Serverzertifikat ist erforderlich; HTTP im lokalen Browser ändert die SMTP-Sicherheit nicht. Empfänger werden ausdrücklich je Betriebsstätte festgelegt, ohne automatische Übernahme von Konto-Adressen. Details und Compose-Overlay: [SMTP-Anleitung](docs/smtp-erinnerungen.md).

Zustellungen haben stabile Message-IDs, Empfängerbindung, kurze Datenbank-Leases und unveränderliche Versuchshistorien. SMTP läuft außerhalb der SQLite-Transaktionen. Aktuelle Kontrollzyklen, Schwellen, Außerbetriebnahme und Empfängerkonfiguration werden vor jedem Versandversuch geprüft. Vorbereitete überholte Hinweise werden verworfen. Eine Änderung während bereits laufender SMTP-Übertragung kann deren Annahme nicht atomar zurücknehmen.

SMTP-Annahme ist keine Bestätigung der Postfachzustellung. Temporäre Fehler werden mit 1/5/15/60 Minuten Abstand bis insgesamt fünf Versuchen wiederholt. Dauerhafte Ablehnung oder Zertifikatsfehler verlangen manuelle Klärung. Verlorene DATA-Antworten, abgebrochene Prozesse oder abgelaufene Versand-Leases bleiben als unklar stehen; keine automatische Wiederholung mit möglicher Doppelmail. Ein SMTP-Erfolg mit anschließend verlorener Datenbankbestätigung kann ebenfalls unklar werden. Exakt-einmalige externe Zustellung wird nicht zugesichert.

`GET /api/v1/devices/reminder-mail` und die Oberfläche zeigen die letzten 200 Zustellungen im berechtigten Bereich, Status, Versuche und Message-ID, ohne Empfängeradressen oder Zugangsdaten auszugeben. Weitere Einträge werden ausdrücklich angekündigt. Der Status bleibt auch nach Außerbetriebnahme sichtbar. Wiederaufnahme erfolgt über die lokale CLI; bei unklarer Annahme ist nach externer Prüfung eine ausdrückliche Bestätigung des Doppelmail-Risikos erforderlich. SMTP-Protokolltexte, Passwörter und Mailinhalte werden nicht geloggt. Die bisherigen Bewegungs-/Prüf-Outboxen werden nicht per E-Mail versendet. Bestände, Fristen, Sperren und Prüfnachweise bleiben von Zustellfehlern unabhängig.

Bestehende Installation vor Update sichern, ausdrücklich auf Schema 22 migrieren und anschließend starten. Sicherungen umfassen Warteschlange, Versuchshistorien und Empfängerbindungen; SMTP-Dateien/Secrets separat geschützt sichern.

Geprüft: vollständige Go-Race-Suite, Vet/Build, echte lokale TLS-/STARTTLS-Verbindungen einschließlich Anmeldung, geprüfte Zertifikate, fehlendes STARTTLS, Header-/Konfigurationsprüfung, 4xx/5xx, verlorene DATA-Antwort und QUIT-Abbruch nach Annahme. Warteschlange prüft Rollback, konkurrierende Worker, abgelaufene Leases, begrenzte Wiederholungen, stabile Message-IDs, manuelle Freigabe und Empfänger-/Fristenänderungen. Browser prüft Versandstatus, sichere Texte, Fehlerbereinigung und verspätete Antworten nach Abmeldung. HTTPS-Compose-Test mit lokalem TLS-SMTP-Testserver bestand einschließlich Backup/Migration 21→22, Standard ohne Versand, aktivem Overlay, Wiederholschutz, Neustartpersistenz, Snapshot-Integrität und Bereichsrechten. Frühere Bestände, Dokumente, Nachweise und Geräte-/Materialprüfbasiswerte blieben unverändert. Lokale Backup-/Migrationsprüfung bestand. Die Entwicklungsinstanz läuft wieder ohne SMTP-Konfiguration; reale SMTP-Konten und Postfachzustellung sind nicht geprüft.
