# Besondere Produktpflichten (Schema 28)

Die neue Bewertung dokumentiert pro Gerät, welche besonderen Pflichten anwendbar sind und auf welche Nachweise sich die fachliche Prüfung bezieht. Eine Produktklassifizierung wird niemals aus Namen, Modell, Berufsbezeichnung oder Benutzerrolle abgeleitet. Die Erfüllung der Pflichten und die vollständige Einsatzfreigabe bleiben gesondert abzunehmen.

## Bedienung

Unter **Besondere Produktpflichten** das Gerät auswählen und die Bewertung laden. Geräteverantwortliche der tatsächlichen Betriebsstätte können neue Bewertungen und Widerrufe speichern; andere berechtigte Leser sehen die Historie ihres Gerätebereichs. Administratorrecht allein erlaubt keine Bewertung.

Eine zuvor geprüfte und freigegebene Produktunterlage ausdrücklich auswählen. Für jeden der acht Bereiche die Anwendbarkeit erfassen:

- Anlage 1: Funktionsprüfung am Betriebsort und besondere Einweisung.
- Anlage 2: besondere Kontrollpflichten.
- Medizinproduktebuch und dessen Zugänglichkeit.
- Wartung nach Herstellervorgaben.
- IT-Sicherheitsüberprüfungen.
- Aufbereitung und Hygiene.
- Besondere Anwendergruppen und Ausnahmen.
- Weitere produktspezifische Pflichten.

**Ungeklärt** ist die Voreinstellung. **Erforderlich** verlangt eine konkrete Nachweisreferenz; **begründet nicht anwendbar / Ausnahme** verlangt eine fachliche Begründung der Nichtanwendbarkeit beziehungsweise Ausnahme. Eine bloße Auswahl bedeutet keine automatische rechtliche Einstufung. Auch für ungeklärte Bereiche ist der aktuelle Prüfstand zu begründen. Alle acht Bereiche müssen genau einmal erfasst werden.

Prüfnachweis, Qualifikation der prüfenden Person, betrieblicher Auftrag und Begründung angeben und die fachliche Prüfung ausdrücklich bestätigen. Die Befristung beträgt höchstens 366 Tage; das ist eine technische Obergrenze dieses Nachweises, keine gesetzliche Prüffrist. Keine Patientendaten eintragen; notwendige Referenzen genügen.

Ein **Widerruf** enthält die Prüfbegründung, aber keine neue Unterlage, Anwendbarkeit oder Gültigkeitsbestätigung. Bestehende Ereignisse werden ergänzt und niemals überschrieben. Bei unbestätigter Speicherung hält die Oberfläche Inhalt, Gerät und Wiederholungsschlüssel fest; die unveränderte Anfrage erneut senden. Nach einer erneuten Anmeldung mit demselben Konto das Gerät über **Produktpflichten laden** wiederherstellen. Bei Versionskonflikten neu laden und fachlich abgleichen.

## Aussage und Grenzen

`reviewed` bedeutet: Anwendbarkeit und Nachweisreferenzen vollständig erfasst, ausdrückliche Fachprüfung dokumentiert und aktuell. `unknown` bedeutet: keine aktuelle Bewertung, mindestens ein ungeklärter Bereich, Ablauf, geänderte Geräteidentifikation/Standort oder zurückgezogene Produktunterlage. `blocked` kennzeichnet einen ausdrücklich widerrufenen Nachweis.

Unterlagen werden anhand ihrer gespeicherten SHA-256-Prüfsumme gebunden und müssen weiterhin freigegeben sein. Die Geräte- und Nachweisversion werden beim Schreiben geprüft. Ein Widerruf ist auch dann möglich, wenn die Unterlage inzwischen nicht mehr freigegeben ist. Exakte Wiederholantworten bleiben erhalten, werden aber erst nach erneuter Prüfung von Sitzung und aktuellen Standortrechten ausgegeben.

Herstellerwartung kann inzwischen ausdrücklich mit einem vorhandenen Wartungsereignis aus derselben Geräteakte verknüpft werden. Die automatische Vorprüfung löst dieses Ereignis und seine freigegebene Nachweisunterlage auf. Funktions-, Einweisungs- und weitere besondere Nachweise sowie die abschließende fachliche Erfüllungsabnahme bleiben offen. Auch acht begründete Ausnahmen erzeugen **keine** Einsatzfreigabe. Die Packlistenbewertung enthält den aktuellen Prüfstand pro Gerät. Die Komponente `product_duties` bleibt bis zur gesonderten Erfüllungsabnahme ungeklärt; ein Widerruf oder eine blockierte Wartungsvorprüfung blockiert sie. Material-, Funktions- und Personenbewertungen werden weiterhin separat ausgewiesen, `overall_readiness` bleibt `not_assessed`.

