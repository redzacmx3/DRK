# Personenbezogene Einsatzfreigabe einer Packliste

Schema 32 ergänzt eine ausdrückliche betriebliche Einsatzfreigabe mit eigener unveränderlicher Historie. Sie gilt ausschließlich für die benannte Person, eine konkrete Sollversion am Lagerort und den dokumentierten Einsatzbezug / Nutzungsumfang. Eine Freigabe für eine andere Person oder Sollversion wird nicht übertragen. Sie dokumentiert die externe fachliche Entscheidung; Kompetenz, Auftrag und reale Einsatzbedingungen werden nicht aus einer Benutzerrolle abgeleitet.

## Vorbereitung und Bedienung

1. Aktuelle Sollpackliste mit den tatsächlich erforderlichen Materialien, Geräten und Zubehör hinterlegen. Eine leere beziehungsweise rein materialbezogene Gerätebewertung ermöglicht keine Gesamtfreigabe; zusätzliche Freigabeumfänge sind gesondert zu modellieren.
2. Aktuelle Materialbestände und physische Materialprüfung erfassen. Geräte am Sollort einschließlich Funktion und Zubehör prüfen; Inbetriebnahme, Kontrollregeln und Fristen sowie Sperren / offene Meldebewertungen müssen geklärt sein.
3. Für die konkret vorgesehene Person aktuelle Qualifikation, ausdrückliche Einweisungspflicht / Zuordnung, Gebrauchsanweisungszugang und fachliche Endanwenderabnahme hinterlegen. Jedes Sollgerät benötigt zusätzlich eine aktuelle ausdrückliche Erfüllungsabnahme der Produktpflichten. Dokumente und weitere Personenereignisse vorher ergänzen, dann gegebenenfalls Aktenbindungen erneut bestätigen.
4. Unter **Personenbezogene Einsatzfreigabe** Sollversion und Person wählen, Stand laden und alle sieben Teilkomponenten prüfen. Die Vorschau allein gibt nichts frei.
5. Einsatzbezug, geprüften vollständigen Nutzungsumfang, Einsatzbedingungen, Übergabenachweis, Freigabekompetenz, betrieblichen Auftrag, Nachweis und Begründung erfassen. Entscheidung, Datum und Gültigkeit ausdrücklich bestätigen. Die Freigabe ist heute (UTC) zu dokumentieren und technisch auf höchstens 24 Stunden begrenzt; das ist keine gesetzliche Frist. Bestehende Teilnachweise und Tagesbindungen können früher unwirksam werden.
6. Negative Ergebnisse ablehnen beziehungsweise bestehende Entscheidungen widerrufen. Ablehnung / Widerruf bleiben blockierend, bis eine neue zulässige ausdrückliche Freigabe gespeichert wird. Die neueste Entscheidung pro Person / Sollversion ist maßgeblich, auch bei unterschiedlichen Einsatzbezügen.

Nur Personen mit **Materialverantwortung und Geräteverantwortung an derselben tatsächlichen Betriebsstätte** dürfen Freigaben schreiben und fremde personenbezogene Freigaben prüfen. Administratorrecht allein genügt nicht. Weitere Leser sehen ausschließlich den eigenen Freigabestand und die eigene Historie. Deaktivierte oder nicht mehr im Gerätebereich berechtigte Personen können nicht positiv freigegeben werden.

## Aussage und Bindung

Die folgenden Voraussetzungen müssen vollständig aktuell sein: Material; physische Anwesenheit / Funktion / Zubehör; technische Kontrollen und Sperren; persönliche Qualifikation / Einweisung; Gebrauchsanweisungszugang; fachliche Endanwenderabnahme; fachliche Erfüllung der Produktpflichten. Bloße Dokumentreferenzen oder acht Anwendbarkeitsausnahmen ersetzen keine erforderliche Fachabnahme.

Gebunden werden Sollversion, Lagerort / Betriebsstätte / Betreiber und deren Verwaltungsstände, Material- und Geräteschnappschüsse, die konkreten neuesten physischen Prüfnachweise, persönliche Nachweise und Verwaltungsstand des ausgewählten Kontos, Produkt-/Erfüllungs-/Aufbereitungsstände sowie die aktuelle Freigabeberechtigung / Verwaltungsrevision der freigebenden Person. Ein neuer physischer Nachweis verlangt auch bei unverändertem Ergebnis neue Einsatzfreigabe. Rechteentzug, Kontodeaktivierung und spätere Wiedererteilung / Reaktivierung reaktivieren alte Freigaben nicht. Änderungen an Einsatzbedingungen oder Umfang sind organisatorisch als neue Entscheidung zu erfassen; ungemeldete externe Änderungen kann die Anwendung nicht erkennen.

`released` bedeutet: aktuelle ausdrückliche Freigabe im zurückgegebenen Umfang. `unknown` bedeutet: Freigabe fehlt, ist abgelaufen oder ihre Grundlage ist verändert / unvollständig. `blocked` bedeutet: eine Voraussetzung ist blockiert, die freigebende Person aktuell nicht mehr berechtigt oder die Entscheidung wurde ausdrücklich abgelehnt / widerrufen.

