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

Dieser Check prüft die Oberfläche mit simulierter API: ausdrücklichen Zählstart, eingefrorene Basis trotz Neuladen, erneute Zählung nach Konflikt und Nullzählung. Die Go-Tests prüfen die echte SQLite-Transaktion, Rechte, Migration und konkurrierende Korrekturen. Der Browsercheck ersetzt keinen HTTPS-/Compose-Funktionstest.

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

Als Nächstes Packlisten/Einsatzbereitschaft und Scanner umsetzen. Vor Pilotfreigabe die [Blueprint-Abnahmekriterien](docs/blueprint-v2.md) erfüllen.
