# Inventarisierung und optionale Module

Die Inventarisierung ist immer aktiv: Geräte und Equipment mit ihren Grunddaten, Inventarnummern und Lagerorten sowie Materialbestand, CSV-Erfassung, Excel-Vorlagen, Scannerauflösung und Bestandsbuchungen. Die organisatorische Verwaltung, Anmeldung, Rollen und Sicherung bleiben ebenfalls verfügbar. Für die erste Erfassung ist keine vollständige Geräteakte erforderlich.

Weitere Funktionen werden je Installation beim Start ausgewählt. Die Einstellung gilt für alle Benutzer dieser Instanz; ihre fachlichen Rechte und Betriebsstätten bleiben separat maßgeblich. Es handelt sich um ein gemeinsames Docker-Image mit einer gemeinsamen Datenbank. Zusätzliche Container sind für die Module nicht notwendig.

## Auswahl in Docker Compose oder Dockhand

In den Stack-Variablen von Dockhand oder der zur Compose-Datei gehörenden `.env` setzen:

```dotenv
# Nur Inventarisierung:
DRK_MODULES=inventory
```

Für Inventarisierung mit zusätzlichen Geräteakten:

```dotenv
DRK_MODULES=inventory,devices
```

Für den bisherigen vollständigen Umfang:

```dotenv
DRK_MODULES=all
```

`compose.yaml`, `compose.local.yaml` und `compose.nas.yaml` reichen die Auswahl an den Dienst weiter. Ohne Einstellung oder bei einem leeren Compose-Wert wird `all` verwendet, damit bestehende Installationen ihren bisherigen Umfang behalten. Anschließend den Container mit der neuen Einstellung neu erstellen, zum Beispiel:

```sh
docker compose -f compose.local.yaml up -d --force-recreate inventory
# Mit eigener Variablendatei:
docker compose --env-file .env.local -f compose.local.yaml up -d --force-recreate inventory
```

HTTP und HTTPS werden weiterhin über die [Transportkonfiguration](schnellstart-docker.md) ausgewählt. Die Modulauswahl funktioniert in beiden Modi.

## Funktionsgruppen und Abhängigkeiten

| Kennung | Inhalt | Erforderliche weitere Module |
|---|---|---|
| `inventory` | Geräte-/Equipment-Grundbestand und Materialinventarisierung | Immer aktiv |
| `devices` | Zusätzliche Geräteakten mit Inbetriebnahme, Kontrollen, Einweisungen, Wartung, Vorfällen und Außerbetriebnahme | — |
| `documents` | Anhänge mit Quarantäne, Scan und Dokumentfreigabe | `devices` |
| `compliance` | Produktpflichten, Qualifikation und fachliche Nachweis-/Erfüllungsabnahmen | `devices`, `documents` |
| `reprocessing` | Aufbereitungsprozesse und einzelne Aufbereitungszyklen | `compliance` |
| `packs` | Sollpacklisten und physische Packlistenprüfungen | — |
| `recalls` | Material-/Chargensperren und Rückrufe zu Material oder Geräten/Equipment | — |
| `reminders` | Kontrollerinnerungen und optionaler SMTP-Versand | `devices` |
| `release` | Personenbezogene Einsatzfreigabe von Packlisten | `packs`, `devices`, `documents`, `compliance`, `reprocessing`, `recalls` |

Alle Abhängigkeiten müssen ausdrücklich in der Auswahl enthalten sein. Beispiel für Geräteakten mit Dokumenten und Erinnerungen:

```dotenv
DRK_MODULES=inventory,devices,documents,reminders
```

Rückrufe sind auch bei reiner Inventarisierung separat zuschaltbar: `DRK_MODULES=inventory,recalls`. Geräte-/Equipment-Rückrufe benötigen dabei keine zusätzlichen Geräteakten.

`inventory` wird auch dann beibehalten, wenn es in einer gültigen Liste nicht ausdrücklich steht. `all` ist ausschließlich als alleiniger Wert zulässig. Unbekannte Kennungen, doppelte Einträge, eine Mischung aus `all` und anderen Kennungen oder fehlende Abhängigkeiten verhindern den Start. Es werden keine weiteren Module automatisch zugeschaltet.

Beim direkten Programmstart ist auch ein Flag möglich; es hat Vorrang vor der Umgebungsvariable:

```sh
bin/inventory serve --db var/inventory.db --modules inventory,devices
```

Ein ausdrücklich leerer `DRK_MODULES`-Wert oder `--modules ''` beim direkten Programmstart ist ungültig. Ohne Umgebungsvariable und ohne Flag gilt `all`.

## Abschalten und spätere Erweiterung

