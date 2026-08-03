# Stream8 Sync — specifica del protocollo

Questo documento descrive il protocollo usato dal sito **Stream8** per
sincronizzare la cronologia dei titoli guardati tra più dispositivi,
tramite un server auto-ospitato dall'utente. È scritto per essere
sufficiente, da solo, a implementare un server compatibile (o un client
compatibile) senza bisogno di leggere il codice sorgente di riferimento.

La sincronizzazione è **completamente opzionale** e **disattivata di
default** nel sito Stream8. Se attivata, il sito Stream8 stesso funziona
solo da client verso il server descritto qui: l'utente fornisce l'URL del
proprio server e una chiave API, generata dal server stesso.

---

## 1. Panoramica

Il server espone due superfici di rete separate, tipicamente su due porte
diverse dello stesso host:

1. **Interfaccia di amministrazione** (web, pensata per un umano): crea e
   revoca le chiavi API ("account"). Va protetta (es. autenticazione HTTP
   Basic, o accessibile solo da rete privata/VPN) perché chi vi accede può
   creare/eliminare chiavi.
2. **API di sincronizzazione** (pensata per essere chiamata dal browser,
   cross-origin, dal sito Stream8): legge e scrive la cronologia. Ogni
   richiesta autentica con una chiave API tramite header
   `Authorization: Bearer <chiave>`.

Ogni chiave API identifica un **account** isolato: la cronologia di un
account non è mai visibile né mescolata con quella di un altro account,
anche sullo stesso server.

Il server di riferimento (incluso in questo repository, `main.go`) implementa
questa specifica in Go, senza dipendenze esterne, con storage su file JSON.
Qualunque altra implementazione (altro linguaggio, altro storage) è
altrettanto valida purché rispetti i contratti qui descritti.

---

## 2. Autenticazione dell'API

Ogni richiesta all'API di sincronizzazione deve includere:

```
Authorization: Bearer <chiave_api>
```

Se l'header manca o la chiave non corrisponde a nessun account attivo, il
server deve rispondere `401 Unauthorized` con un corpo JSON:

```json
{ "error": "chiave API mancante o non valida" }
```

La chiave API è un segreto: chi la possiede ha pieno accesso in lettura e
scrittura alla cronologia di quell'account. Un server conforme dovrebbe:

- Generarla con una fonte casuale sicura (almeno 256 bit di entropia).
- Non salvarla mai in chiaro su disco (solo un hash, es. SHA-256):
  mostrarla in chiaro **una sola volta**, al momento della creazione o
  della rigenerazione, esattamente come un token API di un qualunque
  servizio cloud.

---

## 3. CORS

