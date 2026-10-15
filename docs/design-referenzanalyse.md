# Design- und Funktionsvergleich: InvenTree, OpenBoxes und Shelf

Stand: 16.10.2026. Analyse von Quellcode, Dokumentation und den Original-Screenshots in den Repositories; keine praktische Abnahme laufender Referenzinstallationen. Die untersuchten Entwicklungsversionen sind keine Aussage über die jeweils letzte stabile Veröffentlichung.

- InvenTree: Commit `2f139f94b4b684e45ff28eed7a3c5fc2cf875028`.
- OpenBoxes: Commit `94f393a240347209ad88fa6b48124e0238dd17ad` (`develop`).
- Shelf: Commit `dd9b66f718f35c960b64329e4894795cacc16dc4`. Layout anhand UI-Code und Projektabbildung geprüft; keine laufende Shelf-Installation abgenommen.
- Vergleichsbasis DRK: `8aa9b7a`, Oberfläche für `v0.1.0-rc.4`, Schema 34.

Diese Analyse unterscheidet Empfehlungen und Umsetzung. Das erste Paket ist ab `v0.1.0-rc.5` mit Schema 35 umgesetzt: [Objektansicht, Einzelanlage, Inventarsuche und Fotoverwaltung](inventar-objektansicht.md). Ausleihe, Reservierungen, Scan-Inventur und weitere vorgeschlagene Funktionen bleiben nachfolgende Pakete. Der folgende Abgleich berücksichtigt das erste Umsetzungspaket; spätere Empfehlungen sind weiterhin offen.

## Einordnung

Shelf ist das bevorzugte Vorbild für die einfache Geräte-/Equipmentbedienung und die visuelle Objektübersicht. InvenTree ist besonders als Vorbild für objektbezogene Inventarbedienung geeignet: Artikel, Bestandsobjekte, Detailbereiche und leistungsfähige Tabellen. OpenBoxes liefert passende Anregungen für standortbezogene Materialverwaltung, Gesundheitslogistik und geführte Lagerprozesse.

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
| Objektbezogene Geräteakte | Gemeinsame Objektseite mit Grunddaten, Fachnachweisen, Dokumenten und Historie in lokalen Reitern ab rc.5 umgesetzt | Weitere Vereinfachung einzelner Fachformulare nach Erprobung |
| Tabellen und Suche | Serverseitige Inventarsuche, Art-/Standortfilter, Sortierung und Pagination mit erhaltener Listenauswahl ab rc.5 umgesetzt | Weitere Listen, gespeicherte Ansichten und Spaltenkonfiguration |
| Schnelles Anlegen | Einzelanlage mit drei Mindestangaben im Browser ab rc.5 umgesetzt; Excel/CSV weiterhin vorhanden | Bedienungsabnahme mit echten Erfassungsrunden |
| Materialkarte | Produkte/Kennungen, Chargen, Bestände, Buchungen und Journal vorhanden | Zusammenhängende Materialdetailseite und kontextbezogene Aktionen |
| Aufgabenübersicht | Fristen, Sperren, Meldebewertungen und Erinnerungen vorhanden | Einheitlicher Einstieg mit direkten Verweisen auf die betroffenen Objekte; modulabhängige Anzeige |
| Standortnavigation | Betriebsstätten, Lagerorte und zugeordnete Bestände vorhanden | Eigene übersichtliche Standortansicht; Hierarchie und Kategorien gesondert modellieren |
| Nachfüllbedarf | Versionierte Packlisten und Fehlmengen vorhanden | Allgemeine Mindest-/Meldebestände und Nachfülllisten über Packlisten hinaus |
| Verfallsorientierte Entnahme | Chargen/Verfall und Nutzbarkeitsprüfung vorhanden | FEFO-Auswahlhilfe und gesonderte Anbruchregeln; nicht als bereits fertige Funktion ausweisen |
| Vorschau und Bilder | Erkennungsfoto mit eigener Versionshistorie und Rechteprüfung ab rc.5 umgesetzt | Optionale seitliche Schnellansicht, Bildvorschauen in weiteren Listen und Bildgalerie |

## Sinnvolle Reihenfolge

