# Fertiges Image und GitHub-Releases

Die Anwendung wird als Image unter `ghcr.io/redzacmx3/drk` gebaut. Für die Ugreen DXP2800 ist `linux/amd64` vorgesehen. Es muss auf dem NAS nicht kompiliert werden.

## Veröffentlichung

Push auf `main` startet Tests, Vet und Image-Build. Nach erfolgreichem Lauf stehen `latest` und der vollständige Commit-Tag bereit. Ein Versions-Tag wie `v0.1.0-rc.1` startet denselben geprüften Image-Build mit einem eigenen Versions-Tag. Erst nach erfolgreicher Image-Veröffentlichung erstellt der Workflow ein GitHub-Release mit Installationsdateien und Image-Digest. Tags mit einem Vorabversionszusatz werden als Prerelease veröffentlicht. Ein Release verschiebt `latest` nicht; dieses kennzeichnet weiterhin den Entwicklungsstand auf `main`.

Der Erfolg muss unter **Actions → Container image** bestätigt werden. Ein gepushter Git-Tag ist noch keine erfolgreiche Image-Veröffentlichung. Im Repository müssen Actions und die Paket-Schreibberechtigung erlaubt sein. Für einen Abruf ohne Anmeldung muss das Container-Package `drk` auf GitHub unter **Packages → Package settings → Change visibility** öffentlich sein. Eine öffentliche Repository-Seite allein macht das Package nicht automatisch öffentlich.

## Installation in Dockhand

1. Unter **Releases** die Datei `compose.release.yaml` einer erfolgreich gebauten Version herunterladen und als Stack importieren. Diese Datei enthält bereits den konkreten Image-Tag und keine Build-Anweisung.
2. Die Stack-Variablen anhand von `env.release.example` setzen. NAS-IP, Port und `DRK_ORIGIN` müssen der tatsächlich im Browser verwendeten Adresse entsprechen.
3. Stack starten und den [Web-Assistenten](ersteinrichtung.md) öffnen. Den einmaligen Einrichtungscode in der Konsole des Containers mit `cat /data/setup-code.txt` abrufen. Administrator und Funktionsumfang werden im Browser eingerichtet.

Alternativ mit Docker Compose:

```sh
docker compose --env-file .env -f compose.release.yaml pull
docker compose --env-file .env -f compose.release.yaml up -d --wait
```

Die Compose-Datei direkt aus dem Branch verwendet standardmäßig `latest`; für reproduzierbare Tests `DRK_IMAGE` auf einen bestätigten Versions-Tag oder Digest setzen. Bei einem privaten Package ist vorher eine berechtigte Registry-Anmeldung in Dockhand beziehungsweise Docker erforderlich; Zugangsdaten nicht in die Compose-Datei oder ins Repository schreiben.

## HTTPS

Der lokale HTTP-Test benötigt keine Zertifikate. Für HTTPS dieselbe Datei verwenden und `DRK_HTTP=false`, die genaue HTTPS-Adresse als `DRK_ORIGIN`, `DRK_TLS_DIR`, `DRK_HEALTH_URL=https://localhost:8080/health/ready` und `DRK_HEALTH_CA=/certs/cert.pem` setzen. Das Zertifikatsverzeichnis muss `cert.pem` und `key.pem` enthalten. Das mitgelieferte Skript `prepare-nas-tls.sh` hilft bei der Backend-Vorbereitung; für Tailscale/Traefik gilt die [NAS-Anleitung](nas-dockhand.md).

## Vorhandene Daten aktualisieren

Den tatsächlichen bisherigen Volume-Namen als `DRK_DATA_VOLUME` übernehmen. Der Release-Stack hat einen eigenen Standardnamen; ein unbeabsichtigt neues Volume würde eine leere Installation anzeigen. Alte Volumes nicht löschen.

Dienst stoppen, ein neues konsistentes Backup mit dem bisherigen Image erstellen, dann den bestätigten neuen Image-Tag einstellen und herunterladen. Anschließend ausdrücklich migrieren und wieder starten:

```sh
docker compose --env-file .env -f compose.release.yaml run --rm init migrate --db /data/inventory.db
docker compose --env-file .env -f compose.release.yaml up -d --wait
```

`init` migriert vorhandene Datenbanken nicht automatisch. Die Rückkehr zu einem älteren Image nach einer Migration setzt eine passende geprüfte Sicherung voraus. [Sicherung und Wiederherstellung](verschluesselte-backups.md).

## Navigation ab v0.1.0-rc.2

Jeder Bereich wird einzeln geöffnet. Auf großen Bildschirmen steht links eine Menüleiste; auf kleinen Bildschirmen ist die Bereichsauswahl horizontal scrollbar. Geräte und Equipment öffnen sich nach der Anmeldung zuerst. Deaktivierte Module erscheinen nicht im Menü; Verwaltung ist nur für Administratoren sichtbar. Ein Bereichswechsel erhält Formulareingaben und unbestätigte Vorgänge. Die Barcode-Übergabe öffnet automatisch die Materialbuchung beziehungsweise die passende Packliste.

Für das Update auf diese Oberfläche das Image auf `ghcr.io/redzacmx3/drk:v0.1.0-rc.2` setzen, sobald das entsprechende Release erfolgreich gebaut wurde. Datenvolume und vorhandene Stack-Einstellungen beibehalten. Die Datenbank bleibt auf Schema 34; für diese reine Oberflächenänderung ist keine zusätzliche Migration erforderlich. Anschließend die Browserseite neu laden.