Il sito Stream8 gira su un'origine diversa da quella del server (l'utente
ospita il server dove preferisce). L'API **deve** quindi rispondere con
header CORS che permettano la richiesta cross-origin:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, PUT, OPTIONS
Access-Control-Allow-Headers: Authorization, Content-Type
```

Le richieste `OPTIONS` (preflight) devono ricevere una risposta `204 No
Content` con questi header, senza richiedere autenticazione. Un'origine
jolly (`*`) è appropriata qui perché l'autenticazione avviene tramite header
esplicito (`Authorization`), non tramite cookie: non ci sono credenziali
implicite del browser da proteggere con un'origine più restrittiva.

---

## 4. Endpoint dell'API di sincronizzazione

### `GET /v1/health`

Nessuna autenticazione richiesta. Usato dal client per verificare che l'URL
inserito dall'utente sia raggiungibile e sia effettivamente un server
Stream8 Sync, prima di salvare le impostazioni.

**Risposta 200:**
```json
{ "status": "ok", "protocol": "stream8-sync-v1" }
```

### `GET /v1/history`

Richiede autenticazione. Restituisce l'intera cronologia salvata per
l'account associato alla chiave.

**Risposta 200:**
```json
{ "entries": [ { "...": "vedi sezione 5" } ] }
```

Se l'account non ha ancora nessuna cronologia salvata, `entries` è un array
vuoto `[]`, non `null` e non un errore.

### `PUT /v1/history`

Richiede autenticazione. **Sostituisce** l'intera cronologia salvata per
l'account con quella fornita nel corpo della richiesta (non è un'aggiunta
incrementale: vedere sezione 6 per il perché e come il client deve
comportarsi).

**Corpo della richiesta:**
```json
{ "entries": [ { "...": "vedi sezione 5" } ] }
```

**Risposta 200:**
```json
{ "entries": [ "...le voci effettivamente salvate, dopo dedup/ordinamento/limite..." ], "count": 42 }
```

Un server conforme deve, prima di salvare:
- **Deduplicare** per campo `_key` (vedi sezione 5), tenendo la voce con
  `watchedAt` più alto tra i duplicati.
- **Ordinare** dal `watchedAt` più recente al meno recente.
- **Limitare** il numero di voci salvate (il server di riferimento usa 300)
  per contenere lo spazio occupato: è accettabile scartare silenziosamente
  le voci più vecchie oltre il limite.
- **Limitare la dimensione del corpo della richiesta** (il server di
  riferimento usa 2 MB) e rispondere `400 Bad Request` se superata.

Un server conforme **non deve** unire le voci ricevute con quelle già
salvate in precedenza (niente merge lato server): deve sostituirle
interamente. Il motivo è spiegato nella sezione 6.

### Codici di errore comuni

| Codice | Quando |
|---|---|
| `400` | Corpo della richiesta non valido, non JSON, o troppo grande |
| `401` | Chiave API mancante, sconosciuta o revocata |
| `405` | Metodo HTTP non supportato su quell'endpoint |
| `500` | Errore interno del server (es. impossibile scrivere su disco) |

Il corpo di ogni risposta di errore è `{ "error": "messaggio leggibile" }`.

---

## 5. Formato di una voce di cronologia

Ogni elemento dell'array `entries` è un oggetto JSON. Il server tratta
questi oggetti come **opachi**: non ha bisogno di conoscere o validare
tutti i campi, solo questi due, usati per deduplicare/ordinare:

| Campo | Tipo | Obbligatorio | Uso |
|---|---|---|---|
| `_key` | string | sì | Identificativo univoco della voce (titolo + episodio). Usato per deduplicare. |
| `watchedAt` | number (Unix ms) | sì | Timestamp dell'ultima visione. Usato per ordinare e per decidere quale voce tenere tra due con la stessa `_key`. |

Tutti gli altri campi (`mediaType`, `id`, `title`, `year`, `posterPath`,
`voteAverage`, `season`, `episode`, `viaService`, ecc.) sono definiti dal
client e il server deve conservarli e restituirli intatti, **anche se in
futuro il client ne aggiunge di nuovi**: un server conforme non deve
scartare campi sconosciuti. Questo è ciò che rende il protocollo
"stabile": un client aggiornato può iniziare a inviare nuovi campi senza
richiedere alcun aggiornamento del server.

Esempio di voce:
```json
{
  "_key": "tv-1399-3-7",
  "mediaType": "tv",
  "id": 1399,
  "title": "Il Trono di Spade",
  "year": "2011",
  "posterPath": "/xyz.jpg",
  "voteAverage": 8.4,
  "season": 3,
  "episode": 7,
  "viaService": "Netflix",
  "watchedAt": 1732000000000
}
```

---

## 6. Perché PUT sostituisce e non unisce (e cosa deve fare il client)

Se il server unisse automaticamente ogni `PUT` con la cronologia già
salvata (limitandosi ad aggiungere le voci nuove), un titolo **rimosso**
dall'utente su un dispositivo ricomparirebbe al sync successivo, pescato
dal server: il server non ha modo di distinguere "non l'ho mai visto" da
"l'ho rimosso apposta". Per questo il protocollo lascia la responsabilità
del merge al **client**, che ha il contesto per farlo correttamente:

1. `GET /v1/history` per leggere la cronologia remota.
2. Unire quell'elenco con la cronologia locale, voce per voce, per
   `_key`: se una `_key` esiste in entrambi, tenere quella con `watchedAt`
   più alto; unione altrimenti.
3. `PUT /v1/history` con l'elenco unito così ottenuto, che diventa la nuova
   verità sia locale sia remota.

Per una rimozione esplicita (l'utente cancella una voce o l'intera
cronologia su un dispositivo), il client deve saltare il passaggio 1-2 e
fare direttamente un `PUT` con lo stato locale già aggiornato (post-
rimozione): questo è un caso in cui l'intento esplicito dell'utente deve
prevalere sulla semplice unione.

### 6.1 Rimozioni e tombstone (importante)

Saltare il merge sul dispositivo che cancella **non basta da solo**: se
un'altra copia della cronologia (un secondo dispositivo, o lo stesso
dispositivo con uno stato locale non ancora aggiornato) esegue in seguito
il flusso normale dei passaggi 1-3 qui sopra, e quella copia possiede
ancora la voce cancellata, il passaggio 2 la reintrodurrà: dal punto di
vista del merge, "voce assente" non è distinguibile da "voce mai
sincronizzata", quindi l'assenza perde sempre contro qualunque copia che
la possiede ancora. È la causa del bug per cui una voce cancellata
"riappare" al sync successivo.

Un client conforme deve quindi rappresentare la cancellazione come una
voce normale marcata `"deleted": true`, con `watchedAt` aggiornato al
momento della cancellazione (un **tombstone**), non come la semplice
assenza della `_key`:

```json
{ "_key": "tv-1399-3-7", "deleted": true, "watchedAt": 1732001111000 }
```

Poiché il server tratta le voci come opache (sezione 5), un tombstone
viaggia nei `GET`/`PUT` come qualunque altra voce e partecipa allo stesso
merge per `_key`/`watchedAt` più alto: un tombstone più recente batte una
visione più vecchia della stessa `_key` (la cancellazione si propaga
correttamente ovunque), mentre una visione successiva alla cancellazione
batte il tombstone (ri-guardare un titolo dopo averlo cancellato lo fa
ricomparire, come atteso). Il client deve filtrare le voci con
`deleted: true` da qualunque elenco mostrato all'utente, e può eliminarle
definitivamente dal proprio stato locale una volta trascorso un periodo di
sicurezza (es. 90 giorni) durante il quale ci si aspetta che ogni altro
dispositivo attivo le abbia già viste.

Questo è un modello di sincronizzazione volutamente semplice ("last write
wins" sull'intero elenco, con tombstone per le cancellazioni), non un
sistema completo di risoluzione dei conflitti: adatto a un uso personale
su un numero ridotto di dispositivi che non sincronizzano nello stesso
istante, non a scenari di scrittura concorrente pesante.

---

## 7. Endpoint dell'interfaccia di amministrazione

Questi endpoint sono pensati per essere usati dalla UI web servita dallo
stesso server (stesso host/porta), non per essere chiamati dal sito
Stream8. Un'implementazione alternativa del server può strutturarli
diversamente, purché la UI web funzioni; non fanno parte del contratto che
il sito Stream8 richiede — l'unico contratto vincolante per l'interoperabilità
con Stream8 è quello della sezione 4.

- `GET /api/accounts` → elenco account (senza le chiavi, mai restituite dopo la creazione)
- `POST /api/accounts` `{ "label": "nome" }` → crea un account, risponde con `{ "account": {...}, "apiKey": "..." }` (chiave mostrata una sola volta)
- `POST /api/accounts/{id}/rotate` → rigenera la chiave di un account esistente, risponde `{ "apiKey": "..." }` (di nuovo, una sola volta)
- `DELETE /api/accounts/{id}` → elimina l'account e la sua cronologia

---

## 8. Configurazione del server di riferimento

Variabili d'ambiente (vedi anche `Dockerfile`/`docker-compose.yml`):

| Variabile | Default | Descrizione |
|---|---|---|
| `WEB_PORT` | `8080` | Porta dell'interfaccia di amministrazione |
| `API_PORT` | `8081` | Porta dell'API di sincronizzazione |
| `DATA_DIR` | `./data` | Cartella per i file di storage (va montata come volume in Docker) |
| `ADMIN_USER` | *(vuoto)* | Utente per l'HTTP Basic Auth sulla UI web |
| `ADMIN_PASSWORD` | *(vuoto)* | Password per l'HTTP Basic Auth sulla UI web |

Se `ADMIN_USER`/`ADMIN_PASSWORD` non sono impostate, l'interfaccia di
amministrazione resta **senza alcuna protezione**: il server lo segnala
nei log all'avvio. Consigliato impostarle sempre, o esporre la porta web
solo su una rete privata/VPN.

---

## 9. Cosa deve fare il sito Stream8 (lato client)

Riassunto della logica che il client (il sito Stream8) implementa sopra
questo protocollo:

1. Nelle Impostazioni, un interruttore attiva/disattiva la sincronizzazione
   (di default disattivata), con due campi: URL del server (verso l'API,
   quindi tipicamente `http://host:8081` o l'URL pubblico equivalente) e
   chiave API. Un pulsante verifica la connessione chiamando
   `GET /v1/health` prima di salvare.
2. Un interruttore separato attiva/disattiva la sincronizzazione
   automatica ad ogni nuovo episodio guardato; è comunque sempre presente
   un pulsante "Sincronizza ora" per un sync manuale, indipendentemente
   da quell'interruttore.
3. Ad ogni sync (automatico o manuale): fetch remoto, merge locale come
   descritto in sezione 6, push del risultato, aggiornamento della
   cronologia locale con l'elenco unito.
4. In caso di rimozione esplicita di una voce o reset della cronologia, se
   la sincronizzazione è attiva, il client fa un `PUT` diretto con lo
   stato locale aggiornato (bypassando il merge), per propagare subito la
   rimozione.
5. Tutti gli errori di rete verso il server di sync sono gestiti in modo
   da non bloccare mai l'uso normale dell'app: la sincronizzazione è
   sempre un'aggiunta opzionale, mai una dipendenza per il funzionamento
   di base del sito.
