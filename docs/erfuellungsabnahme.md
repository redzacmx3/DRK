# Fachliche Erfüllungsabnahme der Produktpflichten

Schema 31 ergänzt eine separate, unveränderliche Erfüllungshistorie. Die bisherige Produktpflichtenbewertung erfasst weiterhin die Anwendbarkeit; ihre Teilabnahmen bleiben erhalten. Erst eine ausdrücklich gespeicherte Erfüllungsabnahme führt die acht Bereiche für dieses Gerät fachlich zusammen. Die tatsächliche rechtliche Einordnung, Vollständigkeit und Prüferkompetenz werden extern geprüft und dokumentiert.

## Ablauf

1. Anwendbarkeit aller acht Produktpflichtbereiche auf Grundlage einer freigegebenen Produktunterlage fachlich bewerten. Erforderliche Bereiche mit ihren konkreten Teilnachweisen abnehmen. Gegebenenfalls aktuelle personenbezogene Nachweise und Aufbereitungszyklen vorher erfassen; Änderungen daran können Aktenbindungen verwerfen.
2. Eine freigegebene Erfüllungsunterlage in derselben Geräteakte bereitstellen. Neue Unterlagen können bestehende Aktenbindungen ändern: danach nötige Teilabnahmen erneut prüfen / bestätigen.
3. Unter **Fachliche Erfüllungsabnahme der Produktpflichten** das Gerät laden. Die acht Vorprüfungen zeigen aktuelle Teilnachweise, dokumentierte Ausnahmen, fehlende Nachweise oder Blockaden. Diese Vorschau allein ist keine Abnahme.
4. Für jeden Bereich ausdrücklich bestätigen, ob sein gesamter Umfang fachlich vollständig geprüft wurde. Nachweis und Begründung des vollständigen Umfangs erfassen, auch bei einer Ausnahme. Abnahmeentscheidung, Datum, Gültigkeit, Unterlage, Abnahmekompetenz, Auftrag und Gesamtbegründung angeben und die Fachprüfung ausdrücklich bestätigen.
5. Unvollständige oder ungeklärte Ergebnisse ablehnen; bestehende Abnahmen bei Bedarf widerrufen. Ablehnung und Widerruf bleiben blockierend, bis eine neue zulässige fachliche Abnahme gespeichert wird. Keine allgemeine Gerätesperre wird aufgehoben.

Die Befristung beträgt höchstens 366 Tage und ersetzt keine gesetzliche oder Herstellerfrist. Laufende Kontroll-, Qualifikations-, Dokumenten- und Aufbereitungsbedingungen können früher unwirksam werden. Abnahmedatum UTC, frühestens am Tag der aktuellen Produktpflichtenbewertung und der Freigabe der Erfüllungsunterlage.

## Zusammenführung

Die Anwendung verlangt folgende aktuelle Teilvoraussetzungen:

| Bereich | Vorprüfung bei ausdrücklich erforderlicher Pflicht |
|---|---|
| Anlage 1 | Inbetriebnahmenachweis, fachlich abgenommene besondere Funktionsprüfung und Einweisung der benannten Person; STK entsprechend aktueller Kontrollregel |
| Anlage 2 | MTK entsprechend aktueller Kontrollregel; weitergehenden Bereichsumfang ausdrücklich extern fachlich prüfen |
| Medizinproduktebuch | Aktuelle Teilabnahme für Buchvollständigkeit, Zugang und Aufbewahrung |
| Herstellerwartung | Aktuelle fachliche Herstellerwartungsabnahme |
| IT-Sicherheit | Aktuelle externe IT-Sicherheitsabnahme einschließlich bestätigter Fälligkeit und Maßnahmenstatus |
| Aufbereitung | Aktuelle Prozesseignung und konkrete gültige Ergebnisfreigabe |
| Besondere Anwendergruppen | Aktuelle fachliche Gruppen-/Umfangs-/Ausnahmebewertung |
| Weitere Pflichten | Aktuelle vollständige Einzelpflichtenabnahme und elektrische Sicherheit entsprechend aktueller Kontrollregel |

