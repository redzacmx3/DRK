# Dokumentprüfung und Freigabe (Schema 26)

Uploads beginnen unverändert in Quarantäne. Geräteverantwortliche am Standort des Geräts können eine Malwareprüfung anstoßen und danach eine getrennte fachliche Freigabe dokumentieren. Administratorrechte allein reichen nicht. Ein sauberer Scan bestätigt weder die Herkunft noch die fachliche Richtigkeit eines Dokuments.

## Bedienung

Geräteakte laden, im Abschnitt „Anhang prüfen und freigeben“ den konkreten Anhang wählen und seine Prüfhistorie laden. Nachweisreferenz, Qualifikation der prüfenden Person, betrieblicher Auftrag und Begründung ausdrücklich erfassen.

1. „Malwareprüfung ausführen“ überträgt die gespeicherten Dateibytes an den konfigurierten ClamAV-Dienst. Der Dateihash wird zuvor unabhängig überprüft. Ergebnis: `clean`, `infected` oder `unverified`. Auch nach `clean` bleibt der Anhang in Quarantäne.
2. „Fachlich geprüft freigeben“ erfordert eine dokumentierte Inhaltsprüfung und als unmittelbar vorhergehendes Ereignis einen sauberen Scan desselben Dateihashes, höchstens 24 Stunden alt. Die Signaturdaten dürfen beim Scan und bei Freigabe höchstens 72 Stunden alt sein. Diese Fristen sind technische Betriebsvorgaben, keine gesetzliche Freigabefrist.
3. Ungeeignete Anhänge „Zurückweisen“ oder eine bestehende Freigabe ausdrücklich entziehen. Ein gespeicherter erneuter Scan setzt den Status ebenfalls auf Quarantäne, unabhängig vom Ergebnis. Eine erneute Freigabe benötigt eine neue fachliche Entscheidung.

Scan und Entscheidung werden mit Dokumenthash, Revision, Akteur, Zeitpunkt und Prüfbezügen unveränderlich gespeichert. Ein fehlender/unerreichbarer Scanner, alte Signaturen, unbekannte Antworten und Scannerfehler ergeben `unverified`; sie erlauben keine Freigabe. Nach unklarer Antwort unverändert mit demselben Schlüssel erneut senden. Die aktuellen Standortrechte werden auch vor Wiederholantworten geprüft. Nach einem Versionskonflikt erneut laden und abgleichen. Browserentwürfe liegen ausschließlich im Arbeitsspeicher des Tabs; nach Schließen oder Neuladen den gespeicherten Stand prüfen.

## Scanner bereitstellen

Die Anwendung enthält keinen Virenscanner und lädt keine Signaturen selbst herunter. Ein optionaler, separat betreuter ClamAV-`clamd`-Dienst muss aktuelle Signaturen und das `VERSION`-/`INSTREAM`-Protokoll bereitstellen. Seine Zeitzone auf UTC stellen: die ClamAV-Versionsantwort enthält den Signaturzeitpunkt ohne Zeitzonenangabe; die Anwendung interpretiert ihn als UTC. Signaturaktualisierung, Dienstüberwachung und reale Scannerabnahme bleiben Betreiberaufgaben.

Alle drei Compose-Varianten reichen `DRK_CLAMD_ADDRESS` an die Anwendung weiter, zum Beispiel `clamav:3310` für einen vorhandenen Dienst im selben privaten Docker-Netz. Danach den Anwendungscontainer mit `up -d --wait` neu erzeugen. Die Entwicklungsvariante mit `network_mode: host` braucht stattdessen eine vom Host erreichbare private Adresse. Die leere Standardeinstellung deaktiviert die Scannerverbindung; Upload und Quarantäneprüfung bleiben nutzbar, Freigaben sind gesperrt.

`clamd` bietet auf diesem TCP-Protokoll keine Authentifizierung oder TLS. Nur einen vertrauenswürdigen, abgeschotteten lokalen/privaten Endpunkt verwenden; Port 3310 nicht öffentlich veröffentlichen. Die Adresse wird ausschließlich durch die Betriebskonfiguration festgelegt und niemals aus einem Browserrequest übernommen. Die gesamte Scannerkommunikation ist auf acht Sekunden begrenzt, der Server bearbeitet höchstens eine Dokumentprüfung gleichzeitig. HTTP/HTTPS der eigentlichen Anwendung wird unabhängig wie bisher über Compose gewählt.

## Zugriff und Auswirkungen

Freigegebene Anhänge können alle berechtigten Leser des Gerätebereichs als Attachment herunterladen. Quarantänisierte oder zurückgewiesene Inhalte bleiben nur per ausdrücklichem `?review=true`-Prüfdownload für Geräteverantwortliche zugänglich. Downloads prüfen den gespeicherten SHA-256, verwenden `nosniff` und eine Sandbox; es gibt keine öffentliche Dateifreigabe.

Die personenbezogene Einweisungsbewertung liest den aktuellen Dokumentstatus. Eine Freigabe kann die ausdrücklich erfassten personenbezogenen Anforderungen erfüllen; ein Entzug macht diesen Teil wieder ungeklärt. Ursprüngliche Einweisungs-/Uploadereignisse und physische Geräteprüfbasiswerte werden dabei nicht nachträglich umgeschrieben. Die vollständige Einsatzfreigabe bleibt `not_assessed`. Besondere Produktpflichten, tatsächlicher Gebrauchsanweisungszugang und konkrete Funktions-/Fachnachweise sind weiterhin gesondert zu bestätigen.

Das Geräteaktenarchiv enthält aktuelle Dokumentmetadaten sowie die vollständige Prüf-/Freigabehistorie in JSON und HTML. Dateiinhalte bleiben unabhängig vom Freigabestatus aus dem Archiv ausgeschlossen; freigegebene Inhalte werden über ihren geschützten Download abgerufen.

## API und Upgrade

`GET /api/v1/devices/{id}/documents` enthält `status` und `review_version`. `GET .../documents/{document}/reviews` liefert die letzten 100 Prüfereignisse. `POST` auf denselben Review-Pfad erwartet `action=scan|release|reject|quarantine`, `expected_version`, den angezeigten `sha256`, `evidence`, `qualification`, `assignment` und `note`, Origin/CSRF sowie einen Idempotenzschlüssel. Maximal 1000 Prüfereignisse je Dokument; weitere Änderungen werden vollständig abgelehnt. Die unveränderlichen Originalbytes bleiben erhalten.

Vor einem Upgrade ein neues konsistentes Backup erzeugen und ausdrücklich auf Schema 28 migrieren. Sicherungen umfassen Anhänge, Prüfereignisse, Outbox und Wiederholantworten. Vorhandene Anhänge bleiben beim Upgrade in Quarantäne. Scannerkonfiguration und aktuelle Signaturen sind außerhalb der SQLite-Sicherung zu betreuen.

Die automatisierten Entwicklungstests verwenden einen kontrollierten ClamAV-Protokoll-Testserver, einschließlich sauberer/infizierter/fehlerhafter und veralteter Befunde. Sie ersetzen keine Prüfung mit einem echten ClamAV-Dienst und aktuellen Signaturen auf dem Zielsystem.
