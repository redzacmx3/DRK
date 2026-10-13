# Gebrauchsanweisungszugang und gemeinsame Bewertung (Schema 27)

Für ein konkretes Benutzerkonto wird dokumentiert, ob die passende freigegebene Gebrauchsanweisung am Geräteort tatsächlich zugänglich und in einer verständlichen Sprache verfügbar ist. Ein Downloadrecht oder eine zugeordnete Einweisung allein belegt diesen Zugang nicht. Geräteverantwortliche prüfen die tatsächliche Situation und die Zuordnung der Unterlage; die Software erkennt deren fachliche Eignung nicht automatisch.

## Bedienung

In „Personenbezogene Gerätebewertung“ Gerät und Konto auswählen und Bewertung laden. Unter „Gebrauchsanweisungszugang“ eine bereits freigegebene Unterlage wählen, deren Version und verständliche Sprache angeben und tatsächlichen Zugang ausdrücklich bestätigen. Nachweisreferenz, Qualifikation der prüfenden Person, betrieblicher Auftrag und Begründung dokumentieren.

Eine Bestätigung gilt höchstens 24 Stunden ab Erfassung. Diese technische Betriebsvorgabe begrenzt die Aussage über eine veränderliche Zugangssituation; sie ist keine gesetzliche Frist. „Gültig bis“ wird in lokaler Browserzeit eingegeben und als eindeutiger Zeitpunkt übertragen. Genau am Ablaufzeitpunkt ist die Bestätigung nicht mehr aktuell. Eine neue Prüfung ergänzt einen neuen Nachweis. Nicht verfügbare oder ungeeignete Unterlagen zunächst prüfen/freigeben, siehe [Dokumentprüfung](dokumentpruefung.md).

Ein Entzug des tatsächlichen Zugangs wird als eigener Widerruf dokumentiert. Frühere Nachweise bleiben erhalten. Auch ausgeschiedene Konten können zu vorhandenen Bestätigungen widerrufen werden; neue Bestätigungen erfordern ein aktives Konto im aktuellen Gerätebereich. Änderungen an Geräteidentifikation, Lagerort oder Betriebsstättenbereich verlangen eine neue Zugangsprüfung. Wird die Dokumentfreigabe entzogen, ist die Zugangsbestätigung bei der nächsten Bewertung nicht mehr ausreichend.

Ergebnisse: `confirmed` bestätigt ausschließlich den befristet dokumentierten Zugang; `unknown` bedeutet fehlender/abgelaufener Nachweis, veränderte Geräte-/Standortbasis oder nicht freigegebene Unterlage; `blocked` bedeutet ausdrücklicher Widerruf beziehungsweise nicht aktives Konto im Gerätebereich. Es bleibt `overall_readiness=not_assessed`.

## Zusammengeführte Voraussetzungen

Die Packlistenbewertung ergänzt `operating_evidence` mit sechs getrennten Komponenten:

| Komponente | Bewertungsgrundlage |
|---|---|
| Material | Nutzbarer Sollbestand und eigene aktuelle physische Materialprüfung |
| Anwesenheit/Funktion/Zubehör | Aktueller physischer Gerätenachweis zur konkreten Sollversion |
| Gerätekontrollen | Inbetriebnahme, ausdrücklich bewertete Kontrollpflichten, Fristen, Sperren, Außerbetriebnahme und offene Meldebewertung |
| Personenqualifikation | Konkrete Einweisungszuordnung und gültige erforderliche Qualifikationen des angemeldeten Kontos |
| Gebrauchsanweisungszugang | Aktuelle personengebundene Zugangsbestätigung für jedes Sollgerät |
| Besondere Produktpflichten | Anwendbarkeit strukturiert dokumentierbar; Erfüllungsabnahme weiter offen, Widerruf blockiert |

`met` bedeutet ausschließlich, dass die erfasste Teilvoraussetzung erfüllt ist. `unknown`, `needs_check` und `blocked` bleiben getrennt sichtbar. Die gemeinsame Bewertung lautet `blocked` bei einem bekannten Hindernis, ansonsten `incomplete`; alte Sollversionen heißen `obsolete`. Sie vergibt noch keine Gesamtfreigabe. Fehlende besondere Produktpflichten werden nicht aus allgemeinen Qualifikationscodes, App-Rollen oder einer bloßen Zugangsbestätigung abgeleitet.

Die bisherigen Material- und Geräteergebnisse bleiben als eigene Felder erhalten. Zugangsnachweise ändern weder Geräteaktenversionen noch Bestände, technische Sperren oder physische Prüfbasiswerte. Die aktuelle Zusammenführung wird beim Lesen berechnet, ohne frühere Nachweise nachträglich umzuschreiben.

## Rechte, Nachweise und Schnittstellen

Schreiben benötigt `device_manager` an der aktuellen Betriebsstätte des Geräts; Administratorrecht allein genügt nicht. Andere Gerätebereichsleser sehen nur eigene Zugangsereignisse. Geräteverantwortliche können die Nachweise ihrer Geräte prüfen. Geräteaktenarchive enthalten die Zugangsereignisse entsprechend diesen Personenrechten in JSON und HTML; gewöhnliche Leser erhalten keine fremden Zugangsereignisse. Das Archiv enthält weiterhin keine Dateiinhalte.

`GET /api/v1/devices/{id}/manual-access` liefert die aktuelle Bewertung und die letzten 100 Ereignisse der eigenen Person; `user_id` für andere Personen ist nur Geräteverantwortlichen zugänglich. `POST` erwartet `action=confirm|revoke`, stabile `user_id`, Zugangsrevision `expected_version`, Geräteversion `expected_device_version`, Origin/CSRF und Idempotenzschlüssel sowie vollständige Prüfbezüge. Eine Bestätigung enthält außerdem `document_id`, `manual_version`, `language`, ausdrücklich `accessible=true` und `valid_until` als RFC3339-Zeitpunkt. Ein Widerruf enthält keine neue Unterlage, Sprache, Befristung oder Zugangsbestätigung.

Nach unklarer Antwort identischen Inhalt mit demselben Schlüssel wiederholen; aktuelle Rechte werden vor Wiederholantworten geprüft. Nach Versionskonflikt neu laden und abgleichen. Entwürfe liegen nur im Tab-Arbeitsspeicher; nach Neuladen zuerst den tatsächlichen Stand prüfen. Bis zu 2000 Zugangsereignisse je Gerät; weitere Änderungen werden vollständig abgewiesen. Sicherungen umfassen unveränderliche Ereignisse, Outbox und Wiederholantworten.

Vor Upgrade ein neues konsistentes Backup erzeugen und ausdrücklich auf Schema 30 migrieren. Bestehende Unterlagen, Bestände und physische Prüfnachweise bleiben erhalten. Reale Zugangsprüfung am Einsatzort und besondere Produktpflichten müssen vor produktiver Freigabe fachlich abgenommen werden.

Die Anwendbarkeit besonderer Produktpflichten und Nachweisreferenzen kann inzwischen [strukturiert dokumentiert](produktpflichten.md) werden. Ihre abschließende Erfüllungsabnahme steht weiter aus.
