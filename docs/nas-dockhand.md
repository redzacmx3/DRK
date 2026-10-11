# Auf Ugreen DXP2800 mit Dockhand testen

Diese Konfiguration baut das Projekt direkt aus dem GitHub-Repository und startet es auf dem NAS. Ein fertiges Containerimage ist derzeit nicht in einer Registry veröffentlicht. Den ganzen Checkout herunterladen, nicht nur die Compose-Datei: Dockerfile, Go-Code und `vendor/` werden zum Build benötigt. Keine Go-Installation auf dem NAS nötig. Voraussetzung ist Docker mit aktuellem Compose v2 (mindestens 2.20.2).

Der Aufbau ist:

```text
Browser → HTTPS inventar.deine-domain.de → Traefik auf VPS
        → Tailscale → HTTPS NAS-Tailscale-IP:8443 → Inventory-Container
```

Beide Verbindungen verwenden HTTPS. Das Programm vertraut keinen Forwarded-Headern; Traefik verbindet sich selbst per TLS mit dem Backend. Tailscale muss auf NAS und VPS laufen und die Verbindung erlauben. Der Traefik-Container muss die NAS-Tailscale-IP erreichen können; allein eine funktionierende Verbindung vom VPS-Host bestätigt das noch nicht. Zugriff über Tailscale-Regeln auf den gewünschten VPS beschränken. Der Browser-Origin muss genau der konfigurierten HTTPS-Adresse entsprechen.

## 1. Projekt von GitHub holen

Repository: <https://github.com/redzacmx3/DRK> · Branch `main`.

In Dockhand den Stack **`drk-nas`** als Git-basierten Stack für dieses Repository anlegen und **`compose.nas.yaml`** als Compose-Datei auswählen, sofern deine Dockhand-Version Git-Stacks unterstützt. Sonst auf der GitHub-Seite **Code → Download ZIP**, auf dem NAS entpacken und den vollständigen Projektordner als lokalen Stack verwenden. Der Build-Kontext `.` muss dieser Projektordner auf dem NAS sein. Ein einzelner ins Webformular kopierter Compose-Text hat den Quellcode nicht automatisch als Build-Kontext.

Alternativ per SSH auf dem NAS:

```bash
git clone https://github.com/redzacmx3/DRK.git
cd DRK
cp deploy/nas.env.example .env.nas
```

Beim ZIP-Download entsprechend in den entpackten Ordner wechseln und die Beispieldatei kopieren. Einen vorhandenen Checkout oder eine vorhandene `.env.nas` nicht überschreiben.

## 2. Drei Werte anpassen

In `.env.nas` beziehungsweise den **Stack-Umgebungsvariablen in Dockhand** setzen:

| Variable | Beispiel | Bedeutung |
|---|---|---|
| `DRK_ORIGIN` | `https://inventar.deine-domain.de` | Exakte Browseradresse, ohne abschließenden Slash oder Pfad |
| `DRK_BIND_IP` | `100.x.y.z` | Tailscale-IPv4 des NAS selbst; kein Platzhalter und nicht `0.0.0.0` |
| `DRK_TLS_DIR` | `/volume1/docker/drk/tls` | Absoluter Zertifikatsordner auf dem NAS; an deinen tatsächlichen Speicherpfad anpassen |
| `DRK_HTTPS_PORT` | `8443` | Optionaler veröffentlichter NAS-Port, Standard 8443 |

Der Zertifikatsordner ist ein Pfad auf dem **Docker-Host**, nicht im Dockhand-Container. `.env.nas` wird bei CLI-Aufrufen ausdrücklich über `--env-file` geladen. In Dockhand die Variablen explizit hinterlegen, falls dessen Stack-Verwaltung die Datei nicht lädt. Die Werte enthalten keine Passwörter.

Das feste Docker-Volume **`drk_nas_inventory_data`** enthält Datenbank und Datenbankanhänge. Es bleibt bei Containerwechseln und Neubuilds erhalten. Optional kann `DRK_DATA_VOLUME` vor der ersten Einrichtung einen anderen festen Namen setzen; späteres Ändern würde eine andere Datenbank öffnen. Nicht `down -v` verwenden.

## 3. NAS-Backend-Zertifikat erzeugen

Auf dem NAS im Projektordner ausführen; den Pfad passend zu `DRK_TLS_DIR` wählen:

```bash
sudo bash scripts/prepare-nas-tls.sh /volume1/docker/drk/tls
```