**Zuerst Bedienbarkeit:** Geräteobjekt mit lokalen Tabs, einfache Einzelanlage und einheitliche Listen mit serverseitiger Suche/Filterung/Blättern. Die Inventarisierung bleibt eigenständig und für Laien überschaubar.

**Danach Material und Aufgaben:** Materialkarte, geführte Eingangs-/Entnahme-/Umlagerungsdialoge und ein Dashboard mit tatsächlichem Handlungsbedarf. Erst anschließend zusätzliche Bestandsregeln, Nachfülllisten, Kategorien und Standortbaum.

**Später bei Bedarf:** Gespeicherte Ansichten, anpassbare Spalten, Sammelaktionen, konfigurierbare Dashboard-Widgets und Beschaffung. Fertigung und vollständige Einkaufs-/Versandprozesse gehören zu den Referenzprojekten, erweitern aber den bisherigen DRK-Kern erheblich.

Die bestehende Installation als einzelnes Docker-Image mit lokalem HTTP oder konfigurierbarem HTTPS bleibt die technische Grundlage. Die genannten Gestaltungsideen erfordern keinen Wechsel des Backend-Stacks.

## Ergänzung: Shelf als Vorbild für Geräte und Equipment

Shelf richtet den Einstieg auf physische Gegenstände, ihren Ort und ihre aktuelle Zuordnung aus. Die Quellen zeigen eine zu Symbolen einklappbare Seitenleiste mit Workspace-Auswahl und Benutzermenü, Objektüberschriften mit Vorschaubild und Status sowie lokale Reiter für Übersicht, Aktivitäten, Buchungen und Erinnerungen. Die Übersicht kombiniert Stammdatenkarten mit Informationen zu Zuordnung, Mengen und Scans. Bearbeiten und weitere Aktionen bleiben am ausgewählten Objekt erreichbar. Der Quellcode enthält responsive Layouts; das ist keine praktische Abnahme aller mobilen Abläufe.

Für DRK empfiehlt sich damit eine klare Kombination: **Shelf für visuelle Einfachheit und Equipmentabläufe, InvenTree für Tabellen und Objektakten, OpenBoxes für Material-/Lagerprozesse.**

| Shelf-Funktion | Nutzen für DRK | Einordnung zum bisherigen Stand |
| --- | --- | --- |
| Objektfotos und Bildvorschau | Gegenstände auch ohne Kenntnis der Inventarnummer wiedererkennen | Eigenständige Fotoansicht mit Versionshistorie ab rc.5 umgesetzt; weitere Bildvorschauen bleiben offen |
| Zuordnung / Custody | Sichtbar machen, wer einen Gegenstand übernommen hat; Ausgabe und Rücknahme nachvollziehen | Neues optionales Ausleihe-/Zuordnungsmodul. Fachrechte, Einweisungen und Betriebsverantwortung bleiben getrennt |
| Reservierungen / Bookings | Ausrüstung für einen Zeitraum planen, Überschneidungen erkennen, Rückgabe und Überfälligkeit verfolgen | Neue fachliche Funktion mit Kalender, Verfügbarkeitsregeln und Übergabe-/Rückgabehistorie; keine bloße neue Gerätekennzeichnung |
| Kits | Zusammengehörige Ausrüstung gemeinsam anzeigen und verwalten, etwa Gerät plus Zubehör | Mit vorhandenen Packlisten abstimmen. Ein physisches Set und eine versionierte Sollpackliste sind unterschiedliche Konzepte |
| QR-/Barcode-Arbeitsabläufe | Vom Scan zur Objektseite, Standortänderung, Übergabe oder Inventurrunde gelangen | Scannerauflösung und Etiketten sind vorhanden. Gerätebezogene Direktaktionen und Sammelabläufe ergänzen; Rechte und ausdrückliche Bestätigung erhalten |
| Scanbasierte Inventurrunden / Audits | Erwartete Gegenstände systematisch abhaken, fehlende und unerwartete Gegenstände ausweisen | Zusätzliche körperliche Geräteinventur. Vorhandene Materialzählungen und fachliche Gerätekontrollen haben andere Zwecke |
| Kategorien, Tags und Zusatzfelder | Geräte nach Art, Einsatzgruppe oder lokalen Merkmalen übersichtlich ordnen | Zusätzliche Klassifikation und strukturierte Zusatzfelder. Fachlich vorgeschriebene Nachweise bleiben in ihren definierten Feldern |
| Konfigurierbare Inventartabelle | Fotos, Standorte, Zuordnung oder relevante Zusatzfelder gezielt anzeigen; Auswahl je Nutzer speichern | Ergänzt serverseitige Suche/Filter, Spaltenverwaltung und spätere Sammelaktionen |
| Modelle und mengenverfolgte Assets | Gleichartige Einzelgeräte einem Modell zuordnen; Verbrauchsgegenstände mengenmäßig verwalten | Für Stammdatenvorlagen interessant. Das bestehende DRK-Modell für Produkte, Chargen und Bestände gesondert erhalten und prüfen |

