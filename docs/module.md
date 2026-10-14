# Inventarisierung und optionale Module

Die Inventarisierung ist immer aktiv: Geräte und Equipment mit ihren Grunddaten, Inventarnummern und Lagerorten sowie Materialbestand, CSV-Erfassung, Excel-Vorlagen, Scannerauflösung und Bestandsbuchungen. Die organisatorische Verwaltung, Anmeldung, Rollen und Sicherung bleiben ebenfalls verfügbar. Für die erste Erfassung ist keine vollständige Geräteakte erforderlich.

Weitere Funktionen werden bei der [ersten Einrichtung](ersteinrichtung.md) im Browser ausgewählt und später über die Verwaltungsoberfläche geändert. Die Einstellung gilt für alle Benutzer dieser Instanz; ihre fachlichen Rechte und Betriebsstätten bleiben separat maßgeblich. Es handelt sich um ein gemeinsames Docker-Image mit einer gemeinsamen Datenbank. Zusätzliche Container sind für die Module nicht notwendig.

## Auswahl in der Verwaltung

Mit einem Administratorkonto anmelden und die Modulauswahl in der Verwaltung öffnen. Dort die benötigten Funktionsgruppen auswählen, ihre Abhängigkeiten beachten und die Änderung begründen. Inventarisierung kann nicht abgeschaltet werden. Die App prüft die geladene Konfigurationsrevision, Rechte, Abhängigkeiten und Wiederholungen; nach einem Konflikt die aktuelle Auswahl neu laden und abgleichen.

Änderungen gelten unmittelbar für die Oberfläche und API-Schreibzugriffe. Andere bereits geöffnete Tabs aktualisieren ihre Auswahl bei der nächsten Konfigurationsprüfung; serverseitig sind abgeschaltete Schreibaktionen bereits gesperrt. Ein Containerneustart ist dafür nicht notwendig. Die letzten 100 Konfigurationsrevisionen werden mit Herkunft, Zeitpunkt, Akteur und Begründung angezeigt; ältere Ereignisse bleiben in der Datenbank erhalten.

## Compose als Startvorgabe

`DRK_MODULES` bleibt für automatische Bereitstellung und lokale Wiederherstellung verfügbar. Der erste Dienststart übernimmt die Startvorgabe. Sobald die erste Einrichtung oder ein Administrator die Auswahl verändert hat, bewahrt ein Neustart mit derselben Startvorgabe die gespeicherte Auswahl. Auch `DRK_MODULES=all` schaltet daher nicht bei jedem Neustart die im Browser abgeschalteten Module wieder ein.

Wird der Compose-/CLI-Startwert ausdrücklich in eine andere gültige Auswahl geändert, übernimmt der nächste Dienststart diese neue Vorgabe und protokolliert eine Konfigurationsrevision. Für alltägliche Änderungen die Verwaltungsoberfläche verwenden. Eine bereits angewandte Startvorgabe unverändert zu lassen ist keine erneute Rücksetzung auf diesen Wert.

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

Jede tatsächliche Änderung der Modulauswahl wird als neue unveränderliche Konfigurationsrevision gespeichert. Ein Neustart mit derselben Startvorgabe erzeugt keine neue Revision und erhält spätere Web-Änderungen. Eine geänderte Auswahl macht frühere Einsatzfreigaben für die aktuelle Verwendung ungültig; auch Abschalten mit anschließendem Wiedereinschalten stellt diese nicht automatisch wieder her. Die zuständige Person muss erneut prüfen und ausdrücklich freigeben. Die früheren Nachweise bleiben als Historie erhalten.

Für die erste Inventarisierung deshalb `inventory` wählen und später `devices` ergänzen, wenn Geräteakten benötigt werden. Weitere Funktionsgruppen lassen sich danach mit ihren Abhängigkeiten ergänzen.

## Erinnerungsjobs und SMTP

Ohne Modul `reminders` werden weder neue Erinnerungen berechnet noch SMTP-Nachrichten versendet, auch wenn `DRK_DEVICE_REMINDERS` auf seinem bisherigen Standard `true` steht. Die Hintergrundworker bleiben dann still bereit. Eine gültige SMTP-Konfiguration darf schon vorhanden sein; sie bleibt bis zum Aktivieren des Moduls unbenutzt. Vorhandene Warteschlangen und Versandhistorien werden erhalten.

Bei aktivem Modul bestimmt `DRK_DEVICE_REMINDERS=false` weiterhin, ob der automatische Erinnerungsjob läuft. SMTP-Versand setzt sowohl das Modul als auch den eingeschalteten Job voraus. Die [SMTP-Konfiguration](smtp-erinnerungen.md) bleibt optional. Nach einer Aktivierung im Browser reagieren konfigurierte Hintergrundworker innerhalb einer Minute; die Erinnerungsberechnung erfolgt danach stündlich und bei einer Konfigurationsrevision. SMTP-Zugangsdaten und Transport bleiben außerhalb des Webformulars geschützt konfiguriert.

Die CLI-Befehle `remind-devices`, `deliver-reminders` und `retry-reminder-mail` prüfen die aktuelle gespeicherte Auswahl und verlangen das Erinnerungsmodul. Sie ändern die Modulauswahl nicht; der ursprüngliche `DRK_MODULES`-Startwert wird bei diesen manuellen Jobs nicht erneut angewendet. Ein ausdrücklich übergebenes `--modules` dient als zusätzliche Erwartungsprüfung und muss mit der gespeicherten Auswahl übereinstimmen. Sicherung, Migration und Kontenverwaltung bleiben unabhängig davon ausführbar, auch wenn eine fehlerhafte Startvorgabe den Webdienst blockiert.

## Bestehende Instanz auf Schema 34 aktualisieren

Die Web-Verwaltung der Module und der geschützte Einrichtungszustand benötigen Schema 34. Für eine vorhandene Instanz erst ein neues konsistentes Backup erstellen, anschließend ausdrücklich migrieren. Das Datenvolume und vorhandene Sicherungen erhalten. Beispiel für den lokalen Compose-Stack im Projektordner, gegebenenfalls jeweils `--env-file .env.local` ergänzen:

```sh
docker compose -f compose.local.yaml build
docker compose -f compose.local.yaml stop inventory
docker compose -f compose.local.yaml run --rm init backup \
  --db /data/inventory.db --out /data/before-web-setup-34.db
docker compose -f compose.local.yaml run --rm init migrate \
  --db /data/inventory.db
docker compose -f compose.local.yaml up -d --wait
```

Das Backupziel muss neu sein; bei einem vorhandenen Ziel einen anderen Namen wählen. Für den NAS-Stack die entsprechenden Schritte mit `maintenance` nach der [NAS-Anleitung](nas-dockhand.md) ausführen. Eine neue Datenbank wird direkt auf Schema 34 initialisiert; `init` aktualisiert bestehende Datenbanken weiterhin nicht automatisch. Bei der ersten Auswahl ohne vorherige Konfigurationshistorie wird der bisherige volle Umfang zugrunde gelegt. Bestehende Konten und Fachdaten öffnen keine Ersteinrichtung.

Beim Upgrade von Schema 33 bleiben bereits gespeicherte Modulauswahl und Konfigurationsrevision erhalten. Der identische Startwert erzeugt keine neue Revision. Bei einem älteren Schema ohne Konfigurationshistorie speichert der erste Dienststart dagegen die erste Revision; zuvor erteilte Einsatzfreigaben müssen danach erneut ausdrücklich geprüft und freigegeben werden, auch bei `DRK_MODULES=all`. Die bisherigen Entscheidungen und Nachweise bleiben unverändert in der Historie erhalten.
