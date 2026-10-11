# Rückrufe und Sicherheitsmaßnahmen (Schema 24)

Die Oberfläche erfasst zentrale Rückrufe mit Quelle, Titel, ausdrücklichem betroffenem Bereich, aktuellen Standorttreffern und unveränderlicher Maßnahmenhistorie. Die Treffer-Vorschau muss vor Aktivierung geprüft werden. Eine veränderte Trefferbasis verhindert die Aktivierung bis zur erneuten Vorschau. Auch ohne aktuelle Treffer kann ein Rückruf bewusst aktiviert werden, um spätere passende Eingänge zu sperren.

## Bereiche und Rechte

Material: vorhandenes Produkt mit optional einer ausdrücklich ausgewählten Charge. Ohne Charge sind alle Chargen des Produkts einschließlich später angelegter Chargen betroffen. Die Zuordnung basiert auf stabilen Produkt-/Chargen-IDs; gleiche Namen gelten nicht automatisch als dasselbe Produkt.

Geräte: exakt übereinstimmende Hersteller- und Modellfelder. Optional eine exakte Serien-/Chargenkennung oder ein inklusiver numerischer Serienbereich mit gleich langen ASCII-Ziffernfolgen. Beispielsweise `0001` bis `0099`; `1` oder `A001` werden dadurch nicht getroffen. Leere Serienfelder erfassen alle Geräte der Bauart. Fehlende, anders geschriebene oder unvollständige Geräteidentifikationen sind gesondert zu klären. Es gibt keine unscharfe Zuordnung oder automatische Bewertung eines Herstellerdokuments.

Globale Erstellung, Vorschau und Gesamtabschluss benötigen eigenständiges Administratorrecht **und** die betreffende Fachrolle (`material_manager` beziehungsweise `device_manager`) an mindestens einer Betriebsstätte. Diese Kombination ist die ausdrücklich globale Rückrufkoordination der Installation; ihre Übersicht enthält die Treffer aller Betriebsstätten. Ein Administratorkonto ohne Fachrolle erhält diese Befugnis nicht. Deshalb dieses kombinierte Recht nur den zuständigen Koordinatoren zuweisen.

Einzelmaßnahmen benötigen die passende Fachrolle an der betroffenen Betriebsstätte, auch bei einem globalen Koordinator. Andere Leser sehen ausschließlich ihre aktuellen Standorttreffer und standortbezogene Maßnahmenhistorie. Globale Quellen-/Abschlussreferenzen bleiben innerhalb eines für sie sichtbaren Rückrufs lesbar; Treffer anderer Standorte werden nicht ausgegeben. Nach einem eigenen Maßnahmenereignis bleibt die historische Sicht auch bei nicht mehr vorhandenem Bestand erhalten.

## Sperrwirkung und physischer Bestand

Aktive Materialrückrufe setzen die effektive Chargensperre. Entnahmen werden abgelehnt; vorhandene Mengen und Lagerzustände bleiben unverändert und nachvollziehbar. Passende neue Zugänge werden in Quarantäne gebucht, auch bei CSV-Zugängen in neu angelegte Chargen. Scanner, Bestand und Packlistenbewertung berücksichtigen die Sperre. Eine Umlagerung bleibt als physischer Vorgang möglich; sie hebt die zentrale Sperre nicht auf.

Passende Geräte erhalten eine unabhängige zentrale `recall`-Sperre in Akte, Bewertung, Export und Arbeitsübersicht. Sie gilt auch für später registrierte passende Geräte und kann nicht über die einzelne Gerätefreigabe geschlossen werden. Bestandene Kontrollen verändern sie nicht. Eine geänderte Geräteidentifikation kann die aktuelle Zuordnung verändern; bei vorhandenen technischen Nachweisen erzeugt der bestehende Ablauf dabei eine separate Quarantäne zur fachlichen Klärung.

## Maßnahmen und Abschluss

Jeder aktuelle Materialtreffer ist eine positive Bestandsposition aus Charge, Lagerort und Lagerzustand; jeder Gerätetreffer ist eine konkrete Akte. Ein Maßnahmenabschluss benötigt Nachweisreferenz, Begründung, aktuelle Rückrufversion und die geladene Trefferbasis. Mengen-, Bestandsversions- oder Geräteaktenänderungen machen einen vorherigen Maßnahmenabschluss für den aktuellen Treffer ungültig. Einzelmaßnahmen verändern keine Mengen oder technischen Nachweise. Rückgaben, Umlagerungen und Korrekturen separat im jeweiligen Journal dokumentieren.

Der gesamte Rückruf kann erst geschlossen werden, wenn alle aktuell vorhandenen Treffer einen gültigen Maßnahmenabschluss haben. Neue Treffer verhindern den Abschluss. Ein Abschluss ohne aktuelle Treffer benötigt ebenfalls eine bewusste Abschlussentscheidung und Referenz; er bestätigt keinen unprotokollierten physischen Verbleib. Anfangs-, Maßnahmen- und Abschlussereignisse speichern die damaligen Treffer unveränderlich in der Datenbank.

Ein Gesamtabschluss beendet ausschließlich diesen zentralen Rückruf. Andere Rückrufe, manuelle Chargensperren, Quarantäne, Defekte und Kontrollversagenssperren bleiben wirksam. Bereits in Quarantäne gebuchte Ware wird nicht automatisch verfügbar. Frühere physische Material-/Geräteprüfnachweise werden nach Schließen nicht wieder gültig; die Rückrufhistorie bleibt Teil der Prüfbasis. Die vollständige Einsatzfreigabe bleibt `not_assessed`.

Geschlossene Definitionen werden nicht geändert oder wieder geöffnet. Eine neue Sicherheitsmeldung wird als neuer Rückruf mit eigener Quelle erfasst. Behördenmeldungen erfolgen weiterhin außerhalb des Programms; gespeicherte Referenzen ersetzen keine Meldung.

## Technischer Betrieb

Vor Upgrade ein neues konsistentes Backup anlegen und ausdrücklich auf Schema 24 migrieren; bestehende Volumes erhalten. Die Definition, Ereignisse und Wiederholantworten sind unveränderlich. Sitzung und aktuelle Rechte werden auch vor einer identischen Wiederholantwort geprüft. Bei unklarer Antwort hält die Oberfläche Inhalt und Schlüssel im Tab-Arbeitsspeicher fest. Nach Neuladen zuerst den tatsächlichen Stand prüfen.

API: `POST /api/v1/recalls/preview` mit ausdrücklichem `selector`; `POST /api/v1/recalls` mit `action=create|complete|close`, Origin/CSRF und Idempotenzschlüssel; `GET /api/v1/recalls` mit bereichsgefilterten Treffern, Versionen und Historie. Erstellung benötigt `preview_basis`, Titel, Quelle, Nachweis und Begründung. Änderungen benötigen `id`, `expected_version`, `evidence` und `note`; Einzelmaßnahmen zusätzlich `target_key` und `basis_hash`.

Grenzen: 500 Rückrufe pro Übersicht, höchstens 2000 Treffer pro einzelnen Vorgang sowie 2000 sichtbare Treffer beziehungsweise Historieneinträge insgesamt; Überschreitungen werden vollständig abgelehnt. Die Oberfläche zeigt die letzten 100 sichtbaren Ereignisse je Rückruf ausdrücklich begrenzt; ältere Nachweise bleiben gesichert. Blätterfunktionen, Herstellerdatei-Auswertung und zusätzliche Serien-/UDI-Regeln sind spätere Erweiterungen. Hardware-, Last-, fachliche und produktive Abnahme stehen weiterhin aus.
