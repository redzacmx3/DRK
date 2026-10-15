# Design- und Funktionsvergleich: InvenTree und OpenBoxes

Stand: 16.10.2026. Analyse von Quellcode, Dokumentation und den Original-Screenshots in den Repositories; keine praktische Abnahme laufender Referenzinstallationen. Die untersuchten Entwicklungsversionen sind keine Aussage über die jeweils letzte stabile Veröffentlichung.

- InvenTree: Commit `2f139f94b4b684e45ff28eed7a3c5fc2cf875028`.
- OpenBoxes: Commit `94f393a240347209ad88fa6b48124e0238dd17ad` (`develop`).
- Vergleichsbasis DRK: `8aa9b7a`, Oberfläche für `v0.1.0-rc.4`, Schema 34.

Diese Analyse ist ein Gestaltungsvorschlag. Sie ergänzt keine Funktionen und verändert keine Datenmodelle, Berechtigungen oder Modulabhängigkeiten.

## Einordnung

InvenTree ist besonders als Vorbild für objektbezogene Inventarbedienung geeignet: Artikel, Bestandsobjekte, Detailbereiche und leistungsfähige Tabellen. OpenBoxes liefert passende Anregungen für standortbezogene Materialverwaltung, Gesundheitslogistik und geführte Lagerprozesse.

| Bereich | InvenTree: beobachtete Ansätze | OpenBoxes: beobachtete Ansätze | Übertragung auf DRK |
| --- | --- | --- | --- |
| Navigation | Hauptbereiche, Brotkrumennavigation, Kategorie-/Standortbäume und lokale Detailpanels | Hauptbereiche für Inventar, Produkte, Ein-/Ausgang, Beschaffung und Berichte; Standortkontext | Wenige stabile Hauptbereiche, zusätzliche Navigation innerhalb des ausgewählten Geräts oder Materials |
| Listen | Suche, serverseitige Filter, Sortierung, Blättern, Spaltenauswahl, gespeicherte Filtergruppen, Zeilenaktionen | Filterleisten und Tabellen für Lagerbewegungen, getrennte Arbeitslisten | Einheitliche Tabellen für Geräte und Material mit sichtbarem Filterzustand und Ergebnisumfang |
| Details | Eigene Objektseiten mit Panels für Stammdaten, Bestand, Historie, Anhänge und weitere passende Bereiche; optionale Vorschau seitlich | Produktbezogene Bestandskarte, Chargen und Transaktionslog; direkte Objektaktionen | Gerät einmal wählen und Grunddaten, Akte, Dokumente sowie Historie im selben Kontext bearbeiten |
| Dashboard | Zusammenstellbare Widgets, Zähler und direkte Aktionen | Standortbezogene Kennzahlen, Verfallsübersicht, Bestandsschwellen und offene Bewegungen | Handlungsorientierte Übersicht: überfällige Prüfungen, Sperren, Materialverfall und offene Aufgaben |
| Materialfunktionen | Bestandsobjekte mit Chargen/Seriennummern, Ablaufdaten, Rückverfolgung, Lieferanten und Beschaffung | Chargen/Verfall, Ein-/Ausgänge, Kommissionieren/Packen/Versenden, Mindest-/Melde-/Höchstbestände | Vorhandene Bestandsfunktionen sichtbar zusammenführen; Nachfüllbedarf und geführte Buchungen als Erweiterungen bewerten |
| Darstellung | Dichte Fachoberfläche, Hell-/Dunkelmodus, strukturierte Detailansichten | Helle Arbeitsfläche, Informationskarten, Tabellen und Statusdarstellung | Kompakte, ruhige Darstellung, klare Primäraktion, verständliche Statusbezeichnungen |

Die InvenTree-Dokumentation bezeichnet die Weboberfläche ausdrücklich als für große Displays ausgelegt und nicht für Mobilgeräte optimiert. Das Projekt bietet zusätzlich eine eigene mobile App. DRK soll seine direkt nutzbare mobile Weboberfläche erhalten; dichte Desktop-Tabellen benötigen dafür eine angepasste Darstellung.

## Empfohlene Informationsstruktur

Hauptnavigation:

1. Übersicht
2. Inventar
3. Material
4. Standorte
5. Packlisten, sofern aktiviert
6. Verwaltung

Scanner und Import sind unmittelbar erreichbare Aktionen in den passenden Bereichen. Ein eigener Scan-Einstieg kann zusätzlich erhalten bleiben. Geräteakten und fachliche Nachweise werden innerhalb des betreffenden Geräts geöffnet. Eine zentrale bereichsübergreifende Fristen- und Aufgabenliste bleibt sinnvoll.

Gerätedetail: **Übersicht · Grunddaten · Geräteakte/Prüfungen · Dokumente · Historie**. Geräteakte und Dokumente erscheinen entsprechend Modulauswahl und Rechten. Ein reines Equipmentobjekt braucht weiterhin nur die Grunddateninventarisierung.

Materialdetail: **Übersicht · Bestände/Chargen · Bewegungen · Kennungen**. Wareneingang, Entnahme und Umlagerung beginnen mit dem bereits gewählten Material. Für jede Buchung bleiben Ziel, Menge und ausdrückliche Bestätigung erforderlich.