Das Skript erstellt ein selbst signiertes Zertifikat für **`drk-nas.internal`**, `localhost` und `127.0.0.1`. Es ist 365 Tage gültig. Die Dateien erhalten die erforderlichen Rechte für Container-UID 1000. Vorhandene Zertifikate/Schlüssel werden niemals ersetzt; bei vorhandenen Dateien müssen Namen, Gültigkeit und Leserechte passen. Der gemountete Ordner muss für UID 1000 zugänglich sein.

Nur **`cert.pem`** zum VPS kopieren. Der private **`key.pem`** bleibt auf dem NAS. Traefik vertraut genau diesem Backend-Zertifikat. Vor Ablauf bewusst ein neues Zertifikatspaar in einem neuen Ordner erzeugen und den Traefik-Vertrauensanker sowie den NAS-Mount koordiniert aktualisieren.

## 4. Bauen und einmalig initialisieren

Für den ersten Start eine NAS-SSH-Sitzung im Projektordner verwenden. Dockhand anschließend denselben Stack/dasselbe Volume verwalten lassen; keinen zweiten gleichzeitigen Dienst auf demselben Port starten.

```bash
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml build inventory
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml run --rm maintenance migrate --db /data/inventory.db
# Nur für eine neue Testinstallation mit synthetischen Daten:
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml run --rm maintenance seed-demo --db /data/inventory.db
```

`maintenance` ist ein einmaliger CLI-Dienst mit Profil und startet nicht automatisch im normalen Stack. Vor Migration einer bestehenden Datenbank zuerst ein neues Backup anlegen, siehe unten.

Es gibt kein Standardkonto. Für Material/Verbrauchsmaterial einen Materialwart anlegen; das Passwort wird verdeckt eingegeben und nur per stdin übergeben:

```bash
read -r -s -p 'Passwort für test-material (mindestens 12 Zeichen): ' DRK_TEST_PASSWORD
printf '\n'
printf '%s\n' "$DRK_TEST_PASSWORD" | docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml run --rm -T maintenance create-user --db /data/inventory.db --username test-material --site 00000000-0000-4000-8000-000000000002 --role material_manager
unset DRK_TEST_PASSWORD
```

Für Geräte einen separaten Benutzer `test-geraete` mit **`--role device_manager`** auf dieselbe Weise anlegen. Die Betriebsstätten-ID oben gehört ausschließlich zu den synthetischen Demodaten. `set-sites` ersetzt sämtliche Rollen/Zuweisungen eines Kontos und widerruft seine Sitzungen; damit lässt sich nicht einfach eine zweite Rolle ergänzen.

Dann in Dockhand deployen oder per CLI starten:

```bash
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml up -d --wait --wait-timeout 60
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml ps
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml logs --tail 50 inventory
```

Der Dienst soll **healthy** melden. Er bindet im Container an `0.0.0.0`, wird auf dem NAS aber nur an der angegebenen Tailscale-IP veröffentlicht. Die Standard-Datei `compose.yaml` bleibt eine eigene Loopback-Entwicklungsumgebung.

## 5. Traefik/Dokploy auf dem VPS anbinden

[Beispielkonfiguration](../deploy/traefik-nas.example.yaml) als **dynamische Konfiguration des Traefik-File-Providers** verwenden. Es handelt sich um einen externen HTTPS-Dienst auf dem NAS, nicht um einen auf dem VPS gestarteten DRK-Container.

Anpassen:

- Router-Host auf deine Subdomain, identisch zu `DRK_ORIGIN`.
- Backend-URL auf `https://NAS-TAILSCALE-IP:8443` beziehungsweise deinen Port.
- EntryPoint und Zertifikatsresolver auf die tatsächlich in Dokploy konfigurierten Namen. `websecure` und `letsencrypt` sind Beispiele. Ist dein Wildcard-Zertifikat bereits im TLS-Store hinterlegt, `certResolver` entfernen und `tls: {}` verwenden.
- NAS-`cert.pem` für den Traefik-Prozess lesbar im Container schreibgeschützt unter `/etc/traefik/certs/drk-nas.pem` verfügbar machen. Der Pfad in `rootCAs` bezeichnet einen Pfad **im Traefik-Container**.
- `serverName: drk-nas.internal` beibehalten: Dieser Name steht im Backend-Zertifikat und dient der TLS-Prüfung; er muss nicht per DNS erreichbar sein.

Dynamische Datei in das von Dokploy verwaltete File-Provider-Verzeichnis legen beziehungsweise über die dort verfügbare Traefik-Konfiguration einbinden. Unterstützte Pfade und UI unterscheiden sich je Dokploy-Version; die Datei wird nicht automatisch durch Deploy des NAS-Stacks auf dem VPS installiert. Bei einem neuen Zertifikats-Mount muss Traefik gegebenenfalls neu erstellt werden. Bestehende Traefik-Konfiguration ergänzen, nicht ersetzen. **Keine deaktivierte Zertifikatsprüfung und kein `insecureSkipVerify`.**

