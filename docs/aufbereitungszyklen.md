# Aufbereitungszyklen und Ergebnisfreigaben

Schema 30 ergänzt eine gerätebezogene, unveränderliche Zyklushistorie. Die fachliche Eignung des Aufbereitungsprozesses, dessen tatsächliche Durchführung und die Freigabe eines konkreten Ergebnisses sind getrennte Nachweise. Eine Ergebnisfreigabe hebt keine Defekt-, Rückruf- oder sonstige unabhängige Sperre auf und ist keine gesamte Einsatzfreigabe.

## Ablauf

1. Die erforderliche Aufbereitung in den Produktpflichten bewerten und den externen Prozess mit Herstellervorgaben, Klassifizierung, validierter Verfahrensversion, Befähigung, Ergebnisfreigabeverfahren und Lagerung fachlich bestätigen. Vorhandene Prozessbewertungen aus Schema 29 bleiben erhalten; für die neue Zyklusführung müssen sie einmal erneut bestätigt werden.
2. Im Bereich **Aufbereitungszyklen und Ergebnisfreigaben** das Gerät wählen und den aktuellen Stand laden. Aufbereitungsbedarf dokumentieren, etwa nach Benutzung oder Kontamination. Das erzeugt eine eigenständige Aufbereitungssperre.
3. Einen tatsächlich durchgeführten Zyklus erfassen: eindeutige externe Zyklus-/Chargenreferenz, tatsächlicher Abschlusszeitpunkt, Ausführender, Parameter-/Verfahrensnachweis, bestanden/nicht bestanden und freigegebenes Protokoll derselben Geräteakte. Ein Zyklus bleibt bis zur separaten Ergebnisfreigabe gesperrt. Zyklusreferenzen sind innerhalb einer Geräteakte eindeutig.
4. Nur den neuesten bestandenen, noch nicht freigegebenen Zyklus ausdrücklich für die Ergebnisfreigabe auswählen. Aktuelle Prozessbasis, Zyklusprotokoll und Freigabekriterien fachlich prüfen und mit eigener freigegebener Freigabeunterlage, Kompetenz/Beauftragung, Datum, Begründung und bestätigter Nutzungs-/Lagergültigkeit nachweisen. Diese Gültigkeit wird aus extern geprüften Vorgaben bestätigt, nicht automatisch als Sterilfrist berechnet. Die technische Obergrenze beträgt 366 Tage; die Prozessbewertung kann früher ablaufen.
5. Fehlgeschlagene oder ungeklärte Ergebnisse ablehnen; aktuelle Ergebnisfreigaben bei Bedarf widerrufen. Neuer Aufbereitungsbedarf, ein neuer Zyklus, Ablehnung, Widerruf oder Ablauf verhindert die weitere Verwendung der alten Freigabe. Nach Ablehnung, Widerruf oder Ablauf ist eine neue Durchführung mit neuer Zyklusreferenz erforderlich.

Die Abschlusszeit muss nach dem zuletzt dokumentierten Bedarf beziehungsweise vorherigen Aufbereitungsereignis und spätestens jetzt liegen. Ein älterer Zyklus kann dadurch keinen neuen Bedarf beseitigen. Die Browserangabe wird als Zeitpunkt mit Zeitzone übertragen; Datumsprüfungen verwenden UTC.

## Bindungen und Sperren

Die Zyklusführung bindet Geräteidentität, Standort, Geräteereignisstand, den fachlich bewerteten Prozess sowie Prüfversionen/Prüfsummen der maßgeblichen Unterlagen. Neue Zyklus- oder Freigabeunterlagen können ergänzt werden, ohne allein dadurch die gültige Prozesseignung zu verwerfen. Änderungen am Gerät, Verfahren oder den maßgeblichen Prozessunterlagen benötigen erneute Prozessbewertung. Prozesswiderruf, Ablehnung oder zwischenzeitlich fehlende positive Prozessbewertung verhindern automatische Wiederherstellung alter Zyklusfreigaben.

Der Zyklus und seine Ergebnisfreigabe binden jeweils die konkrete eigene Nachweisunterlage mit Prüfversion und Prüfsumme. Entzug oder erneute Prüfung einer dieser Unterlagen macht die alte Freigabe unwirksam; erneute Dokumentfreigabe reaktiviert sie nicht. Eine neue geprüfte Durchführung und separate Freigabe sind erforderlich.

Eine unveränderte Prozessabnahme in einer neuen Buch-/Pflichtbewertung erhält einen noch gültigen Zyklusstand. Änderungen der eigentlichen Prozessabnahme verwerfen ihn. Neue Aufbereitungsereignisse machen eine vorhandene Buchvollständigkeitsabnahme ungeklärt; ein veralteter, zuvor geladener Bewertungsstand wird abgewiesen.