Für STK, MTK und elektrische Sicherheit gilt: eine ausdrücklich begründet nicht anwendbare Kontrollregel kann als dokumentierte Ausnahme vorgeprüft werden. Bei erforderlicher Kontrolle sind aktuelle oder demnächst fällige Frist **und** passende fachliche Teilabnahme nötig. Eine unbekannte Kontrollpflicht, ein bloß aufgelöstes Prüfereignis oder eine Freitextreferenz genügt nicht. Fristüberschreitung beziehungsweise negative Teilnachweise blockieren.

Begründete Bereichsausnahmen werden aus der Anwendbarkeitsbewertung übernommen, aber nicht automatisch rechtlich bestätigt. Auch acht dokumentierte Ausnahmen benötigen eine neue ausdrückliche Erfüllungsabnahme mit vollständiger fachlicher Bereichsprüfung. Die Software behauptet nicht, dass die modellierten Teilvoraussetzungen sämtliche produktspezifischen gesetzlichen Aufgaben abdecken. Deshalb sind die vollständigen acht Bereichsumfänge mit Nachweisen und Verantwortlichkeit ausdrücklich extern abzunehmen.

## Aussage und Bindung

`accepted`: aktuelle, ausdrücklich dokumentierte fachliche Erfüllungsabnahme für dieses Gerät. `unknown`: Abnahme fehlt, ist abgelaufen, ihre gebundene Grundlage hat sich geändert oder notwendige Teilnachweise fehlen. `blocked`: ausdrückliche Ablehnung / Widerruf, unabhängige Gerätesperre, Außerbetriebnahme oder blockierter Pflichtnachweis.

Gebunden werden die Version der Anwendbarkeitsbewertung, Geräte-/Personen-/Dokumentstände, Aufbereitungsereignisstand, die acht aktuellen Vorprüfungen und Prüfversion / SHA-256 der Erfüllungsunterlage. Schon eine neue Anwendbarkeitsbewertung verlangt neue Erfüllungsabnahme. Dokumententzug und erneute Freigabe reaktivieren keine alte Abnahme. Die Erfüllungshistorie selbst erzeugt keine rückwirkende Änderung ihrer eigenen Grundlage; sie wird separat archiviert.

Die Packlistenbewertung zeigt die eigene Erfüllungsbewertung je Gerät. Die Teilkomponente `product_duties` wird nur dann `met`, wenn alle Sollgeräte eine aktuelle ausdrückliche Erfüllungsabnahme besitzen. Fehlende Geräte / Teilabnahmen bleiben ungeklärt, Ablehnungen und Blockaden bleiben blockierend. Materialprüfung, Anwenderqualifikation, Gebrauchsanweisungszugang, Endanwenderabnahme, physische Geräteprüfung und technische Sperren werden unverändert getrennt bewertet. Sind sämtliche erfassten Teilkomponenten erfüllt, bezeichnet `operating_evidence.status=recorded_prerequisites_met` ausschließlich diesen Nachweisstand. Die gesamte Einsatzfreigabe und `overall_readiness=not_assessed` bleiben separat.

## API, Rechte und Export

`GET /api/v1/devices/{id}/duty-fulfilment` liefert `assessment` mit Version, Geräteversion, Status, Gründen, acht `prerequisites`, den letzten 100 Ereignissen und aktuellem Schreibrecht `manager`. Der aktuelle `basis_hash` wird nur Geräteverantwortlichen der tatsächlichen Betriebsstätte für die Vorbereitung geliefert. Administratorrecht allein erlaubt keine Abnahme.