Die Subdomain zeigt per DNS auf den VPS. Ein Wildcard-Zertifikat deckt den Browserzugriff ab; das NAS-Backend verwendet das separate Zertifikat aus Schritt 3. Wildcard-Ausstellung benötigt üblicherweise DNS-Challenge, sofern dein bestehendes Zertifikat nicht schon genutzt wird.

Vom VPS mit dem kopierten Backend-Zertifikat prüfen:

```bash
curl --cacert /PFAD/ZUM/NAS-cert.pem --resolve drk-nas.internal:8443:NAS-TAILSCALE-IP https://drk-nas.internal:8443/health/ready
```

Bei anderem Port beide Portangaben ändern. Dieselbe Route muss auch aus dem Traefik-Netzwerk funktionieren. Danach die öffentliche Subdomain öffnen und anmelden. Direkter HTTP-Zugriff wird nicht unterstützt. Direkter Zugriff über eine andere HTTPS-Adresse erlaubt wegen der Origin-Prüfung ebenfalls keine Anmeldung.

## 6. Test und spätere Updates

Mit `test-material` Material/Bestandsbuchungen und den Verbrauchsmaterial-CSV-Import testen; mit `test-geraete` Geräteakten und Geräte-CSV. Zunächst nur synthetische Daten. Die Excel-Listen können schon unabhängig vom laufenden Programm benutzt werden.

Bei Updates den Checkout von GitHub aktualisieren und das Image neu bauen. Datenvolume und Zertifikate behalten. Dienst stoppen, Backup mit neuem Dateinamen erstellen, anschließend migrieren und neu starten:

```bash
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml build inventory
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml stop inventory
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml run --rm maintenance backup --db /data/inventory.db --out /data/before-update-EINDEUTIGER-NAME.db
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml run --rm maintenance migrate --db /data/inventory.db
docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml up -d --wait --wait-timeout 60
```

Den Snapshot zusätzlich vom Volume auf einen getrennten Sicherungsort kopieren. Kein bestehendes Backup überschreiben. Eine laufende SQLite-Datei nicht einfach kopieren. Automatische Dockhand-Updates vorerst deaktiviert lassen: Migration und Backup sind bewusst manuelle Schritte.

## Grenzen des aktuellen Tests

Go-Tests und ein Docker-Test mit echtem Traefik 3.6 prüfen die ausdrücklich aktivierte Netzwerkbindung, TLS auf beiden Verbindungen, exakte öffentliche Origin, sichere Anmeldung/Buchung und die persistente NAS-Compose-Konfiguration. Ein entfernter Backend-Vertrauensanker führt im Test korrekt zum Verbindungsfehler; Zertifikatsprüfung bleibt eingeschaltet. Dein physisches NAS, deine Tailscale-Regeln und deine Dokploy-/Traefik-Installation sind hier nicht erreichbar und müssen bei dir geprüft werden. Login-Limits gelten derzeit für die Backend-Verbindungsadresse, hinter Traefik also gemeinsam für dessen Nutzer. Produktive Freigabe, vollständige Rollen-/Qualifikationsprüfung und Restore-Abnahme bleiben offen.

## Kontrollerinnerungen

Der vorhandene Container erzeugt automatisch gespeicherte Hinweise; kein zusätzlicher Dienst nötig. `DRK_REMINDER_DAYS=60,30,7` legt Vorwarnzeiten fest, `DRK_DEVICE_REMINDERS=false` schaltet den Hintergrundlauf ab. Die Oberfläche zeigt den letzten erfolgreichen Lauf. E-Mail-Versand ist optional per SMTP-Overlay verfügbar, siehe [SMTP-Anleitung](smtp-erinnerungen.md). Bestehende Datenbanken vor Update sichern und ausdrücklich auf Schema 27 migrieren; Volumes erhalten.

## Organisatorische Verwaltung

Betreiber, Betriebsstätten, Lagerorte und Benutzer lassen sich jetzt mit einem eigenen Administratorkonto über die Oberfläche einrichten. Für eine leere Installation ist `seed-demo` nicht erforderlich. Konto ohne automatische Fachrechte ausdrücklich lokal anlegen, siehe [Verwaltung](administration.md). Fachrechte je Betriebsstätte anschließend bewusst vergeben.