Die gewöhnliche Geräte-Sperrfreigabe kann eine Aufbereitungssperre nicht aufheben. Der Aufbereitungsbereich kann nur die eigene Sperre beseitigen. Andere Sperren bleiben bestehen und verhindern positive Zyklus-/Ergebnisfreigaben. Gerät und Packbericht berücksichtigen die aktuelle Aufbereitungssperre; die gesamte Einsatzfreigabe bleibt `not_assessed`.

## Rechte, Historie und Grenzen

Lesen erfolgt innerhalb des erlaubten Gerätebereichs. Schreiben erfordert die aktuelle Geräteverantwortlichenrolle der tatsächlichen Betriebsstätte; Administratorrecht allein reicht nicht. Die Freigaberolle ersetzt keine fachliche Kompetenz. Standort-/Rollenrechte werden auch vor historischen Wiederholantworten geprüft. Unbestätigte Speicherungen behalten Originalinhalt und Idempotenzschlüssel; eine Wiederholantwort ändert den aktuellen Zustand nicht.

Historie, separate Ergebnisfreigaben und aktueller Status erscheinen im Geräteaktenarchiv. Höchstens 2000 Aufbereitungsereignisse je Gerät; der Export bleibt vollständig oder wird abgewiesen. Ereignisse und Wiederholantworten sind unveränderlich und werden mit Outbox atomar gespeichert.

Das Programm liest keine Aufbereitungsanlage aus und validiert keine Prozessparameter technisch. Externe Aufbereitungs-/Freigabenachweise und reale Kompetenz müssen fachlich geprüft werden. Der Status **Ergebnis freigegeben** ist keine automatische Aussage **steril**. Eine geräteübergreifende Chargen-/Sterilgutverwaltung, Materialaufbereitung, automatische Sterilfristen und vollständiges Aufbereitungs-QM sind separate offene Erweiterungen. Nicht dokumentierte Benutzung, Kontamination oder externe Verfahrensänderung erkennt die Anwendung nicht automatisch; Bedarf muss ausdrücklich erfasst werden.

## API

`GET /api/v1/devices/{id}/reprocessing` liefert `assessment` mit Version, aktuellem Status und letzter Historie; aktuelle Geräteverantwortliche erhalten zusätzlich `prerequisites` mit Prozessversion und `basis_hash`.

`POST` an denselben Pfad verwendet `action: need | cycle | release | reject | revoke`, `expected_version`, `expected_device_version`, `evidence`, `qualification`, `assignment`, `note` und einen Idempotenzschlüssel.

- `cycle`: zusätzlich aktuelle `process_version`, `expected_basis`, `performed_at` (RFC3339), passendes UTC-`performed_on`, eindeutige `cycle_reference`, `executor`, `parameters_evidence`, `result: passed | failed`, eigene freigegebene `document_id`.
- `release`: aktuelle `cycle_version`, deren maßgebliche `process_version`, aktuelle `expected_basis`, `reviewed: true`, `performed_on` ab Zyklustag, `release_evidence`, bestätigtes zukünftiges `valid_until` (RFC3339, höchstens 366 Tage), eigene freigegebene `document_id`.
- `reject` / `revoke`: zusätzlich konkrete aktuelle `cycle_version`; keine neue Durchführung oder positive Freigabe.
- `need`: keine Zyklus-/Freigabefelder.

Die Versions-/Prozessbasis muss frisch sein. Einzelne Freigaben werden nicht pauschal aus einem positiven Prozess-, Prüf- oder Packlistenstatus abgeleitet.

## Upgrade auf Schema 30

Vor Update ein neues konsistentes Backup erstellen und erhalten. Schema 30 ergänzt drei Tabellen für Aufbereitungsereignisse, Outbox und Wiederholantworten; vorhandene Daten bleiben erhalten. Dienst stoppen, die neue Programmversion ausdrücklich mit `inventory migrate --db ...` auf der bestehenden Datenbank ausführen, Dienst starten und Bereitschaft prüfen. `init` migriert vorhandene Datenbanken nicht automatisch.

Für Compose: zunächst mit laufender bisheriger Version sichern, dann neue Version bauen/laden. Anschließend `docker compose stop inventory`, `docker compose run --rm -T inventory migrate --db /data/inventory.db`, `docker compose up -d --wait`. Volumes erhalten. Die Migration ist wiederholbar. Ein Rollback benötigt die frühere Programmversion und die passende Sicherung vor Migration; Schema nicht manuell zurücksetzen und keine Volumes löschen. Siehe [Docker-Schnellstart](schnellstart-docker.md) und [NAS-Anleitung](nas-dockhand.md).
