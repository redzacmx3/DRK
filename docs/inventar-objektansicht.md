# Inventarliste, Einzelanlage und Objektansicht

Ab `v0.1.0-rc.5`, Datenbankschema 35. Umsetzung des ersten Pakets aus dem [Referenzvergleich](design-referenzanalyse.md).

## Gegenstand finden und öffnen

Die Inventarliste bietet Suche, Art- und Standortfilter sowie Sortierung nach Inventarnummer, Bezeichnung, Standort oder Anlagedatum. Die Suche berücksichtigt Inventarnummer, aktuelle Bezeichnung, Hersteller, Modell, Serien-/Losnummer, Fremdcode und Standortname. Auch korrigierte Grunddaten und Gegenstände jenseits der früheren 500er-Übersicht werden gefunden. Groß-/Kleinschreibung einschließlich Umlauten wird für die Suche vereinheitlicht.

Die API liefert 50 Gegenstände je Browserseite, mit Zurück/Weiter und Gesamtzahl des gefilterten berechtigten Bestands. Die Geräte-/Equipmentzähler beziehen sich auf diese Filter, die Lagerortzahl auf den eigenen Berechtigungsbereich. Der allgemeine CSV-Export behält seinen bisherigen Umfang und sein eigenes Limit; die aktuelle Suchauswahl ist kein neuer gefilterter Export.

Auf die Bezeichnung eines Gegenstands klicken. Die Objektansicht enthält eine Übersicht, Grunddaten, Geräteakte/Prüfungen, Dokumente, fachliche Nachweise und Historie. Zusätzliche Reiter erscheinen nur bei passenden aktivierten Modulen. Alle Bereiche behalten das gewählte Objekt. Die Rückkehr zur Inventarliste erhält die dortigen Filter und die Seite.

Die bestehenden Fachformulare, Nachweisprüfungen und Freigaberegeln bleiben maßgeblich. Nicht bestätigte Schreibvorgänge behalten ihren Inhalt und Wiederholungsschlüssel. Ein anderer Gegenstand kann erst nach Klärung eines solchen Vorgangs geöffnet werden; für eine Wiederholung nach erneuter Anmeldung ist dasselbe Konto erforderlich.

## Einzelnen Gegenstand anlegen

Mit Geräteverantwortung am gewünschten Standort **Gegenstand anlegen** wählen. Pflichtangaben sind:

- Inventarnummer: Buchstaben/Ziffern sowie `.`, `_`, `/`, `-`; höchstens 100 Zeichen.
- Bezeichnung.
- Standort aus den eigenen berechtigten Betriebsstätten.

Art kann Gerät, Equipment oder ungeklärt sein. Hersteller, Modell und Seriennummer dürfen unbekannt bleiben. Eine leere Bemerkung erhält die Begründung „Manuelle Erstinventarisierung“. Inventarnummern werden vor der Anlage ohne Unterscheidung von Groß-/Kleinschreibung auf vorhandene Belegung geprüft. Historisch schon vorhandene abweichende Kennungen werden nicht automatisch geändert oder zusammengeführt.

Nach erfolgreicher Anlage öffnet sich der neue Gegenstand. Er hat dadurch keinen Inbetriebnahme-, Prüf- oder Einsatzfreigabenachweis. Equipment bleibt ohne zugeschaltete Geräteakten inventarisierbar. Für viele Gegenstände stehen weiterhin die Excel-/CSV-Vorlagen bereit.

## Erkennungsfoto

In der Übersicht können Geräteverantwortliche ein Foto speichern oder das aktuelle Foto entfernen. PNG und JPEG bis 4 MiB, höchstens 6 Millionen Pixel und 6.000 Pixel je Seite. Der Server decodiert das Bild vollständig und kodiert ausschließlich die Bildpixel als PNG neu; Metadaten und angehängte Originaldaten werden nicht ausgeliefert. Auch das Ergebnis darf höchstens 4 MiB groß sein. Ein öffentliches Bildhosting oder ein externer Bilddienst wird nicht benötigt.

Das Foto ist Teil der Inventarisierung und funktioniert unabhängig vom Dokumentenmodul. Es ist kein fachlicher Nachweis. Bestehende Dokumentanhänge bleiben in ihrem bisherigen Quarantäne-/Prüfverfahren; sie werden durch die Fotofunktion nicht freigegeben.

