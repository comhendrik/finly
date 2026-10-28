# Finly

Finly ist eine lokal betriebene Web-App für die monatliche Budgetplanung. Einkommen und monatliche Fixkosten bilden den Betrag, der nach festen Ausgaben übrig bleibt. Für jeden Monat lassen sich daraus Budgets anlegen und einzelne Ausgaben erfassen. Monate werden ausschließlich manuell über die Pfeile gewechselt; es gibt keine automatischen Abbuchungen.

## Start mit Docker Compose

Über **Konto erstellen** kann jeder Benutzer ein eigenes Finly-Konto anlegen. Benutzername und Passwort sind pro Konto eindeutig; Monatsbudgets und Ausgaben werden getrennt gespeichert. Passwörter müssen mindestens 12 Zeichen haben. Lokal ist `COOKIE_SECURE=false` nötig, weil `localhost:8080` ohne TLS läuft.

```sh
docker compose up --build -d
```

Danach ist Finly unter [http://localhost:8080](http://localhost:8080) erreichbar. Docker Compose startet zwei Container: den Go-Server, der Oberfläche und API ausliefert, sowie PostgreSQL. PostgreSQL-Daten liegen im Docker-Volume `postgres_data` und bleiben bei `docker compose down` erhalten. Für ein Backup verwende `docker compose exec -T db pg_dump -U finly finly > finly.sql`; wiederherstellen kannst du es mit `docker compose exec -T db psql -U finly finly < finly.sql`.

Vorhandene `data/finly.json`-Dateien werden beim Start nicht eingelesen. Daten werden ausschließlich über **Konto → Daten importieren** aus einem benutzerunabhängigen Finly-JSON-Export übernommen. Im Konto-Menü kannst du auch einen Export für eine inklusive Von-bis-Monatsspanne erstellen. Setze für einen veröffentlichten Dienst `POSTGRES_PASSWORD` in einer privaten `.env`-Datei auf ein starkes Passwort. Die Compose-Standardwerte sind nur für lokale Entwicklung geeignet.

Unter **Konto** kannst du dein Passwort ändern, dein Konto samt zugehörigen Finanzdaten löschen oder einen JSON-Export in ein anderes Finly-Konto importieren. Im Export sind keine Kontodaten enthalten; beim Import werden nur die Monate aus der Datei ersetzt. Die auswählbare Exportspanne umfasst höchstens 240 Monate.

## Deployment auf Railway

Das Repository enthält eine `railway.json`-Konfiguration und nutzt den vorhandenen Dockerfile-Build. Railway baut damit einen Go-App-Service; ein separater Nginx-Service ist nicht nötig. Die App bindet an Railways `PORT` und prüft über `/health`, ob PostgreSQL erreichbar ist. Railway unterstützt den Dockerfile-Build und diese Healthcheck-Einstellungen direkt in der Config-as-Code-Datei ([Dockerfiles](https://docs.railway.com/builds/dockerfiles), [Config as Code](https://docs.railway.com/config-as-code/reference)).

1. Erstelle in Railway ein Projekt und füge das Repository als App-Service hinzu.
2. Füge dem Projekt einen PostgreSQL-Service hinzu.
3. Setze im App-Service die Variablen `DATABASE_URL=${{Postgres.DATABASE_URL}}` und `COOKIE_SECURE=true`. Passe `Postgres` im Verweis an den tatsächlichen Namen deines Datenbank-Services an. Railway stellt die Datenbankverbindungsvariablen bereit und empfiehlt die private `DATABASE_URL` für Verbindungen innerhalb des Projekts ([PostgreSQL auf Railway](https://docs.railway.com/databases/postgresql)).
4. Deploye den App-Service und erzeuge in dessen Networking-Einstellungen eine öffentliche Domain.

Railway startet den Build automatisch aus dem Dockerfile. PostgreSQL wird als eigener Railway-Service betrieben; ein lokales Compose-Volume wird dafür nicht verwendet. Sichere die Datenbank regelmäßig über Railways Datenbank-Backup-Funktionen oder einen separaten `pg_dump`-Prozess.

### Veröffentlichung im Internet

Jeder kann sich selbst registrieren. Für eine Veröffentlichung außerhalb von Railway betreibst du Finly hinter einem Reverse Proxy mit gültigem TLS-Zertifikat (zum Beispiel Caddy oder nginx), leitest nur HTTPS an den Server-Container weiter und setzt `COOKIE_SECURE=true` in der Compose-Umgebung (etwa in einer privaten `.env`-Datei). Öffne Port 8080 nicht direkt ins Internet. Verwende ein starkes, einzigartiges Passwort und sichere PostgreSQL regelmäßig. Der Anmeldeschutz begrenzt Fehlversuche pro IP-Adresse.

### API zum Erfassen von Ausgaben

Unter **Einstellungen → API-Zugriff** kannst du mit erneut eingegebenem Finly-Passwort einen API-Key erstellen. Er wird als JWT Bearer-Token genau einmal angezeigt, gilt ein Jahr und kann dort erneuert oder widerrufen werden. Finly speichert nur den Token-Hash. Sende den Token im `Authorization: Bearer <token>`-Header.

Die öffentliche API hat derzeit genau einen Endpunkt:

#### `POST /api/v1/expenses`

Fügt eine Ausgabe hinzu. Der Aufruf muss JSON mit `Content-Type: application/json` senden.

| Feld | Typ | Pflicht | Beschreibung |
| --- | --- | --- | --- |
| `month` | String | Ja | Zielmonat im Format `YYYY-MM`, zum Beispiel `2026-10`. |
| `name` | String | Ja | Bezeichnung der Ausgabe; darf nicht leer sein. |
| `amount` | String | Ja | Betrag als Text. Der Server entfernt alle Zeichen außer Ziffern und Kommas und verarbeitet das Komma als Dezimaltrennzeichen. |
| `budget_id` | String | Nein | ID eines bestehenden Budgets im Zielmonat. Wenn ausgelassen oder leer, wird eine Standardausgabe erstellt. |
| `date` | String | Nein | Ausgabedatum im Format `YYYY-MM-DD`. Ohne Angabe verwendet Finly das aktuelle Serverdatum. |
| `category` | String | Nein | Kategorie. Standardausgaben erhalten ohne Angabe `Allgemein`; bei Budgetausgaben übernimmt Finly die Budgetkategorie oder den Budgetnamen. |

Beispiel für eine Standardausgabe:

```sh
curl -X POST http://localhost:8080/api/v1/expenses \
  -H "Authorization: Bearer $FINLY_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"month":"2026-10","name":"Mittagessen","amount":"12,50","category":"Essen"}'
```

Zusätzliche Zeichen im Betragstext werden serverseitig ignoriert. So wird zum Beispiel `"amount":"2,49B B-y"` als `2,49` verarbeitet.

Für eine Budgetausgabe ergänze `"budget_id":"BUDGET_ID"` im JSON. Die Budget-ID muss zu einem Budget im angegebenen Monat gehören.

Erfolgreich angelegte Ausgaben liefern HTTP `201 Created` und ein JSON-Objekt mit ID, Monat, Typ und Ausgabe:

```json
{
  "id": "1792841831877942486",
  "month": "2026-10",
  "type": "standard",
  "expense": {
    "id": "1792841831877942486",
    "name": "Mittagessen",
    "amount": 12.5,
    "date": "2026-10-26",
    "category": "Essen"
  }
}
```

Fehlerantworten sind JSON mit einem `error`-Feld:

| HTTP-Status | Bedeutung |
| --- | --- |
| `400 Bad Request` | Ungültiges JSON, fehlende Pflichtfelder, ungültiger Monat oder ungültiges Datum. |
| `401 Unauthorized` | Bearer-Token fehlt, ist ungültig, abgelaufen oder wurde widerrufen. |
| `404 Not Found` | Die angegebene Budget-ID wurde im Zielmonat nicht gefunden. |
| `500 Internal Server Error` | Finly konnte die Ausgabe nicht speichern. |

Der API-Key kann nur Ausgaben hinzufügen. Es gibt keine API-Endpunkte zum Lesen, Ändern oder Löschen von Daten oder zum Verwalten von Budgets. Die API-Key-Erstellung und der Widerruf sind geschützte Formulare in der Weboberfläche, keine öffentlichen API-Endpunkte. Bei Veröffentlichung verwende für API-Aufrufe ausschließlich HTTPS und halte den Bearer-Token geheim.

Die Anmeldung ersetzt keine TLS-Terminierung, Firewall, sichere Host-Konfiguration oder Backups. Die Anwendung hat keinen Passwort-Reset. Ein vergessenes Passwort muss durch eine administrative Änderung des Passwort-Hashes in den PostgreSQL-Datensätzen für den betreffenden Benutzer zurückgesetzt werden.

Einkommen, Fixkosten, Budgets und erfasste Ausgaben sind je Monat gespeichert. In den Einstellungen lassen sich Einkommen, Fixkosten und Monatsbudgets für den ausgewählten Monat ändern. Einzelne Ausgaben erfasst du in der Übersicht beim jeweiligen Budget. Der Tab „Standardausgaben“ erfasst Ausgaben außerhalb der Budgets und zieht sie vom nach Fixkosten und Budgetplanung verbleibenden Betrag ab. Beim ersten Speichern eines Monats werden Einkommen und Fixkosten aus dem nächstliegenden gespeicherten Monat übernommen. Liegt der Monat vor allen gespeicherten Monaten, dienen die Werte des ältesten bekannten Monats als Vorlage; für spätere Monate gelten die Werte des letzten bekannten Monats. Bereits gespeicherte Monatswerte bleiben unverändert. Der Go-Dienst speichert den Zustand in PostgreSQL.

## Projektstruktur

- `frontend/` enthält HTML-Templates und statische Styles, die der Go-Server ausliefert.
- `backend/cmd/finly/` ist der Programmeinstieg.
- `backend/` enthält HTTP-Routen, Handler, Authentifizierung, Speicherung und Hilfsfunktionen.
- `backend/domain/` enthält die Datenmodelle.
- `backend/services/` enthält Geschäftsregeln, die von Weboberfläche und API gemeinsam genutzt werden.
