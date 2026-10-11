# Verwaltung (Schema 23)

Die Verwaltungsoberfläche erscheint nach Anmeldung mit eigenständigem Administratorrecht. Sie verwaltet Betreiber, Betriebsstätten, Lagerorte und Benutzer. Fachrechte für Material oder Geräte ergeben sich ausschließlich aus den ausgewählten Rollen je Betriebsstätte. Mehrere Rollen sind möglich; ohne Zuordnung besteht kein fachlicher Zugriff. Das Administratorrecht umfasst die organisatorische Verwaltung der gesamten Installation. Es ist keine fachliche Qualifikation.

## Erstes Administratorkonto

Neue Datenbank zunächst initialisieren. Bei bestehender Installation zuerst konsistent sichern und ausdrücklich `migrate` ausführen. Weder Datenbank noch Volume ersetzen. Es gibt kein Standardpasswort; die Oberfläche vergibt keine Administratorrechte.

Für die einfache Compose-Installation (`compose.local.yaml`, HTTP oder HTTPS per `.env`):

```bash
read -r -s -p 'Administratorpasswort (mindestens 12 Zeichen): ' DRK_ADMIN_PASSWORD
printf '\n'
printf '%s\n' "$DRK_ADMIN_PASSWORD" | docker compose -f compose.local.yaml run --rm -T init create-admin --db /data/inventory.db --username admin
unset DRK_ADMIN_PASSWORD
```

Für `compose.nas.yaml` entsprechend `docker compose -p drk-nas --env-file .env.nas -f compose.nas.yaml run --rm -T maintenance …` verwenden. Für die lokale Go-Entwicklung: `bin/inventory create-admin --db var/inventory.db --username admin`, Passwort über stdin. Für das Entwicklungs-Compose `compose.yaml`: `docker compose run --rm -T inventory create-admin --db /data/inventory.db --username admin`.

Ein bestehendes aktives Konto lokal berechtigen oder das Recht entziehen:

```bash
docker compose -f compose.local.yaml run --rm -T init grant-admin --db /data/inventory.db --username admin
docker compose -f compose.local.yaml run --rm -T init revoke-admin --db /data/inventory.db --username admin
```

Diese lokalen Befehle widerrufen Sitzungen und protokollieren die Änderung. `revoke-admin` kann als bewusster lokaler Wiederherstellungsschritt auch das letzte Administratorrecht entfernen. Dann ist bis zur erneuten lokalen Vergabe keine Web-Verwaltung möglich. Die Web-Oberfläche kann Administratorrechte weder vergeben noch entziehen. Ein Deaktivieren des eigenen Kontos oder des letzten aktiven Administrators wird verhindert, Letzteres auch durch `disable-user`.

## In der Oberfläche

Zuerst Betreiber, danach Betriebsstätte und Lagerort anlegen. Anschließend Benutzer mit Passwort und ausdrücklich ausgewählten Fachrechten erstellen. Aktivstatus/Rechte und Passwort werden in getrennten Aktionen geändert. Benutzername, Betreiberzuordnung einer Betriebsstätte und Betriebsstättenzuordnung eines Lagerorts bleiben fest. Es gibt keine Löschfunktion; bestehende Inventar- und Nachweisreferenzen bleiben erhalten.

Jede Änderung benötigt eine Begründung. Änderungen an vorhandenen Objekten verwenden die geladene Version; nach einem Konflikt neu laden und abgleichen. Rechte-, Aktivstatus- und Passwortänderungen beenden alle Sitzungen des betroffenen Kontos. Auch bei Änderungen am eigenen Konto ist eine erneute Anmeldung erforderlich.

Bei unklarer Antwort bleiben Daten und Idempotenzschlüssel für das erneute Senden gesperrt. Passwortdaten verbleiben dabei nur im Arbeitsspeicher des geöffneten Tabs; das sichtbare Passwortfeld wird sofort geleert. Nach Sitzungsablauf kann dasselbe Konto den unveränderten Vorgang wiederholen. Ein anderer Benutzer kann diesen Entwurf nicht senden. Schließen oder Neuladen des Tabs verwirft den Entwurf; vor einer Neuerfassung zuerst den tatsächlichen Stand prüfen.

Die letzten 200 Verwaltungsänderungen werden angezeigt; ältere unveränderliche Ereignisse bleiben in der gesicherten Datenbank. Vorher-/Nachher-Zustände enthalten keine Passwörter oder Passwort-Hashes. Die Verwaltungsübersicht verweigert mehr als 1000 Objekte je Typ vollständig, statt unbemerkt Teile auszublenden. Pagination und administrative MFA sind spätere Erweiterungen.

## API

`GET /api/v1/me` enthält `administrator`; `GET /api/v1/admin` liefert Objekte mit `kind`, `id`, `version`, festen Zuordnungen und Benutzerrechten sowie die letzten Ereignisse. Beide administrativen Endpunkte prüfen das eigenständige Recht; Fachrollen verleihen es nicht.

`POST /api/v1/admin/changes` benötigt Sitzung, Origin, CSRF und Idempotenzschlüssel. Aktionen: `create_operator`, `update_operator`, `create_site`, `update_site`, `create_location`, `update_location`, `create_user`, `update_user`, `reset_password`. Updates benötigen `id` und `expected_version`; jedes Objekt hat eine separate Verwaltungsrevision, die auch lokale Rechteänderungen berücksichtigt. Alle Aktionen benötigen `note`. Rechte sind eine ausdrückliche Liste `{site_id,role}` einschließlich einer leeren Liste für keinerlei Fachrechte. Nach Wiederholungen werden Sitzung und Administratorrecht erneut geprüft. Revision, Ereignis, Sitzungssperre und Antwortspeicherung werden gemeinsam in einer Transaktion geschrieben.
