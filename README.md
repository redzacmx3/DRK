# DRK Warenbestand

Administrative Verwaltung von Medizinprodukten, Sanitätsmaterial und mobilen Packlisten. Fachliche Grundlage: [Architektur-Blueprint v2](docs/blueprint-v2.md). Seine rechtlichen Quellen wurden beim Umgebungsetup nicht erneut geprüft.

## Implementierter Entwicklungsstand

Modularer Go-Dienst mit eingebetteter HTML/JS-Oberfläche, SQLite und expliziten Migrationen. Synthetische Betreiber, Betriebsstätte und Lagerorte. HTTPS-Anmeldung, widerrufbare Sessions und Viewer-Rechte je Betriebsstätte. Die Oberfläche zeigt nur Lagerorte im zugewiesenen Bereich.

Noch offen: weitere Rollen/Aktionen, Bestandsbuchungen, Geräteakte, vollständige PWA, Dokumentenspeicher, produktive Proxy-Konfiguration und vollständige Backup-/Restore-Verfahren. Nur synthetische Daten verwenden. Der Entwicklungsserver bindet ausschließlich an Loopback; Forwarded-Header werden nicht vertraut.

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

## Konten und Viewer-Bereiche

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

Bereichsänderung und Kontosperre widerrufen vorhandene Sessions atomar. Keine Web-Administration und keine automatische fachliche Qualifikation durch diese Viewer-Rechte.

## Sessions und API

Passwörter: Argon2id mit zufälligem Salt, 64 MiB, drei Iterationen, Parallelität zwei. Session: 256-Bit-Zufallswert als `__Host-session` mit Secure, HttpOnly, Path=/ und SameSite=Lax; die Datenbank speichert nur den Hash. Laufzeit: acht Stunden absolut, 30 Minuten Inaktivität. Login rotiert eine bestehende Sitzung; Logout widerruft sie serverseitig.

| Endpoint | Verhalten |
|---|---|
| `GET /health/live` | Prozess erreichbar |
| `GET /health/ready` | Schema und Datenbankzugriff geprüft |
| `POST /api/v1/auth/login` | HTTPS und exakter Origin erforderlich; JSON mit username/password |
| `GET /api/v1/me` | Identität, Viewer-Bereiche und CSRF-Token für die aktuelle Sitzung |
| `POST /api/v1/auth/logout` | Origin und `X-CSRF-Token` erforderlich |
| `GET /api/v1/locations` | Nur zugewiesene Betriebsstätten |
| `GET /api/v1/locations/{id}` | Gleiche Objektprüfung; fremde IDs liefern 404 |

Anmeldung ist auf zehn Versuche pro Minute je direkter Client-IP und zwei gleichzeitige Passwortprüfungen begrenzt. Der Limiter ist pro Prozess und wird beim Neustart zurückgesetzt; verteilter Missbrauchsschutz und produktive Proxy-Auswertung sind noch offen. Keine dauerhafte Browserablage von Sitzungs- oder CSRF-Token durch die Oberfläche.

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

Objektberechtigungen auf weitere Fachaktionen ausweiten, dann Materialbewegungen mit atomaren Buchungen und Idempotenz umsetzen. Vor Pilotfreigabe die [Blueprint-Abnahmekriterien](docs/blueprint-v2.md) erfüllen.
