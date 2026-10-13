# Weitere produktspezifische Einzelpflichten

Schema 30 bleibt unverändert. Die Ergänzung erweitert die vorhandene Produktpflichtenbewertung um eine fachliche Teilabnahme konkreter zusätzlicher Pflichten. Frühere Bewertungen bleiben erhalten; aus ihren allgemeinen Freitextreferenzen wird keine Einzelpflichtenabnahme abgeleitet.

## Bedienung

Unter **Besondere Produktpflichten** das Gerät laden und den Bereich **Weitere produktspezifische Pflichten** ausdrücklich als erforderlich bewerten. Der neue Abschnitt erfasst:

- Den vollständig fachlich geprüften zusätzlichen Pflichtumfang und die Grundlage seiner Vollständigkeit.
- Eine bis zwölf Einzelpflichten mit eindeutigem Kurzcode, konkreter Beschreibung, Erfüllungsstatus, Nachweisreferenz und Begründung.
- Die ausdrücklich bewertete Vollständigkeit des Umfangs sowie Entscheidung, Abnahmedatum, Qualifikation, Auftrag, Nachweis und Begründung der prüfenden Person.
- Eine aktuell freigegebene Bewertungsunterlage derselben Geräteakte.

Mögliche Erfüllungsstände sind **nachgewiesen erfüllt**, **nicht erfüllt**, **ungeklärt** und **fachlich begründet nicht anwendbar**. Bei Nichtanwendbarkeit sind Nachweis und fachliche Begründung ebenso erforderlich; die Anwendung prüft die rechtliche Tragfähigkeit nicht selbst. Codes bestehen aus 2–40 kleinen ASCII-Buchstaben, Ziffern, Unterstrichen oder Bindestrichen und beginnen mit einem Buchstaben. Innerhalb einer Bewertung darf jeder Code nur einmal vorkommen.

Zunächst Felder und Einzelpflichten ausfüllen, anschließend die fachliche Bewertung ausdrücklich bestätigen. Änderungen an Eingaben oder Unterlagen setzen die Bestätigung zurück. Positive Teilabnahme erfordert einen ausdrücklich vollständig erfassten Umfang und ausschließlich erfüllte beziehungsweise fachlich begründet nicht anwendbare Einzelpflichten. Unerfüllte, ungeklärte oder unvollständig erfasste Pflichten können nicht positiv abgenommen werden. Eine Ablehnung kann den geprüften offenen oder unvollständigen Stand mit Begründung dokumentieren.

## Bindung und Aussage

`other_requirements` unterscheidet `accepted`, `unknown` und `blocked`. Abgelehnte oder unerfüllte Einzelpflichten blockieren die Teilbewertung. Neue Geräte-, Personen- oder Dokumentnachweise sowie veränderte Dokumentprüfstände machen eine zuvor positive Abnahme ungeklärt. Erneute Dokumentfreigabe reaktiviert keine alte Abnahme. Die technische Befristung der übergeordneten Produktpflichtenbewertung gilt weiterhin.

Eine aktive Sperre oder Außerbetriebnahme verhindert jede neue positive Abnahme. Bereits bestehende Sperren werden nicht aufgehoben. Elektrische Sicherheit hat weiterhin eine separate Kontrollregel, Prüfung und Teilabnahme; ihre Nachweise werden nicht durch frei benannte Einzelpflichten ersetzt. Der übergeordnete Bereich `other`, die vollständige Erfüllungsabnahme und `overall_readiness=not_assessed` bleiben offen. Diese Ergänzung schafft die fehlende strukturierte Nachweisgrundlage für die spätere zusammengeführte Freigabe.

Die Vollständigkeit der tatsächlich geltenden Pflichten, Prüferkompetenz und externe Nachweise werden fachlich außerhalb der Anwendung geprüft und ausdrücklich dokumentiert. Insbesondere entstehen keine besonderen Produktregeln aus Gerätebezeichnungen. Arzneimittel, spezielle Fachmodule und ein vollständiges Aufbereitungs-QM werden dadurch nicht automatisch unterstützt.

## API und Rechte

Optionales `other_acceptance` in einer ausdrücklich erforderlichen Regel `other` bei `POST /api/v1/devices/{id}/product-duties`:

```json
{
  "reviewed": true,
  "decision": "accepted",
  "performed_on": "2026-10-13",
  "document_id": "ID_DER_FREIGEGEBENEN_UNTERLAGE",
  "expected_snapshot": "AKTUELLER_BOOK_SNAPSHOT",
  "scope": "Extern fachlich bestätigter zusätzlicher Pflichtumfang",
  "completeness_evidence": "Referenz auf die vollständige Pflichtprüfung",
  "scope_complete": true,
  "qualification": "Nachweis der Abnahmekompetenz",
  "assignment": "Betrieblicher Auftrag",
  "evidence": "Referenz auf die Teilabnahme",
  "note": "Fachliche Begründung",
  "requirements": [
    {
      "code": "lagerbedingungen",
      "description": "Produktspezifische Lagerbedingungen",
      "status": "met",
      "evidence": "Extern geprüfter Nachweis",
      "reason": "Fachliche Begründung der Erfüllung"
    }
  ]
}
```

Die umgebende Produktpflichtenbewertung benötigt unverändert alle acht Regeln, Geräte-/Bewertungsversion, Produktgrundlage, ausdrückliche Bestätigung, Befristung und vier Prüfbegründungen. Der Server speichert `other_basis` mit Aktenstand, Produktgrundlagen-Prüfversion und Prüfsumme / Prüfversion der Abnahmeunterlage. Die bestehenden Grenzen von 16 KiB pro Anfrage und 2000 Ereignissen pro Gerät gelten einschließlich der neuen Einzelpflichten. Die maximale Anzahl ist keine fachliche Zusicherung, dass zwölf Pflichten für jedes Produkt ausreichen; größere Umfänge müssen gesondert strukturiert werden.

Nur Geräteverantwortliche der tatsächlichen Betriebsstätte dürfen schreiben; Administratorrecht allein reicht nicht. Leser sehen die erlaubte Historie, aber keinen privaten Vorbereitungsschnappschuss. Sitzung und aktuelle Standortrechte werden vor historischen Wiederholantworten erneut geprüft. Ereignis, Outbox und Wiederholantwort werden atomar gespeichert; Versionenkonflikte speichern keine Teilabnahme. Bei unbestätigtem Ergebnis werden Anfrage und Schlüssel unverändert wiederholt.

Die Geräteakte enthält die Bewertung und Einzelpflichten in JSON sowie die sicher dargestellte Historie in HTML; Archivmanifest und bestehende Größenlimits gelten weiterhin. Für dieses Update ist keine zusätzliche Migration nötig. Bestehende Installationen müssen bereits auf Schema 30 sein; aktuelle Sicherungen und Datenvolumes erhalten.
