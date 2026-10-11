# E-Mail-Erinnerungen einrichten

SMTP ist optional. Die normalen Compose-Dateien verschicken ohne zusätzliche Konfiguration keine E-Mails. Der vorhandene Container übernimmt den Versand; kein weiterer Dienst nötig. Dies ist für Kontrollerinnerungen bestimmt und ersetzt keine Behördenmeldung.

## Dateien vorbereiten

Auf dem NAS ein geschütztes Verzeichnis anlegen. Darin `config.json` und, falls Anmeldung erforderlich, eine separate Datei `password` mit dem SMTP-Passwort ablegen. Dateien für Container-UID 1000 lesbar machen, Zugangsdaten nur für diese Identität beziehungsweise den Administrator lesbar halten. Keine Zugangsdaten in Git, Chat, Compose-Variablen oder Kommandozeilen schreiben.

Beispiel `config.json` ohne Geheimnisse:

```json
{
  "host": "smtp.example.org",
  "port": 587,
  "mode": "starttls",
  "from": "inventar@example.org",
  "username": "inventar@example.org",
  "password_file": "/run/smtp/password",
  "ca_file": "",
  "recipients": [
    {
      "site_id": "00000000-0000-4000-8000-000000000002",
      "address": "geraeteverantwortlich@example.org"
    }
  ]
}
```

SMTP-Host, Absender und Empfänger durch echte Angaben ersetzen; Betriebsstätten-ID aus der Anwendung verwenden. Beispiel-ID gehört ausschließlich den synthetischen Demo-Daten. Ohne SMTP-Anmeldung `username` und `password_file` gemeinsam leer lassen. Implizites TLS: `mode: "tls"`, üblicherweise Port 465. Beide Varianten prüfen Zertifikatskette und Hostname; Klartext oder deaktivierte Prüfung sind nicht vorgesehen. Für eine interne CA deren PEM-Datei in dasselbe Verzeichnis legen und `ca_file: "/run/smtp/ca.pem"` setzen. Die System-CA-Liste bleibt zusätzlich verfügbar.

Höchstens 100 Empfängerbindungen, höchstens 10.000 Zielzustellungen pro Vorbereitungslauf; fehlerhafte oder unbekannte Betriebsstätten verhindern den Start. Adressen ohne Anzeigenamen verwenden. Die Empfängerbindungen gelten unabhängig von Benutzerkonten; nach Personalwechsel muss der Betreiber die Konfiguration aktualisieren. Änderungen an Dateien erfordern einen Neustart.

## In Compose aktivieren

In Dockhand das SMTP-Overlay zusätzlich zur gewählten Basisdatei laden und `DRK_SMTP_DIR` auf das absolute Verzeichnis setzen. Zum Beispiel für die lokale HTTP/HTTPS-Variante:

```sh
docker compose --env-file .env.local -f compose.local.yaml -f compose.smtp.yaml up -d --wait
```

`.env.local` ergänzen:

```dotenv
DRK_SMTP_DIR=/volume1/docker/drk/smtp
```

Für NAS mit Traefik `compose.nas.yaml` als Basis verwenden. Vor dem Update auf Schema 22 den laufenden Dienst stoppen, ein neues konsistentes Backup erzeugen und explizit migrieren; das bestehende Datenvolume erhalten. Das SMTP-Overlay erst nach erfolgreicher Migration zuschalten. `DRK_DEVICE_REMINDERS=true` ist für automatischen SMTP-Betrieb erforderlich.

Beim ersten Aktivieren werden die aktuell gültigen gespeicherten Erinnerungen für konfigurierte Betriebsstätten vorbereitet. Der Versand prüft jede Minute, verarbeitet bis zu 20 Zustellungen pro Lauf und endet spätestens nach 60 Sekunden; einzelne SMTP-Verbindungen dauern höchstens 30 Sekunden. Nach einem Ausfall werden überholte Warnschwellen übersprungen. Entfernte Empfänger erhalten keine weiteren vorbereiteten Hinweise; bereits angenommene Mails lassen sich nicht zurückholen. Wieder hinzugefügte Empfänger lösen keine erneute Zustellung bereits abgeschlossener/verworfener Einträge aus.

Der Menübereich „E-Mail-Versandstatus“ zeigt die letzten 200 Zustellungen mit Kennungen. „Vom SMTP-Server angenommen“ bedeutet, dass der Server die Nachricht angenommen hat; Bounces, Spamfilter und Postfachzustellung müssen separat geprüft werden. TLS-Vertrauen und SMTP-Zugang mit einem kontrollierten Testempfänger prüfen, bevor weitere Empfänger freigeschaltet werden. SMTP-Testbetrieb in der Entwicklung verwendet ausschließlich lokale Testserver.

## Fehler klären

Temporäre Fehler werden automatisch bis maximal fünf Versuchen wiederholt. Dauerhafte Fehler nach Korrektur der Konfiguration ausdrücklich erneut freigeben:

```sh
docker compose -f compose.local.yaml -f compose.smtp.yaml exec -T inventory /app/inventory retry-reminder-mail --db /data/inventory.db --delivery ZUSTELLUNGS_ID
```

Bei „Annahme unklar“ zunächst die Message-ID beim Mailadministrator prüfen. Erst anschließend bei bewusst akzeptiertem Doppelmail-Risiko denselben Befehl um `--accept-duplicate-risk` ergänzen. Eine stabile Message-ID unterstützt die Prüfung, garantiert aber keine serverseitige Deduplizierung. Angenommene Zustellungen können nicht erneut freigegeben werden. Die lokale CLI protokolliert die manuelle Freigabe; der nächste Worker-Lauf prüft nochmals Aktualität und Empfänger.

Ein ausdrücklicher Einmallauf ist über `deliver-reminders --db /data/inventory.db --smtp-config /run/smtp/config.json` möglich. Ohne SMTP-Konfiguration lehnt dieser Befehl ab. Ausschalten: das Overlay weglassen und den Stack neu starten. Dabei bleiben Zustellprotokolle erhalten; es werden keine weiteren Mails gesendet. Externe Konten und SMTP-Konfiguration müssen getrennt von der Datenbank gesichert werden.
