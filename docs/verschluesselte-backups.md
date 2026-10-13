# Verschlüsselte Backups und isolierte Wiederherstellung

`scripts/backup-restic.py` ergänzt den bisherigen CLI-Snapshot um ein verschlüsseltes Restic-Repository. Es benötigt auf dem ausführenden Host Python ab 3.11, Restic und direkten Zugriff auf die SQLite-Datenbank einschließlich ihres WAL-Verzeichnisses. Der Container benötigt keine neuen Abhängigkeiten. Für Dockhand/NAS kann ein separater Backup-Job dieselben Werkzeuge und das Datenvolume bereitstellen; der Pfad muss auf die tatsächlich verwendete Datenbank zeigen.

Die SQLite-Backup-API erstellt einen konsistenten Snapshot bei laufendem Betrieb. Integrität und Fremdschlüssel werden vor der Übertragung geprüft. Dokumentinhalte sind als BLOBs in der Datenbank enthalten. Datenbank und SHA-256-Manifest werden aus einem privaten temporären Verzeichnis an Restic übergeben. Temporäre Klartextdateien werden anschließend entfernt; für vertrauliche Daten einen verschlüsselten temporären Datenträger verwenden. TLS-Zertifikate, Schlüssel, Compose-Konfiguration und Restic-Zugangsdaten müssen zusätzlich getrennt gesichert werden. Das Skript sichert diese Dateien nicht.

## Einrichtung

Ein externes Repository wählen, beispielsweise ein separat betriebenes SFTP-Ziel oder einen S3-kompatiblen Speicher. Ein Repository auf derselben NAS-Platte schützt nicht vor deren Verlust. Repository und Passwortdatei über die üblichen Restic-Umgebungsvariablen konfigurieren; ein zufälliges starkes Passwort sicher hinterlegen und die Datei nur für den Backup-Benutzer lesbar machen. Passwort und erforderliche Zugangsdaten getrennt vom NAS für eine Wiederherstellung verfügbar halten.

```sh
export RESTIC_REPOSITORY=sftp:backup-server:/backups/drk
export RESTIC_PASSWORD_FILE=/geschuetzter/pfad/restic-password
restic init
python3 scripts/backup-restic.py backup --db /daten/drk/inventory.db
restic snapshots --tag drk-inventory
```

`restic init` nur einmal für ein neues Repository ausführen. SSH-Hostprüfung und Zertifikatsprüfung aktiviert lassen. Das Skript übernimmt die übrigen Restic-Umgebungsvariablen für das Speicherziel. Repository-Zugangsdaten gehören nicht ins Git-Repository.

## Automatisierung

Den Backup-Befehl als wiederkehrenden NAS-/Dockhand-Job oder Host-Timer konfigurieren, beispielsweise täglich. Absolute Pfade verwenden, Umgebungsvariablen im Job setzen und ausreichende Leserechte auf Datenbank und WAL-Verzeichnis gewähren. Jeder Lauf muss anhand seines Exitcodes überwacht werden; bei Fehlern eine Benachrichtigung im vorhandenen NAS-Monitoring einrichten. Das Skript liefert nur bei abgeschlossener Sicherung Erfolg. Aufbewahrungsregeln werden nicht automatisch angewendet: Löschung durch `restic forget`/`prune` erst mit ausdrücklich festgelegter Strategie separat konfigurieren. Regelmäßig `restic check --read-data` und eine Rücksicherung durchführen.

## Rücksicherung

Eine konkrete Snapshot-ID aus `restic snapshots --tag drk-inventory` auswählen. `latest` ist bewusst ausgeschlossen. Immer ein neues, noch nicht vorhandenes Zielverzeichnis verwenden:

```sh
python3 scripts/backup-restic.py restore \
  --snapshot KONKRETE_HEX_SNAPSHOT_ID \
  --target /daten/drk-wiederherstellung
```

Restic entschlüsselt zunächst in einen privaten Zwischenbereich. Das Skript prüft Manifest, SHA-256, SQLite-Integrität, Fremdschlüssel und vorhandene Inventory-Tabellen. Erst danach legt es das Zielverzeichnis an und schreibt die wiederhergestellte Datenbank. Bestehende Ziele werden verweigert. Bei einem Fehler während der abschließenden Dateikopie kann ein unvollständiges, separates Ziel verbleiben; die aktive Datenbank wird nicht verändert.

Die Wiederherstellung ist keine automatische Umschaltung: zunächst mit einem passenden Programmstand eine getrennte Testinstanz starten, Anhänge, Konten und repräsentative Bestände prüfen. Den Produktivwechsel separat durchführen, den Dienst vorher stoppen und aktuelle Daten sowie WAL-/SHM-Dateien bewahren. Alte Restic-Snapshots behalten ihren damaligen Schemastand; nötige Migrationen erst nach eigener Sicherung ausdrücklich ausführen. SMTP-Versand und externe Integrationen in der Testinstanz deaktivieren.

## Verifikation und Grenzen

Der Integrationstest lässt ein echtes Restic-Repository anlegen, einen Inventory-Snapshot verschlüsseln und isoliert wiederherstellen. Die Testkopie enthält zusätzlich bestätigte, noch im WAL liegende Daten. Er vergleicht sämtliche Tabellen einschließlich Dokumentinhalten, verweigert bestehende Ziele und `latest`, verweigert fehlende Quelldateien und inkonsistente Manifeste, prüft ein falsches Passwort und liest bei `restic check --read-data` den vollständigen Repository-Inhalt:

```sh
python3 scripts/test-backup-restic.py --restic /pfad/restic --db var/inventory.db
```

Der Test verwendet ausschließlich ein temporäres Repository und verändert die Quelldatenbank nicht. Die Cloud-Prüfung ersetzt keine Abnahme deines externen Speichers, NAS-Jobs oder vollständigen Wiederanlaufs nach Hardwareausfall. Automatische Jobbereitstellung und die Überwachung der gesamten Anwendung bleiben offen.