## API, Archiv und Betrieb

`GET /api/v1/devices/{id}/product-duties` liefert Bewertung, die letzten 100 Ereignisse, das aktuelle Schreibrecht sowie die Wartungsereignisse des Geräts. `POST` erwartet `action=review|revoke`, `expected_version`, `expected_device_version` und die vier Prüfbegründungen. Eine Bewertung enthält zusätzlich `document_id`, `valid_until` als RFC3339, `reviewed=true` und acht `rules` mit `kind`, `applicability=required|exempt|unknown`, `reason` sowie gegebenenfalls `evidence`. Für erforderliche `manufacturer_maintenance` kann zusätzlich `event_version` ausdrücklich eine Wartung aus derselben Geräteakte binden; für andere Bereiche und Ausnahmen ist die Ereignisbindung derzeit unzulässig. Alte Einträge ohne Bindung bleiben gültige Anwendbarkeitsbewertungen, lösen aber keinen Wartungsnachweis auf. Gültige Bereichscodes: `annex_1`, `annex_2`, `device_book`, `manufacturer_maintenance`, `it_security`, `reprocessing`, `special_user_group`, `other`.

Origin, CSRF und gemeinsamer Idempotenzschlüsselraum gelten wie für andere Änderungen. Höchstens 2000 Ereignisse pro Gerät, 16 KiB pro Änderungsanfrage. Ereignis, Outbox und Wiederholantwort werden atomar gespeichert. Der Geräteaktenexport enthält die Ereignisse sowie den aktuellen Vorprüfungsstand in JSON und lesbarem HTML; die bestehenden Exportgrenzen gelten weiterhin, es gibt keine Teilarchive.

Vor Update ein neues konsistentes Backup erzeugen und ausdrücklich auf Schema 28 migrieren. Datenvolumes, frühere Nachweise und Anhänge erhalten. Die Freigabe von Produktunterlagen benötigt weiterhin die dokumentierte Scannerprüfung und eine getrennte fachliche Dokumentfreigabe. Reale Produktklassifizierung, Prüferqualifikation, Auftrag und Pilotabnahme sind betrieblich zu bestätigen.

## Wartungsnachweise vorprüfen

Bei erforderlicher Herstellerwartung in der Bewertung ein konkretes Wartungsereignis auswählen. Reparaturen und reine Serviceereignisse werden nicht als Wartung angeboten. Neben der Ereignisbindung bleibt die fachliche Nachweisreferenz erforderlich. Die Zuordnung erhält dieselbe unveränderliche Historie, Geräte-/Bewertungsversion und sichere Wiederholantwort wie die übrige Bewertung.

`checks` im Prüfbericht unterscheidet **record_found** (Ereignis mit Ergebnis `completed` und aktuell freigegebener, unverändert gebundener Nachweisunterlage aufgelöst), **unknown** (keine Bindung, fehlende Unterlage, Freigabeentzug, neuere Wartung oder Richtigstellung), **blocked** (zugeordnete Wartung fehlgeschlagen / Nacharbeit erforderlich oder Gerät aktuell gesperrt / außer Betrieb) und **not_applicable** (begründete Nichtanwendbarkeit erfasst). Die Vorprüfung gilt nur bei einer aktuellen vollständigen Anwendbarkeitsbewertung; eine zurückgezogene Produktgrundlage macht auch die Vorprüfung ungeklärt.

**record_found ist keine Abnahme.** Herstellerintervall, passende Maßnahmen und Umfang, Prüferbefähigung und tatsächliche Erfüllung sind anschließend fachlich zu prüfen. Es wird keine Wartungsfrist aus einem Freitextdatum erfunden. Alte Ereignisse werden nicht automatisch dem jüngsten Nachweis zugeordnet. Ein neues `completed`-Ereignis hebt bestehende Gerätesperren nicht auf. Eine Richtigstellung verhindert die positive Auflösung des betroffenen Ursprungsnachweises; ein neuer passender Nachweis und eine neue bewusste Zuordnung sind erforderlich.

Diese Ergänzung bleibt auf Schema 28 und benötigt keine neue Migration. Vor dem Programmupdate trotzdem ein neues Backup erzeugen; vorhandene Ereignisse und exakte Wiederholantworten bleiben erhalten.
