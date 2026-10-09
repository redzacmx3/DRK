# DRK Warenbestand

Projektstart für die administrative Verwaltung von Medizinprodukten, Sanitätsmaterial und mobilen Packlisten.

Die fachliche und technische Grundlage ist [Architektur-Blueprint v2](docs/blueprint-v2.md). Seine Anforderungen sind Planungsstand; die beschriebenen Funktionen sind noch nicht implementiert. Die rechtlichen Quellen wurden beim Umgebungsetup nicht erneut geprüft.

## Architektur

Ein modularer Go-Dienst mit eingebettetem PWA-Frontend, REST-API, SQLite auf lokalem Volume und geschütztem Dokumentenspeicher. Eine Betreiberorganisation je Installation. Fachliche Datumsgrenzen verwenden Europe/Berlin.

## Entwicklungsumgebung

In dieser Cloud-Umgebung:

```sh
source /workspace/drk-environment/env.sh
cd /workspace/DRK
go version
```

Go 1.27.2 und GCC (CGO) werden benötigt. Der SQLite-Treiber ist für netzunabhängige Container-Builds in `vendor/` enthalten. SQLite ist über den gepinnten Treiber `github.com/mattn/go-sqlite3` eingebunden.

```sh
export GOPATH=/workspace/drk-environment/cache/gopath
# In dieser Cloud vermeidet direct den gesperrten Storage-Redirect;
# die Go-Prüfsummendatenbank bleibt aktiviert.
GOPROXY=direct go mod download
go test ./...
go vet ./...
go build -o bin/inventory ./cmd/inventory
mkdir -p var
bin/inventory migrate --db var/inventory.db
bin/inventory seed-demo --db var/inventory.db
bin/inventory serve --db var/inventory.db
```

Migrationen sind explizit und wiederholbar. Vor Migration einer bestehenden Datenbank eine konsistente SQLite-Online-Sicherung erstellen; keine isolierte Kopie einer laufenden WAL-Datenbank verwenden. `seed-demo` ist optional, wiederholbar und enthält ausschließlich synthetische Daten.

Der Entwicklungsdienst bindet nur an Loopback. `GET /health/live` prüft den Prozess, `GET /health/ready` das unterstützte Schema und einen tatsächlichen Datenbankzugriff. `GET /api/v1/locations` liefert Lagerorte samt Betreiber/Betriebsstätte. Die Oberfläche liest diese API. Anmeldung, Schreiboperationen und produktiver Betrieb sind noch nicht implementiert; keine realen Daten verwenden. Die eingebettete Oberfläche ist noch keine vollständige PWA.

## Docker Compose (Linux-Entwicklung)

```sh
docker compose build
docker compose run --rm inventory migrate --db /data/inventory.db
docker compose run --rm inventory seed-demo --db /data/inventory.db
docker compose up -d
docker compose exec -T inventory /app/inventory healthcheck
```

Das Image läuft als UID/GID 1000, SQLite liegt im persistenten Volume `inventory_data`. Linux-Hostnetzwerk hält den Dienst auf Host-Loopback; es ist kein produktives Proxy-Deployment. Für Docker Desktop muss Hostnetzwerk ausdrücklich unterstützt/aktiviert sein. `docker compose down` erhält die Daten; `down -v` löscht das Volume.

Vor Produktivbetrieb: Authentifizierung/Objektrechte, Proxy- und TLS-Konfiguration, geschützter Dokumentenspeicher sowie getestete Backup-/Restore-Verfahren ergänzen. Dann das Image mit freigegebenem Digest betreiben und das Proxy-Netzwerk anbinden.

## Erste Umsetzung

Siehe [Projektstart](docs/projektstart.md). Die Blueprint-Abnahmekriterien bleiben Voraussetzung für eine spätere Pilotfreigabe.