`POST` erwartet `action=accept|reject|revoke`, `expected_version`, `expected_device_version` und vier Prüfbegründungen `qualification`, `assignment`, `evidence`, `note`. Abnahme und Ablehnung benötigen zusätzlich `expected_basis`, `reviewed=true`, `performed_on`, `valid_until` (RFC3339), `document_id` sowie acht `areas` mit `kind`, ausdrücklich booleschem `scope_complete`, `evidence` und `reason`. Bereichscodes entsprechen der Anwendbarkeitsbewertung. Positive Abnahme benötigt acht vollständige Bereiche und aktuelle Vorprüfungen. Ablehnung kann geprüfte Unvollständigkeit dokumentieren. Widerruf enthält keine neuen Abnahmefelder und benötigt einen bestehenden Erfüllungsnachweis.

Origin, CSRF und gemeinsamer Idempotenzschlüsselraum gelten. Ereignis, Outbox und Wiederholantwort werden atomar gespeichert. Exakte historische Antworten werden erst nach aktueller Sitzungs-/Standortrechteprüfung ausgeliefert und reaktivieren keine alte Bewertung. Bei unbestätigter Speicherung den eingefrorenen Inhalt unverändert mit demselben Schlüssel wiederholen; bei Versionskonflikten neu laden und fachlich abgleichen. Oberfläche setzt die Bestätigung nach Eingabeänderungen zurück und verwirft verspätete Antworten nach Abmeldung.

Die Geräteakte enthält vollständige Erfüllungshistorie, aktuelle Bewertung und Unterlagen im bestehenden sicheren ZIP-/HTML-Export mit Manifest. Bestehende Grenzen: 16 KiB pro Änderungsanfrage, 2000 Ereignisse pro Gerät / Historienbereich, keine Teilarchive bei überschrittenen Exportgrenzen.

## Upgrade auf Schema 31

Mit der laufenden bisherigen Version zunächst ein neues konsistentes Backup einschließlich Anhängen erstellen. Compose-/SMTP-Konfiguration und TLS-Schlüssel getrennt geschützt erhalten. Dann neues Image bauen / laden, Dienst stoppen, ausdrücklich migrieren und starten:

```sh
# Zuerst mit bisherigem Image und neuem, noch nicht vorhandenen Sicherungsziel:
docker compose run --rm --no-deps -T inventory backup --db /data/inventory.db --out /data/vor-schema31.db
# Danach das neue Image bauen / laden:
docker compose stop inventory
docker compose run --rm --no-deps -T inventory migrate --db /data/inventory.db
docker compose up -d --wait
```

Die Migration ergänzt drei Tabellen mit unveränderlicher Historie und Wiederholantworten. Sie ist wiederholbar und erhält vorhandene Daten, Anhänge und frühere Teilabnahmen. `init` migriert vorhandene Datenbanken weiterhin nicht automatisch. Fresh-Installationen werden direkt mit Schema 31 initialisiert. Für lokale Programme ebenfalls mit altem Programm sichern, neues Programm ausdrücklich `migrate --db ...` ausführen und anschließend Bereitschaft prüfen. Rollback nur mit passendem früherem Programm und Vorabbackup; keine Schemazeilen zurücksetzen und keine Volumes löschen. Für `compose.local.yaml` durchgängig denselben `-f`-Parameter verwenden. `--no-deps` verhindert bei der Migration, dass ein vorgeschalteter `init`-Dienst die noch alte Datenbank vorzeitig mit dem neuen Schema prüft.

Die reale fachliche Abnahme deiner Geräte, NAS-Betrieb, Scanner, eigener SMTP-Anbieter und vollständige Wiederanlaufprüfung bleiben betrieblich zu erproben.

Aktueller Betriebsstand Schema 32: Diese fachliche Produktabnahme wird inzwischen in die [personenbezogene Packlisten-Einsatzfreigabe](einsatzfreigabe.md) eingebunden. Die Produktbewertung selbst bleibt eine Teilbewertung ohne pauschale Einsatzfreigabe. Für aktuelle Updates gelten die dortigen Upgrade-Schritte.