Jeder Foto- oder Entfernungsvorgang hat eine eigene Fotoversion, Zeitpunkt, Akteur, Begründung und Prüfsumme. Gleichzeitige Änderungen erfordern einen frischen Abgleich. Entfernen blendet das aktuelle Foto aus; frühere Versionen bleiben unverändert erhalten und liegen mit in der Datenbanksicherung. Fotos verändern die technische Geräteversion und Gerätefreigaben nicht. Der strukturierte Geräteaktenexport enthält die Fotohistorie als Metadaten, keine zusätzlichen Bilddateien; eine vollständige SQLite-Sicherung enthält auch die Bildinhalte.

Der Bildabruf und die Historie prüfen die eigenen Standortrechte. Erkennungsfotos sind keine anonym öffentlich abrufbaren Dateien.

## API

- `GET /api/v1/inventory/devices`: `q`, `kind`, `location`, `sort` (`number`, `name`, `location`, `created`), `direction`, `limit` (1–100), `offset` (0–1.000.000). Antwort: `items`, `total`, `equipment`, `limit`, `offset`. Die Antwort ist begrenzt; die serverseitige Suche streamt die berechtigten Identitäten und projiziert nur die angeforderte Seite. Für sehr große Bestände bleibt eine Lastabnahme erforderlich.
- `POST /api/v1/devices`: bestehende Geräteanlage, nun mit optionalen Hersteller-/Modell-/Serienangaben und optionalem `asset_kind`.
- `GET /api/v1/devices/{id}/photo`: Fotohistorie mit aktueller Version zuerst.
- `POST /api/v1/devices/{id}/photo`: `expected_version` der Fotohistorie, `action` (`set` oder `remove`), Base64-`content`, `note`. Schreibzugriff mit Sitzung, CSRF und gemeinsamem Idempotenznamensraum.
- `GET /api/v1/devices/{id}/photo/content`: aktuell gültige PNG-Bilddarstellung nach Berechtigungsprüfung.

## Upgrade in Dockhand

Bei bestehender Installation zuerst eine konsistente Sicherung mit dem bisherigen laufenden Image erstellen, etwa in dessen Konsole:

```sh
/app/inventory backup --db /data/inventory.db --out /data/before-inventory-35.db
```

Einen neuen, noch nicht vorhandenen Sicherungsnamen verwenden und die Sicherung zusätzlich außerhalb des NAS aufbewahren. Anschließend:

1. Den bisherigen Stack stoppen.
2. Die Compose-Datei aus dem erfolgreich gebauten Release `v0.1.0-rc.5` im bestehenden Stack übernehmen. Den tatsächlichen bisherigen Wert von `DRK_DATA_VOLUME`, Adresse, Port und alle übrigen Einstellungen beibehalten.
3. `DRK_IMAGE=ghcr.io/redzacmx3/drk:v0.1.0-rc.5` und einmalig `DRK_INIT_COMMAND=migrate` setzen.
4. Image laden und Stack bereitstellen. Der vorgeschaltete Dienst migriert ausdrücklich auf Schema 35; danach startet die Anwendung.
5. Nach erfolgreicher Prüfung `DRK_INIT_COMMAND` auf `init` zurücksetzen. Browserseite vollständig neu laden.

Die Variable wird nur von der entsprechend aktualisierten Compose-Datei ausgewertet. Mit der bisherigen Compose-Datei kann stattdessen der vorhandene explizite CLI-Migrationsweg verwendet werden; siehe [Image-Releases](image-release.md). Neue leere Datenbanken werden mit dem Standard `init` eingerichtet. Bestehende Datenbanken werden ohne ausdrücklich gewählte Migration nicht automatisch aktualisiert.

## Weitere Pakete aus dem Vergleich

Dieses Paket enthält noch keine Ausleihe/Reservierung, scanbasierte körperliche Geräteinventur, neue Standort-/Kategoriehierarchie, Materialdetailkarte, Mindestbestandsregel oder FEFO-Auswahlhilfe. Diese sind im Vergleich als weitere Umsetzungspakete beschrieben. Vorhandene Materialzählungen und Fachprüfungen sind keine neu implementierte Geräteinventurrunde.