Ein verständlicher Status beschreibt seinen Umfang: „inventarisiert“, „Prüfung überfällig“, „gesperrt“ oder „Freigabe ungeklärt“. Ein positiver Inventar- oder Prüfstatus ersetzt keine fachliche Einsatzfreigabe.

## Abgleich mit dem vorhandenen DRK-Stand

| Ansatz | Vorhandene Grundlage | Noch zu ergänzen |
| --- | --- | --- |
| Objektbezogene Geräteakte | Grunddaten, Fachnachweise, Dokumente und Historie vorhanden | Gemeinsame Objektseite und lokale Tabs; technische Auswahl über mehrere globale Bereiche ersetzen |
| Tabellen und Suche | Kompakte Inventarliste mit lokaler Suche/Artfilter; begrenzte Serverübersicht | Serverseitige Suche, Sortierung, Pagination und weitere Filter; gefilterten Kontext beim Öffnen/Zurückkehren erhalten |
| Schnelles Anlegen | Geräteanlage per API und Excel-/CSV-Erfassung vorhanden | Sichtbare einfache Webmaske „Gerät anlegen“ für einzelne Gegenstände |
| Materialkarte | Produkte/Kennungen, Chargen, Bestände, Buchungen und Journal vorhanden | Zusammenhängende Materialdetailseite und kontextbezogene Aktionen |
| Aufgabenübersicht | Fristen, Sperren, Meldebewertungen und Erinnerungen vorhanden | Einheitlicher Einstieg mit direkten Verweisen auf die betroffenen Objekte; modulabhängige Anzeige |
| Standortnavigation | Betriebsstätten, Lagerorte und zugeordnete Bestände vorhanden | Eigene übersichtliche Standortansicht; Hierarchie und Kategorien gesondert modellieren |
| Nachfüllbedarf | Versionierte Packlisten und Fehlmengen vorhanden | Allgemeine Mindest-/Meldebestände und Nachfülllisten über Packlisten hinaus |
| Verfallsorientierte Entnahme | Chargen/Verfall und Nutzbarkeitsprüfung vorhanden | FEFO-Auswahlhilfe und gesonderte Anbruchregeln; nicht als bereits fertige Funktion ausweisen |
| Vorschau und Bilder | Aufklappbare Inventardetails und dokumentbezogene Anhänge vorhanden | Optionale seitliche Schnellansicht und eigens gestaltete Objektbilder; kein vorhandenes Fotoalbum behaupten |

## Sinnvolle Reihenfolge

**Zuerst Bedienbarkeit:** Geräteobjekt mit lokalen Tabs, einfache Einzelanlage und einheitliche Listen mit serverseitiger Suche/Filterung/Blättern. Die Inventarisierung bleibt eigenständig und für Laien überschaubar.

**Danach Material und Aufgaben:** Materialkarte, geführte Eingangs-/Entnahme-/Umlagerungsdialoge und ein Dashboard mit tatsächlichem Handlungsbedarf. Erst anschließend zusätzliche Bestandsregeln, Nachfülllisten, Kategorien und Standortbaum.

**Später bei Bedarf:** Gespeicherte Ansichten, anpassbare Spalten, Sammelaktionen, konfigurierbare Dashboard-Widgets und Beschaffung. Fertigung und vollständige Einkaufs-/Versandprozesse gehören zu den Referenzprojekten, erweitern aber den bisherigen DRK-Kern erheblich.

Die bestehende Installation als einzelnes Docker-Image mit lokalem HTTP oder konfigurierbarem HTTPS bleibt die technische Grundlage. Die genannten Gestaltungsideen erfordern keinen Wechsel des Backend-Stacks.

## Nachvollziehbare Quellen

InvenTree:

- [UI-Struktur, Dashboard und Mobilgrenzen](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/docs/docs/concepts/ui/index.md)
- [Tabellen, Filter und Zeilenaktionen](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/docs/docs/concepts/ui/tables.md)
- [Seitliche Vorschau](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/docs/docs/concepts/ui/preview_panels.md)
- [Artikel-Detailbereiche im Frontend](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/src/frontend/src/pages/part/PartDetail.tsx)
- [Bestandsdetailbereiche im Frontend](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/src/frontend/src/pages/stock/StockDetail.tsx)
- [Chargen/Serienverfolgung](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/docs/docs/stock/traceability.md)
- [Verfallsfunktionen](https://github.com/inventree/InvenTree/blob/2f139f94b4b684e45ff28eed7a3c5fc2cf875028/docs/docs/stock/expiry.md)

OpenBoxes:

- [Projekt und Gesundheitslogistik](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/README.md)
- [Funktionsübersicht](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/docs/about/features.md)
- [Dashboard-Screenshot](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/docs/assets/img/dashboard.png)
- [Dashboard mit Kennzahlen und Widgets](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/src/js/components/dashboard/Dashboard.jsx)
- [Geführter Materialausgang](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/src/js/components/stock-movement-wizard/StockMovement.jsx)
- [Standort- und rechteabhängiges Menü](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/grails-app/services/org/pih/warehouse/api/MegamenuService.groovy)
- [Mindest-, Melde- und Höchstbestände](https://github.com/openboxes/openboxes/blob/94f393a240347209ad88fa6b48124e0238dd17ad/grails-app/domain/org/pih/warehouse/inventory/InventoryLevel.groovy)