Abgeschaltete Funktionsgruppen werden in der Oberfläche ausgeblendet. Die API weist zugehörige Schreibaktionen ab; ein direkter API-Aufruf kann das Abschalten nicht umgehen. Grunddaten von Geräten und Equipment bleiben als Teil der Inventarisierung sichtbar und erfassbar.

Vorhandene Daten werden weder gelöscht noch umgeschrieben. Bereichsgeschützte historische Lesezugriffe und Exporte bleiben verfügbar, soweit die bisherigen Rechte dies erlauben. Bereits bestehende Sperren, ungeklärte Nachweise oder Aufbereitungspflichten werden durch Abschalten nicht erfüllt und verhindern weiterhin unberechtigte positive Bewertungen. Auch eine fehlende Malwareprüfung kann nicht durch Abschalten des Dokumentmoduls umgangen werden.

Jede tatsächliche Änderung der Modulauswahl wird als neue unveränderliche Konfigurationsrevision gespeichert. Ein Neustart mit derselben Auswahl erzeugt keine neue Revision. Eine geänderte Auswahl macht frühere Einsatzfreigaben für die aktuelle Verwendung ungültig; auch Abschalten mit anschließendem Wiedereinschalten stellt diese nicht automatisch wieder her. Die zuständige Person muss erneut prüfen und ausdrücklich freigeben. Die früheren Nachweise bleiben als Historie erhalten.

Für die erste Inventarisierung deshalb `inventory` wählen und später `devices` ergänzen, wenn Geräteakten benötigt werden. Weitere Funktionsgruppen lassen sich danach mit ihren Abhängigkeiten ergänzen.

## Erinnerungsjobs und SMTP

Ohne Modul `reminders` laufen weder die automatische Erinnerungsberechnung noch der Mailworker, auch wenn `DRK_DEVICE_REMINDERS` auf seinem bisherigen Standard `true` steht. Eine ausdrücklich konfigurierte SMTP-Datei ist in diesem Modus ein Startfehler: das SMTP-Overlay weglassen beziehungsweise `DRK_SMTP_CONFIG` entfernen.

Bei aktivem Modul bestimmt `DRK_DEVICE_REMINDERS=false` weiterhin, ob der automatische Erinnerungsjob läuft. SMTP-Versand setzt sowohl das Modul als auch den eingeschalteten Job voraus. Die [SMTP-Konfiguration](smtp-erinnerungen.md) bleibt optional.

Die CLI-Befehle `remind-devices`, `deliver-reminders` und `retry-reminder-mail` verlangen ebenfalls das Erinnerungsmodul und eine Auswahl, die mit der zuletzt gespeicherten Dienstkonfiguration übereinstimmt. Sie ändern die Modulauswahl nicht. Bei einem direkten Aufruf dieselbe Umgebungsvariable oder `--modules` wie beim Dienst verwenden. Die lokalen `init`- und NAS-`maintenance`-Dienste übernehmen die Stack-Auswahl für solche Aufrufe. Sicherung, Migration und Kontenverwaltung bleiben unabhängig davon ausführbar, auch wenn eine fehlerhafte Modulauswahl den Webdienst blockiert.

## Bestehende Instanz auf Schema 33 aktualisieren

Die Konfigurationshistorie benötigt Schema 33. Für eine vorhandene Instanz erst ein neues konsistentes Backup erstellen, anschließend ausdrücklich migrieren. Das Datenvolume und vorhandene Sicherungen erhalten. Beispiel für den lokalen Compose-Stack im Projektordner, gegebenenfalls jeweils `--env-file .env.local` ergänzen:

```sh
docker compose -f compose.local.yaml build
docker compose -f compose.local.yaml stop inventory
docker compose -f compose.local.yaml run --rm init backup \
  --db /data/inventory.db --out /data/before-modules-33.db
docker compose -f compose.local.yaml run --rm init migrate \
  --db /data/inventory.db
docker compose -f compose.local.yaml up -d --wait
```

Das Backupziel muss neu sein; bei einem vorhandenen Ziel einen anderen Namen wählen. Für den NAS-Stack die entsprechenden Schritte mit `maintenance` nach der [NAS-Anleitung](nas-dockhand.md) ausführen. Eine neue Datenbank wird direkt auf Schema 33 initialisiert; `init` aktualisiert bestehende Datenbanken weiterhin nicht automatisch. Bei der ersten Auswahl ohne vorherige Konfigurationshistorie wird der bisherige volle Umfang zugrunde gelegt.

Der erste Dienststart mit Schema 33 speichert die erste Konfigurationsrevision. Vorher erteilte Einsatzfreigaben müssen danach erneut ausdrücklich geprüft und freigegeben werden, auch bei `DRK_MODULES=all`. Die bisherigen Entscheidungen und Nachweise bleiben unverändert in der Historie erhalten.
