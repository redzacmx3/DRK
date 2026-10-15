# Einfacher Docker-Test mit HTTP oder HTTPS

`compose.local.yaml` startet eine lokale Testinstanz ohne Zertifikatsvorbereitung. Ein Image enthält Programm und Oberfläche; SQLite liegt im separaten persistenten Volume. Die Ugreen DXP2800 nutzt die unterstützte Plattform `linux/amd64`.

Die erste Einrichtung erfolgt im Browser: Administratorkonto, Organisation und benötigte Module auswählen. Inventarisierung ist voreingestellt; optionale Geräteakten später in der Verwaltung zuschalten. `DRK_MODULES` bleibt als Startvorgabe verfügbar und überschreibt Web-Änderungen bei unverändertem Neustart nicht. [Module und Abhängigkeiten](module.md).

## Sofort aus dem GitHub-Download testen

Im entpackten Projektordner:

```sh
docker compose -f compose.local.yaml up -d --build --wait
```

`http://localhost:8080` öffnen. Bei neuer Datenbank erscheint die Ersteinrichtung. Den privaten Einrichtungscode über die Konsole des Containers auslesen:

```sh
docker compose -f compose.local.yaml exec -T inventory cat /data/setup-code.txt
```

In Dockhand dazu die Konsole des `inventory`-Containers öffnen und `cat /data/setup-code.txt` ausführen. Im Browser Code eingeben, Prüfhinweise lesen, Administratorkonto mit Passwort, Betreiber, Betriebsstätte und Lagerort anlegen, Fachrechte ausdrücklich auswählen und Module festlegen. Anschließend normal anmelden. Ein separater `create-admin`-Befehl ist für die neue Instanz nicht nötig. [Schritte und Sicherheitsgrenzen](ersteinrichtung.md).

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

Für die einfache Installation die `compose.release.yaml` aus einem erfolgreich veröffentlichten [GitHub-Release](https://github.com/redzacmx3/DRK/releases) verwenden. Sie enthält keinen lokalen Build und ist auf die jeweilige Version voreingestellt. [Veröffentlichung, Dockhand und Updates](image-release.md).

Der GitHub-Actions-Workflow `.github/workflows/container.yml` baut nach Push auf `main` ein amd64-Image für `ghcr.io/redzacmx3/drk:latest` und einen unveränderlichen Commit-Tag. Ein erfolgreicher Workflowlauf und die passende Package-Sichtbarkeit sind Voraussetzung; das Vorhandensein der Workflowdatei bestätigt noch keine Veröffentlichung. Für Downloads ohne Anmeldung muss das Package auf GitHub öffentlich sein; sonst vorab `docker login ghcr.io` mit einem berechtigten Konto.

Nach bestätigter Veröffentlichung `DRK_IMAGE=ghcr.io/redzacmx3/drk:latest` in den Stack-Variablen setzen. Es genügt dann die Compose-Datei; der Quellcode wird nicht benötigt:

```sh
docker compose --env-file .env.local -f compose.local.yaml pull
docker compose --env-file .env.local -f compose.local.yaml up -d --no-build --wait
```

Für reproduzierbare Updates den Commit-Tag statt `latest` verwenden. Bestehende Instanz stoppen, ein neues Backup erzeugen, dann explizit migrieren und starten. `init` überschreibt keine vorhandenen Daten oder Sicherungen.

## Kontrollerinnerungen

Bei aktivem Modul `reminders` erzeugt der vorhandene Container automatisch gespeicherte Hinweise; kein zusätzlicher Dienst nötig. `DRK_REMINDER_DAYS=60,30,7` legt Vorwarnzeiten fest, `DRK_DEVICE_REMINDERS=false` schaltet den Hintergrundlauf ab. Die Oberfläche zeigt den letzten erfolgreichen Lauf. E-Mail-Versand ist optional per SMTP-Overlay verfügbar, siehe [SMTP-Anleitung](smtp-erinnerungen.md). Ein abgeschaltetes Modul erhält Warteschlange und Historie; erneute Aktivierung weckt konfigurierte Worker innerhalb einer Minute. Während der Ersteinrichtung bleiben beide Hintergrundjobs still.

## Organisatorische Verwaltung

Betreiber, Betriebsstätten, Lagerorte, Benutzer und Module lassen sich mit einem Administratorkonto über die Oberfläche verwalten. Die neue Installation wird im Browser eingerichtet; `seed-demo` ist nicht erforderlich. Fachrechte je Betriebsstätte bewusst auswählen und später vergeben, siehe [Verwaltung](administration.md). Vorhandene Daten oder Konten öffnen den Assistenten nicht; für die ausdrückliche lokale Wiederherstellung bleibt die Konten-CLI verfügbar.

Aktueller Schemastand 34: [Sicherung und ausdrückliche Migration](module.md#bestehende-instanz-auf-schema-34-aktualisieren). Neue Installationen werden direkt initialisiert. Datenvolumes, TLS-Schlüssel und Sicherungen erhalten.
