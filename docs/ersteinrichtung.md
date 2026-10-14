# Erste Einrichtung im Browser (Schema 34)

Eine neue, leere Installation führt beim ersten Aufruf durch die Einrichtung. Ein Administratorkonto muss dafür nicht mehr per CLI angelegt werden. Inventarisierung bleibt der Kern; zusätzliche Geräteakten und weitere Module werden im Assistenten ausdrücklich ausgewählt und lassen sich später in der Verwaltung ändern.

## Container starten und Einrichtung freischalten

Für den einfachen lokalen Docker-Test:

```sh
docker compose -f compose.local.yaml up -d --build --wait
```

Die konfigurierte Adresse öffnen, standardmäßig `http://localhost:8080`. Auf dem NAS die in Dockhand eingestellte HTTP-/HTTPS-Adresse verwenden. Die Einrichtung verlangt einen einmaligen Einrichtungscode: Damit kann nicht ein beliebiger Besucher das erste Administratorkonto übernehmen.

In Dockhand die Konsole des laufenden `inventory`-Containers öffnen und ausführen:

```sh
cat /data/setup-code.txt
```

Alternativ am Rechner:

```sh
docker compose -f compose.local.yaml exec -T inventory cat /data/setup-code.txt
```

Für das NAS-Compose bei allen Befehlen die eigene Variablendatei und `compose.nas.yaml` einsetzen. Den Code im Einrichtungsformular eingeben. Er ist vertraulich und gehört weder in Tickets noch in das Repository. Containerlogs enthalten nur den Pfad zur Codedatei. Die Datei wird mit Modus `0600` angelegt und bei einem Neustart mit noch offener Einrichtung wiederverwendet; es gibt kein Standardpasswort.

## Konfiguration und Prüfungen

Der Assistent prüft die erreichbare App-Konfiguration, Datenbank und das Datenverzeichnis. Er zeigt den gewählten Browser-Ursprung und HTTP-/HTTPS-Modus sowie Hinweise zu optionaler Malwareprüfung, SMTP und Hintergrundjobs. Fehler müssen vor dem Abschluss behoben werden. HTTP im lokalen Testnetz ist möglich; HTTPS-Zertifikate, Netzwerkbindung, Origin und externe Dienste werden weiterhin außerhalb der App in Compose eingerichtet.

Danach ein eigenes Administratorkonto mit sicherem Passwort festlegen, Betreiber, erste Betriebsstätte und ersten Lagerort erfassen und die Fachrechte für diesen Standort ausdrücklich auswählen. Im nächsten Schritt die benötigten Module auswählen. Standard für die erste Erfassung ist Inventarisierung. Geräteakten werden nur bei Bedarf hinzugefügt. Der Assistent prüft die [Modulabhängigkeiten](module.md), bevor er die Auswahl übernimmt. Die Zusammenfassung vor dem Abschluss kontrollieren; zusätzliche Benutzer und Rechte anschließend in der [Verwaltung](administration.md) einrichten.

Nach erfolgreichem Abschluss normal anmelden. Der Assistent wird dauerhaft geschlossen; der Code ist danach ungültig. Konten-, Organisations- und Modulkonfiguration werden gemeinsam gespeichert. Bestehende Daten und Nachweise werden dabei nicht ersetzt.

Die Prüfungen bestätigen keine produktive Abnahme von NAS, TLS-Proxy, SMTP-Postfachzustellung oder Malwareprüfung. Ein optionaler fehlender Scanner wird angezeigt und kann niemals eine erforderliche Dokumentprüfung überspringen. Für echte Geräteunterlagen den Scanner entsprechend der [Dokumentprüfung](dokumentpruefung.md) einrichten.

Während der offenen Ersteinrichtung laufen weder Erinnerungsberechnung noch SMTP-Versand. Eine bereits vorbereitete SMTP-Datei benötigt nach der Einrichtung die tatsächlichen UUIDs der neu angelegten Betriebsstätten; Datei und Dienstkonfiguration anschließend entsprechend der [SMTP-Anleitung](smtp-erinnerungen.md) anpassen. Die Ersteinrichtung versendet keine Testmail.

## Vorhandene Installationen und Wiederherstellung

Vorhandene Installationen mit Konten oder Fachdaten öffnen den Assistenten nicht, auch wenn noch kein Administrator vorhanden ist. Der bisherige Datenbestand wird geschützt. Nach Sicherung ausdrücklich auf Schema 34 migrieren; [Vorgehen](module.md#bestehende-instanz-auf-schema-34-aktualisieren). `init` aktualisiert vorhandene Datenbanken nicht automatisch.

Ist bei einer bestehenden Instanz kein Administrator verfügbar, bleibt die ausdrückliche lokale Kontenwiederherstellung über `create-admin` oder `grant-admin` möglich; siehe [Verwaltung](administration.md). Das Entfernen oder Leeren der Codedatei öffnet eine abgeschlossene Einrichtung nicht erneut. Datenbank und Volume niemals löschen, um die Einrichtung neu zu starten.

Beim direkten Programmstart liegt die Codedatei standardmäßig neben der Datenbank. Ein anderer privater Speicherort kann mit `serve --setup-code-file /PFAD/setup-code.txt` festgelegt werden. Das Verzeichnis muss für den App-Benutzer schreibbar sein; die Datei weder öffentlich mounten noch über einen Webserver ausliefern.
