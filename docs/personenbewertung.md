# Personenbezogene Gerätebewertung (Schema 25)

Die neue Ansicht prüft die ausdrücklich erfassten personenbezogenen Anforderungen für ein konkretes Benutzerkonto und Gerät. Sie verbindet Einweisungsereignisse mit stabilen Benutzer-IDs und berücksichtigt befristete Qualifikationsnachweise sowie Widerrufe. App-Rollen, freie Personenangaben und Berufsbezeichnungen werden nicht automatisch als Befähigungsnachweis gewertet.

## Bedienung

In „Personenbezogene Gerätebewertung“ Gerät auswählen und Bewertung laden. Geräteverantwortliche können Konten ihrer Betriebsstätte auswählen und Nachweise erfassen. Andere berechtigte Leser sehen ausschließlich die eigene personenbezogene Bewertung. Nach einer Auswahländerung neu laden; veraltete Personen- oder Geräteversionen verhindern das Speichern.

1. Pflichten anhand bestätigter Produktunterlagen festlegen: Einweisung ausdrücklich erforderlich oder begründete Ausnahme; mindestens einen notwendigen Qualifikationscode eintragen, etwa `basic_competence`. Codes sind selbst festgelegte Kategorien, keine vom Programm bestätigte medizinische Qualifikation. Umfang, besondere Produktpflichten und Ausnahmen fachlich bestimmen.
2. Qualifikation einem konkreten Konto und Gerät zuordnen. Nachweisreferenz, bestätigten Gültigkeitsbeginn und Endtag eintragen. Der Endtag gilt einschließlich in `Europe/Berlin`. Ein zukünftiger Beginn oder unbestätigte unbegrenzte Gültigkeit wird nicht als aktueller Nachweis angelegt. Nachweise können ausdrücklich widerrufen werden, auch nachdem das Konto deaktiviert wurde.
3. Falls erforderlich, ein bestehendes Einweisungsereignis ausdrücklich dem ausgewählten Konto zuordnen. Die Freitextangabe der eingewiesenen Person ersetzt diesen Abgleich nicht. Datum, Gerät/Bauart und belegte Übertragbarkeit des ursprünglichen Ereignisses bleiben erhalten. Fehlerhafte Kontozuordnungen durch einen ergänzenden Widerruf aufheben.

Jeder Vorgang verlangt Nachweisreferenz, Qualifikationsreferenz der prüfenden Person, betrieblichen Auftrag und Begründung. Damit wird eine dokumentierte menschliche Prüfung erfasst; die Software verifiziert externe Referenzen und tatsächliche Befähigung nicht selbständig. Änderungen an den Gerätegrunddaten verlangen einen erneuten Abgleich der Pflichtbewertung und personenbezogenen Nachweise. Daten und Ereignisse werden nicht überschrieben.

## Ergebnis und Grenzen

`unknown`: Pflichtbewertung, konkreter Nachweis oder Einweisungszuordnung fehlt beziehungsweise ein Einweisungsanhang ist nicht freigegeben. `blocked`: Konto inaktiv/außerhalb des Betriebsstättenbereichs oder ein erforderlicher Qualifikationsnachweis ist widerrufen beziehungsweise am Bewertungstag nicht gültig. `recorded_requirements_met`: ausschließlich die ausdrücklich erfassten personenbezogenen Anforderungen sind erfüllt.

Alle Ergebnisse behalten `overall_readiness=not_assessed`. Einweisungsdokumente beginnen in Quarantäne; nach aktueller Malwareprüfung und ausdrücklicher fachlicher Freigabe kann eine erforderliche Einweisung berücksichtigt werden. Ein Freigabeentzug wird unmittelbar bei der Bewertung berücksichtigt. Siehe [Dokumentprüfung](dokumentpruefung.md). Eine belegte Ausnahme ist ausdrücklich zu erfassen und nicht aus fehlenden Einweisungen abzuleiten.

Die Packlistenbewertung enthält zusätzlich die konkrete Benutzerprüfung für das angemeldete Konto. Ihre bestehende technische, Material- und physische Bewertung bleibt getrennt. Qualifikationsereignisse ändern weder Gerätekontrollen noch Bestände, Sperren oder physische Prüfnachweise. Die vollständige Einsatzfreigabe benötigt weiterhin bestätigte produktspezifische Pflichten, die Dokumentfreigabe und fachliche Abnahme. Insbesondere besondere Anforderungen etwa nach Anlage 1, zugängliche Gebrauchsanweisungen und konkrete Funktionsprüfungen werden nicht allein durch Qualifikationscodes erfüllt. Fachlich bestätigte Regeln und dokumentierte reale Vorgänge bleiben erforderlich.

## Rechte, Nachweise und Betrieb

Schreiben: `device_manager` an der Betriebsstätte des Geräts; Administratorrecht allein genügt nicht. Lesen: üblicher Gerätebereich und eigene Benutzer-ID; Geräteverantwortliche dürfen die personenbezogenen Nachweise ihrer Geräte prüfen. Historische Nachweise bleiben erhalten, auch wenn das Konto später inaktiv oder aus dem Bereich entfernt ist. Neue Befähigungs-/Einweisungsnachweise benötigen ein aktives Konto im betreffenden Bereich.

API: `GET /api/v1/devices/{id}/usage` mit optional `user_id`; `POST` auf denselben Pfad mit `action=policy|qualify|revoke|bind|unbind`, aktueller Personenrevision `expected_version`, Geräteversion `expected_device_version`, Origin/CSRF und Idempotenzschlüssel. Nach unklarer Antwort unverändert wiederholen; die aktuellen Schreibrechte werden vor Wiederholantworten geprüft. Der Browser hält Entwürfe ausschließlich im Tab-Arbeitsspeicher. Nach Schließen oder Neuladen zuerst den tatsächlichen Stand kontrollieren.

Grenzen: höchstens 2000 personenbezogene Ereignisse je Gerät und 500 aktuell zugeordnete Konten in der Auswahl; Überschreitungen werden vollständig abgelehnt. Sicherungen umfassen Personenereignisse und Wiederholantworten. Vor Upgrade ein neues konsistentes Backup erzeugen und ausdrücklich auf Schema 28 migrieren; Datenvolumes erhalten. Personenbezogene Angaben auf notwendige Nachweisreferenzen begrenzen; Datenschutz, Aufbewahrung und reale Pilotabnahme organisatorisch festlegen.
