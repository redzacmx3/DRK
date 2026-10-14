# Einfacher Docker-Test mit HTTP oder HTTPS

`compose.local.yaml` startet eine lokale Testinstanz ohne Zertifikatsvorbereitung. Ein Image enthält Programm und Oberfläche; SQLite liegt im separaten persistenten Volume. Die Ugreen DXP2800 nutzt die unterstützte Plattform `linux/amd64`.

Für die erste Inventarisierung in der Stack-Konfiguration `DRK_MODULES=inventory` setzen; mit optionalen Geräteakten `DRK_MODULES=inventory,devices`. Ohne Einstellung bleibt der bisherige volle Funktionsumfang aktiv. [Module und Abhängigkeiten](module.md).

## Sofort aus dem GitHub-Download testen

Im entpackten Projektordner:

```sh
docker compose -f compose.local.yaml up -d --build --wait
docker compose -f compose.local.yaml run --rm init seed-demo --db /data/inventory.db
```

Ein Testkonto anlegen (eigenes Passwort eingeben):

```sh
read -r -s -p 'Passwort: ' DRK_TEST_PASSWORD
printf '\n'
printf '%s\n' "$DRK_TEST_PASSWORD" | docker compose -f compose.local.yaml run --rm -T init create-user --db /data/inventory.db --username test-geraete --site 00000000-0000-4000-8000-000000000002 --role device_manager
unset DRK_TEST_PASSWORD
```

`http://localhost:8080` öffnen. `seed-demo` ist ausschließlich für synthetische Tests gedacht. Für Materialbuchungen ein weiteres Konto mit `--role material_manager` anlegen. Die Rolle bestimmt weiterhin die fachlichen Schreibrechte.

`init` migriert nur eine noch nicht vorhandene Datenbank. Eine bestehende Datenbank wird lediglich auf Bereitschaft geprüft; erforderliche Upgrades müssen nach einem Backup ausdrücklich über `migrate` ausgeführt werden. Nie Datenvolumes löschen, um ein Upgrade zu erzwingen.

## NAS im lokalen Netz

In Dockhand die Variablen setzen oder eine Datei `.env.local` anlegen und bei CLI-Aufrufen zusätzlich `--env-file .env.local` verwenden:

```dotenv
DRK_HTTP=true
DRK_ORIGIN=http://192.168.1.50:8080
DRK_BIND_IP=192.168.1.50
DRK_PORT=8080
```

Die IP durch die NAS-Adresse ersetzen. `DRK_ORIGIN` muss genau der im Browser verwendeten Adresse entsprechen, ohne Pfad oder abschließenden Schrägstrich. Standardmäßig bindet der veröffentlichte Port nur an `127.0.0.1`. Für den NAS-Zugriff muss die gewählte Host-IP erreichbar sein.

HTTP überträgt Passwörter und Daten unverschlüsselt: nur im vertrauenswürdigen lokalen Testnetz einsetzen. Anmeldung, Rechte, CSRF und exakte Origin-Prüfung bleiben aktiv. HTTP verwendet ein eigenes HttpOnly/SameSite-Session-Cookie; HTTPS verwendet weiterhin das Secure-`__Host-session`-Cookie. Nach Umschalten neu anmelden. Kamera-APIs können bei HTTP im LAN im Browser eingeschränkt sein; USB-Tastaturscanner sind davon unabhängig.

## Derselbe Compose-Stack mit HTTPS

```dotenv
DRK_HTTP=false
DRK_ORIGIN=https://inventar.example.de
DRK_BIND_IP=100.64.1.2
DRK_PORT=8443
DRK_TLS_DIR=/volume1/docker/drk/tls
DRK_HEALTH_URL=https://localhost:8080/health/ready
DRK_HEALTH_CA=/certs/cert.pem
```

Zertifikat und Schlüssel vorbereiten und Traefik wie in [nas-dockhand.md](nas-dockhand.md) konfigurieren. `DRK_TLS_DIR` muss `cert.pem` und `key.pem` enthalten; das Healthcheck-Zertifikat muss `localhost` abdecken. Danach `up -d --wait` erneut ausführen. Daten bleiben im selben Volume. Dieser Modus verwendet HTTPS auch zwischen Proxy und NAS. Der Transportmodus ist ausdrücklich eingestellt; Proxy-Header schalten ihn nicht automatisch um.

## Installation eines fertigen Images

Der GitHub-Actions-Workflow `.github/workflows/container.yml` baut nach Push auf `main` ein amd64-Image für `ghcr.io/redzacmx3/drk:latest` und einen unveränderlichen Commit-Tag. Ein erfolgreicher Workflowlauf und die passende Package-Sichtbarkeit sind Voraussetzung; das Vorhandensein der Workflowdatei bestätigt noch keine Veröffentlichung. Für Downloads ohne Anmeldung muss das Package auf GitHub öffentlich sein; sonst vorab `docker login ghcr.io` mit einem berechtigten Konto.

Nach bestätigter Veröffentlichung `DRK_IMAGE=ghcr.io/redzacmx3/drk:latest` in den Stack-Variablen setzen. Es genügt dann die Compose-Datei; der Quellcode wird nicht benötigt:

```sh
docker compose --env-file .env.local -f compose.local.yaml pull
docker compose --env-file .env.local -f compose.local.yaml up -d --no-build --wait
```

Für reproduzierbare Updates den Commit-Tag statt `latest` verwenden. Bestehende Instanz stoppen, ein neues Backup erzeugen, dann explizit migrieren und starten. `init` überschreibt keine vorhandenen Daten oder Sicherungen.

## Kontrollerinnerungen

Der vorhandene Container erzeugt automatisch gespeicherte Hinweise; kein zusätzlicher Dienst nötig. `DRK_REMINDER_DAYS=60,30,7` legt Vorwarnzeiten fest, `DRK_DEVICE_REMINDERS=false` schaltet den Hintergrundlauf ab. Die Oberfläche zeigt den letzten erfolgreichen Lauf. E-Mail-Versand ist optional per SMTP-Overlay verfügbar, siehe [SMTP-Anleitung](smtp-erinnerungen.md). Bestehende Datenbanken vor Update sichern und ausdrücklich auf Schema 32 migrieren; Volumes erhalten.

## Organisatorische Verwaltung

Betreiber, Betriebsstätten, Lagerorte und Benutzer lassen sich jetzt mit einem eigenen Administratorkonto über die Oberfläche einrichten. Für eine leere Installation ist `seed-demo` nicht erforderlich. Konto ohne automatische Fachrechte ausdrücklich lokal anlegen, siehe [Verwaltung](administration.md). Fachrechte je Betriebsstätte anschließend bewusst vergeben.

Aktueller Schemastand 32: [Sicherung und ausdrückliche Migration](einsatzfreigabe.md#upgrade-auf-schema-32). Neue Installationen werden direkt initialisiert.