Die Shelf-Dokumentation unterscheidet CSV-Erstanlage und einen eigenen Bulk-Update-Ablauf. Für DRK wäre ein späteres CSV-Update eine gesonderte Funktion mit Vorschau, Identitätsabgleich und Versionsprüfung. Der bestehende Erstimport darf dadurch keine vorhandenen Objekte stillschweigend überschreiben.

Ein QR-Code als direkter Link kann eine Anmeldung erfordern. Für einen möglichen DRK-Direktlink müssen konfigurierbare HTTP-/HTTPS-Adresse, Authentifizierung und Objektberechtigungen berücksichtigt werden; ein abfotografiertes Etikett gewährt keine zusätzlichen Rechte.

Shelf-Buchungen und andere Bereiche hängen im untersuchten Code von Rollen und Workspace-/Tarifeinstellungen ab. Die Auffindbarkeit im öffentlichen Repository belegt keine kostenlose Verfügbarkeit in jedem gehosteten Tarif. Das begleitende mobile App-Projekt hat laut README eine gesonderte Lizenz; die vorliegende Analyse verwendet keine fremden Implementierungen.

### Aktualisierte Prioritäten

1. **Geräteübersicht und Objektakte:** sichtbare Einzelanlage, optionales Foto, lokale Tabs, klare Direktaktionen und vollständig durchsuchbare Listen. Das bleibt der nächste empfohlene Umsetzungsschritt.
2. **Inventur und Organisation:** scanbasierte Geräteinventur, Standort-/Kategorieansichten und verständliche Set-Zuordnung.
3. **Optionales Ausleihemodul:** zuerst dauerhafte Zuordnung sowie Ausgabe/Rücknahme, darauf aufbauend zeitbezogene Reservierungen und Konfliktprüfung. Ein eigener abschaltbarer Funktionsbereich passt zur bisherigen Modulstrategie.
4. **Materialentwicklung:** die bereits empfohlenen Materialkarten, geführten Buchungen, Nachfülllisten und FEFO-Hilfen nach OpenBoxes-/InvenTree-Vorbild weiterverfolgen.

Reserviert, ausgegeben, zurückgegeben oder gefunden sind organisatorische Zustände. Die fachliche Gerätesperre und Einsatzfreigabe behalten ihre eigene Bedeutung.

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

Shelf:

- [Projekt, Funktionen und Lizenzabgrenzung](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/README.md)
- [Seitenleiste mit Workspace und Benutzermenü](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/webapp/app/components/layout/sidebar/app-sidebar.tsx)
- [Menüeinträge und rollen-/tarifabhängige Sichtbarkeit](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/webapp/app/hooks/use-sidebar-nav-items.tsx)
- [Objektkopf mit Bild, Status und lokalen Reitern](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/webapp/app/routes/_layout%2B/assets.%24assetId.tsx)
- [Inventartabelle und erweiterte Such-/Sortierfunktionen](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/docs/advanced-index/index.md)
- [Spaltenkonfiguration und Objektbilder](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/docs/advanced-index/asset-index-settings.md)
- [CSV-Erstanlage, Bulk-Updates und Mengenobjekte](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/docs/asset-import.md)
- [Reservierungs- und Überschneidungsregeln](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/docs/booking-conflict-queries.md)
- [Scanbasierte Geräteinventur im Web](https://github.com/Shelf-nu/shelf.nu/blob/dd9b66f718f35c960b64329e4894795cacc16dc4/apps/webapp/app/routes/_layout%2B/audits.%24auditId.scan.tsx)