Die zusammengeführte Packlistenbewertung bewertet standardmäßig das angemeldete Konto. Nur dessen aktuelle Freigabe setzt `overall_readiness=released_for_user`; der Einsatzbezug, Umfang und Ablauf sind Bestandteil der Entscheidung. Eine früher dokumentierte, inzwischen unwirksame Freigabe wird `not_released`; ohne eigene Entscheidung bleibt `not_assessed`. Andere Geräte-, Material- und Personenendpunkte bleiben eigene Teilbewertungen und behaupten weiterhin keine allgemeine Einsatzfreigabe.

Materialbuchungen, Rückrufe, Defekte, Fälligkeiten, neue Dokumentprüfstände, Widerrufe und neue Sollversionen verhindern Freigabe entsprechend ihren aktuellen Auswirkungen. Das Modul hebt keine Gerätesperren auf, bucht kein Material und verändert keine Benutzerrechte. Freigabe ist ein betrieblicher Nachweis und ersetzt kein aktuelles Prüfen der tatsächlichen Situation vor Gebrauch. Keine Patientendaten in Einsatzreferenzen eintragen.

## API und Export

`GET /api/v1/packlists/{id}/release?user_id=...` liefert `assessment`, `manager` und für berechtigte Freigabeverantwortliche eine standortgebundene Personenauswahl. Ohne `user_id` wird das eigene Konto bewertet. `basis_hash` für neue Entscheidungen erhalten nur aktuelle Freigabeverantwortliche. Die letzten 100 Ereignisse sind auf die ausgewählte Person beschränkt; die Versionsnummer ist der gemeinsame Freigabestream dieser Sollversion über alle Personen.

`POST /api/v1/packlists/{id}/release` benötigt `action=release|reject|revoke`, `user_id`, `expected_version` sowie `qualification`, `assignment`, `evidence`, `note`. Freigabe und Ablehnung benötigen zusätzlich `expected_basis`, `reviewed=true`, `performed_on` (heute UTC), `valid_until` (RFC3339, höchstens 24 Stunden), `mission_reference`, `operating_scope`, `conditions_evidence`, `handover_evidence`. Positive Freigabe benötigt `prerequisites.status=recorded_prerequisites_met`. Ablehnung kann geprüfte offene / blockierte Bedingungen festhalten. Widerruf enthält keine neuen positiven Freigabefelder und benötigt eine vorhandene Entscheidung für diese Person / Sollversion.

Origin, CSRF, gemeinsamer Idempotenzschlüsselraum und aktuelle Berechtigungen vor historischen Wiederholantworten gelten unverändert. Ereignis, Outbox und Wiederholantwort werden atomar geschrieben. Bei unbestätigter Speicherung hält die Oberfläche Person, Sollversion, Inhalt und Schlüssel fest. Nach erneuter Anmeldung mit demselben Konto den Freigabestand laden und identisch wiederholen. Bestätigte historische Antworten ändern niemals den aktuellen Freigabestand. Versionskonflikte verlangen erneutes Laden / Fachprüfung. Eingabeänderungen setzen die ausdrückliche Bestätigung zurück; verspätete Antworten nach Abmeldung werden verworfen.

`GET /api/v1/packlists/{id}/release/export.json?user_id=...` liefert die vollständige eigene beziehungsweise ausdrücklich zugelassene personenbezogene Freigabegeschichte einschließlich Sollversion, aktueller Bewertung und unveränderten Entscheidungen. Geräteakten und Dokumentarchive werden weiterhin separat exportiert. Grenzen: 16 KiB pro Änderung, 2000 Freigabeereignisse pro Sollversion, 8 MiB pro Export; bei Überschreitung wird kein Teilarchiv ausgeliefert. Eine PDF-Unterschrift oder signierte Archivierung wird dadurch nicht behauptet.

## Upgrade auf Schema 32

Mit bisheriger laufender Programmversion ein neues konsistentes Backup erstellen. Danach neues Image bauen / laden, Dienst stoppen, ausdrücklich migrieren und Bereitschaft prüfen:

```sh
# Mit dem bisherigen Image, neues Sicherungsziel wählen:
docker compose run --rm --no-deps -T inventory backup --db /data/inventory.db --out /data/vor-schema32.db
# Danach neues Image bereitstellen:
docker compose stop inventory
docker compose run --rm --no-deps -T inventory migrate --db /data/inventory.db
docker compose up -d --wait
```

Durchgängig dieselben Compose-Dateien, Umgebungsvariablen und Datenvolumes verwenden; bei lokalem HTTP entsprechend `-f compose.local.yaml`. `--no-deps` verhindert vorzeitiges `init` mit dem neuen Schemastand. Migration ist wiederholbar, ergänzt drei Tabellen und erhält vorhandene Bestände, Dokumente und frühere Teilabnahmen. Vorhandene Datenbanken werden durch `init` nicht automatisch migriert; neue Installationen starten mit Schema 32. Für einen Rollback passendes früheres Programm und Vorabbackup verwenden; keine Schemazeilen oder Volumes löschen. TLS-/SMTP-/Compose-Konfiguration und Restic-Zugänge separat geschützt erhalten.

Die tatsächliche Abnahme auf dem NAS, echte Scanner, ClamAV, SMTP, externe Sicherungszeitplanung, Last und Wiederanlauf bleiben praktisch zu erproben. Freigabeverantwortlichkeit und tatsächliche Einsatzbedingungen sind betrieblich festzulegen.
