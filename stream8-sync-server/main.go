// Stream8 Sync Server - SQLite Pure Go Edition
package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web
var webFS embed.FS

// ---------------------------------------------------------------------
// Configurazione
// ---------------------------------------------------------------------

type config struct {
	webPort       string
	apiPort       string
	dataDir       string
	adminUser     string
	adminPassword string
	maxEntries    int
	maxBodyBytes  int64
}

func loadConfig() config {
	return config{
		webPort:       getEnv("WEB_PORT", "8080"),
		apiPort:       getEnv("API_PORT", "8081"),
		dataDir:       getEnv("DATA_DIR", "./data"),
		adminUser:     os.Getenv("ADMIN_USER"),
		adminPassword: os.Getenv("ADMIN_PASSWORD"),
		maxEntries:    300,
		maxBodyBytes:  2 << 20, // 2 MB
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------
// Modello dati
// ---------------------------------------------------------------------

type Account struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	KeyHash    string     `json:"keyHash"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty"`
	EntryCount int        `json:"entryCount"`
}

type PublicAccount struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty"`
	EntryCount int        `json:"entryCount"`
}

func (a *Account) toPublic() PublicAccount {
	return PublicAccount{
		ID:         a.ID,
		Label:      a.Label,
		CreatedAt:  a.CreatedAt,
		LastSyncAt: a.LastSyncAt,
		EntryCount: a.EntryCount,
	}
}

type historyEntry struct {
	Key       string `json:"_key"`
	WatchedAt int64  `json:"watchedAt"`
}

// ---------------------------------------------------------------------
// Database Store (SQLite)
// ---------------------------------------------------------------------

var errNotFound = errors.New("non trovato")

type Store struct {
	db *sql.DB
}

func newStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("impossibile creare la cartella dati: %w", err)
	}

	dbPath := filepath.Join(dataDir, "stream8.db")
	// DSN con PRAGMA attivi per WAL mode, busy timeout e Foreign Keys
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("errore apertura database SQLite: %w", err)
	}

	// Un solo writer concorrente raccomandato per SQLite WAL
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("errore inizializzazione schema DB: %w", err)
	}

	return s, nil
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		label TEXT NOT NULL,
		key_hash TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		last_sync_at DATETIME,
		entry_count INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS history (
		account_id TEXT NOT NULL,
		entry_key TEXT NOT NULL,
		watched_at INTEGER NOT NULL,
		data TEXT NOT NULL,
		PRIMARY KEY (account_id, entry_key),
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_history_account_watched
	ON history(account_id, watched_at DESC);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

// ---------------------------------------------------------------------
// Operazioni Account
// ---------------------------------------------------------------------

func generateAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "s8_" + hex.EncodeToString(buf), nil
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func generateAccountID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Store) createAccount(ctx context.Context, label string) (*Account, string, error) {
	id, err := generateAccountID()
	if err != nil {
		return nil, "", err
	}
	key, err := generateAPIKey()
	if err != nil {
		return nil, "", err
	}

	account := &Account{
		ID:        id,
		Label:     label,
		KeyHash:   hashAPIKey(key),
		CreatedAt: time.Now().UTC(),
	}

	query := `INSERT INTO accounts (id, label, key_hash, created_at, entry_count) VALUES (?, ?, ?, ?, ?)`
	_, err = s.db.ExecContext(ctx, query, account.ID, account.Label, account.KeyHash, account.CreatedAt, 0)
	if err != nil {
		return nil, "", fmt.Errorf("errore salvataggio account su DB: %w", err)
	}

	return account, key, nil
}

func (s *Store) listAccounts(ctx context.Context) ([]PublicAccount, error) {
	query := `SELECT id, label, created_at, last_sync_at, entry_count FROM accounts ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PublicAccount
	for rows.Next() {
		var a PublicAccount
		var lastSync sql.NullTime
		if err := rows.Scan(&a.ID, &a.Label, &a.CreatedAt, &lastSync, &a.EntryCount); err != nil {
			return nil, err
		}
		if lastSync.Valid {
			t := lastSync.Time.UTC()
			a.LastSyncAt = &t
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) deleteAccount(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) rotateKey(ctx context.Context, id string) (string, error) {
	key, err := generateAPIKey()
	if err != nil {
		return "", err
	}
	hash := hashAPIKey(key)

	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET key_hash = ? WHERE id = ?`, hash, id)
	if err != nil {
		return "", err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if affected == 0 {
		return "", errNotFound
	}

	return key, nil
}

func (s *Store) findByAPIKey(ctx context.Context, key string) *Account {
	if key == "" {
		return nil
	}
	hash := hashAPIKey(key)

	// Seleziona gli account per fare un confronto costante del hash in memoria
	rows, err := s.db.QueryContext(ctx, `SELECT id, label, key_hash, created_at, last_sync_at, entry_count FROM accounts`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var a Account
		var lastSync sql.NullTime
		if err := rows.Scan(&a.ID, &a.Label, &a.KeyHash, &a.CreatedAt, &lastSync, &a.EntryCount); err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(a.KeyHash), []byte(hash)) == 1 {
			if lastSync.Valid {
				t := lastSync.Time.UTC()
				a.LastSyncAt = &t
			}
			return &a
		}
	}
	return nil
}

// ---------------------------------------------------------------------
// Operazioni Cronologia (History)
// ---------------------------------------------------------------------

func (s *Store) getHistory(ctx context.Context, accountID string) ([]json.RawMessage, error) {
	query := `SELECT data FROM history WHERE account_id = ? ORDER BY watched_at DESC`
	rows, err := s.db.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]json.RawMessage, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		entries = append(entries, json.RawMessage(raw))
	}
	return entries, rows.Err()
}

func (s *Store) setHistory(ctx context.Context, account *Account, entries []json.RawMessage, maxEntries int) (int, error) {
	type keyed struct {
		raw  json.RawMessage
		meta historyEntry
	}

	byKey := make(map[string]keyed)
	var noKeyOrder []keyed

	for _, raw := range entries {
		var meta historyEntry
		_ = json.Unmarshal(raw, &meta)
		if meta.Key == "" {
			noKeyOrder = append(noKeyOrder, keyed{raw: raw, meta: meta})
			continue
		}
		existing, ok := byKey[meta.Key]
		if !ok || meta.WatchedAt >= existing.meta.WatchedAt {
			byKey[meta.Key] = keyed{raw: raw, meta: meta}
		}
	}

	merged := make([]keyed, 0, len(byKey)+len(noKeyOrder))
	for _, v := range byKey {
		merged = append(merged, v)
	}
	merged = append(merged, noKeyOrder...)

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].meta.WatchedAt > merged[j].meta.WatchedAt
	})

	if len(merged) > maxEntries {
		merged = merged[:maxEntries]
	}

	// Inizio transazione SQL atomica
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// Reset vecchia cronologia dell'account
	if _, err := tx.ExecContext(ctx, `DELETE FROM history WHERE account_id = ?`, account.ID); err != nil {
		return 0, fmt.Errorf("errore pulizia history: %w", err)
	}

	// Insert atomico del batch
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO history (account_id, entry_key, watched_at, data)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, entry_key) DO UPDATE SET
			watched_at = excluded.watched_at,
			data = excluded.data
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for i, item := range merged {
		key := item.meta.Key
		if key == "" {
			// fallback per chiavi vuote se presenti
			key = fmt.Sprintf("nokey_%d_%d", item.meta.WatchedAt, i)
		}
		if _, err := stmt.ExecContext(ctx, account.ID, key, item.meta.WatchedAt, string(item.raw)); err != nil {
			return 0, fmt.Errorf("errore inserimento voce cronologia: %w", err)
		}
	}

	now := time.Now().UTC()
	entryCount := len(merged)

	// Aggiornamento metadata account
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET last_sync_at = ?, entry_count = ? WHERE id = ?`, now, entryCount, account.ID)
	if err != nil {
		return 0, fmt.Errorf("errore aggiornamento account stats: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("errore commit transazione: %w", err)
	}

	return entryCount, nil
}

// ---------------------------------------------------------------------
// Internazionalizzazione (IT / EN / FR)
// ---------------------------------------------------------------------
//
// Il pannello di amministrazione (web/) e i messaggi di errore dell'API
// possono essere in Italiano, Inglese o Francese. La lingua è scelta così:
//  1. Se il client invia l'header "X-Stream8-Lang" con un valore esplicito
//     (it/en/fr), quella lingua vince sempre — è cosa succede quando
//     l'utente sceglie manualmente una lingua nel selettore del pannello.
//  2. Altrimenti si usa "Accept-Language" (impostazione "Sistema": la
//     lingua del sistema/browser del client), analizzato in modo semplice
//     prendendo il primo tag a due lettere riconosciuto.
//  3. Se nessuna delle due combacia con una lingua supportata, si ricade
//     sull'Italiano.

const (
	langIT = "it"
	langEN = "en"
	langFR = "fr"
)

var supportedLangs = map[string]bool{langIT: true, langEN: true, langFR: true}

var messages = map[string]map[string]string{
	"err_list_accounts": {
		langIT: "errore lettura account",
		langEN: "failed to load accounts",
		langFR: "erreur lors du chargement des comptes",
	},
	"err_bad_body": {
		langIT: "corpo della richiesta non valido",
		langEN: "invalid request body",
		langFR: "corps de la requête invalide",
	},
	"err_create_account": {
		langIT: "impossibile creare l'account",
		langEN: "unable to create the account",
		langFR: "impossible de créer le compte",
	},
	"err_account_not_found": {
		langIT: "account non trovato",
		langEN: "account not found",
		langFR: "compte introuvable",
	},
	"err_rotate_key": {
		langIT: "impossibile rigenerare la chiave",
		langEN: "unable to rotate the API key",
		langFR: "impossible de régénérer la clé",
	},
	"err_delete_account": {
		langIT: "impossibile eliminare l'account",
		langEN: "unable to delete the account",
		langFR: "impossible de supprimer le compte",
	},
	"err_unauthorized": {
		langIT: "chiave API mancante o non valida",
		langEN: "missing or invalid API key",
		langFR: "clé API manquante ou invalide",
	},
	"err_read_history": {
		langIT: "impossibile leggere la cronologia",
		langEN: "unable to read history",
		langFR: "impossible de lire l'historique",
	},
	"err_bad_body_large": {
		langIT: "corpo della richiesta non valido o troppo grande",
		langEN: "invalid or too large request body",
		langFR: "corps de la requête invalide ou trop volumineux",
	},
	"err_save_history": {
		langIT: "impossibile salvare la cronologia",
		langEN: "unable to save history",
		langFR: "impossible d'enregistrer l'historique",
	},
	"err_reread_history": {
		langIT: "cronologia salvata ma rilettura fallita",
		langEN: "history saved but re-reading it failed",
		langFR: "historique enregistré mais relecture échouée",
	},
}

// detectLang determina la lingua da usare per la risposta a questa
// richiesta, secondo le regole descritte sopra.
func detectLang(r *http.Request) string {
	if explicit := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Stream8-Lang"))); explicit != "" {
		if supportedLangs[explicit] {
			return explicit
		}
	}
	accept := r.Header.Get("Accept-Language")
	for _, part := range strings.Split(accept, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if len(tag) >= 2 && supportedLangs[tag[:2]] {
			return tag[:2]
		}
	}
	return langIT
}

// msg restituisce il testo tradotto per `key` nella lingua della richiesta,
// con fallback all'Italiano se la chiave non esiste per quella lingua.
func msg(r *http.Request, key string) string {
	lang := detectLang(r)
	if set, ok := messages[key]; ok {
		if text, ok := set[lang]; ok {
			return text
		}
		return set[langIT]
	}
	return key
}

// ---------------------------------------------------------------------
// HTTP - Helpers & Middlewares
// ---------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Stream8-Lang")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func basicAuthMiddleware(user, password string, next http.Handler) http.Handler {
	if user == "" || password == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="Stream8 Sync Admin"`)
			http.Error(w, "Autenticazione richiesta", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// gzipMiddleware comprime la risposta quando il client dichiara di
// supportarlo (header standard, presente in ogni browser e in `fetch`).
// Usata SOLO sull'API di sync (vedi main()): le risposte, in streaming da
// json.Encoder, non hanno un Content-Length prefissato quindi comprimerle
// è sempre sicuro, e le liste di cronologia in JSON possono arrivare a
// diverse centinaia di voci — un risparmio di banda concreto. Non è
// invece usata sugli asset statici del pannello admin, per il motivo
// spiegato accanto al suo utilizzo in main().
type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gz.Write(b)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

// ---------------------------------------------------------------------
// Router Web & Router API (Routing Nativo Go 1.22+)
// ---------------------------------------------------------------------

func webAssets() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("impossibile isolare gli asset web incorporati: %v", err)
	}
	return sub
}

func webUIRouter(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /", http.FileServer(http.FS(webAssets())))

	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		accs, err := store.listAccounts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, msg(r, "err_list_accounts"))
			return
		}
		writeJSON(w, http.StatusOK, accs)
	})

	mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Label string `json:"label"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, msg(r, "err_bad_body"))
			return
		}
		label := strings.TrimSpace(body.Label)
		if label == "" {
			label = "Account senza nome"
		}
		account, key, err := store.createAccount(r.Context(), label)
		if err != nil {
			log.Printf("errore creazione account: %v", err)
			writeError(w, http.StatusInternalServerError, msg(r, "err_create_account"))
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"account": account.toPublic(),
			"apiKey":  key,
		})
	})

	mux.HandleFunc("POST /api/accounts/{id}/rotate", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		key, err := store.rotateKey(r.Context(), id)
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, msg(r, "err_account_not_found"))
			return
		}
		if err != nil {
			log.Printf("errore rotazione chiave: %v", err)
			writeError(w, http.StatusInternalServerError, msg(r, "err_rotate_key"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"apiKey": key})
	})

	mux.HandleFunc("DELETE /api/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := store.deleteAccount(r.Context(), id)
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, msg(r, "err_account_not_found"))
		} else if err != nil {
			log.Printf("errore eliminazione account: %v", err)
			writeError(w, http.StatusInternalServerError, msg(r, "err_delete_account"))
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	})

	return mux
}

func apiRouter(store *Store, cfg config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":   "ok",
			"protocol": "stream8-sync-v1",
		})
	})

	mux.HandleFunc("GET /v1/history", func(w http.ResponseWriter, r *http.Request) {
		account := store.findByAPIKey(r.Context(), bearerToken(r))
		if account == nil {
			log.Printf("[sync] GET /v1/history 401 chiave non valida (remote=%s)", r.RemoteAddr)
			writeError(w, http.StatusUnauthorized, msg(r, "err_unauthorized"))
			return
		}

		entries, err := store.getHistory(r.Context(), account.ID)
		if err != nil {
			log.Printf("[sync] GET /v1/history account=%s FALLITA: %v", account.ID, err)
			writeError(w, http.StatusInternalServerError, msg(r, "err_read_history"))
			return
		}
		log.Printf("[sync] GET /v1/history account=%s restituite=%d", account.ID, len(entries))
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})

	mux.HandleFunc("PUT /v1/history", func(w http.ResponseWriter, r *http.Request) {
		account := store.findByAPIKey(r.Context(), bearerToken(r))
		if account == nil {
			log.Printf("[sync] PUT /v1/history 401 chiave non valida (remote=%s)", r.RemoteAddr)
			writeError(w, http.StatusUnauthorized, msg(r, "err_unauthorized"))
			return
		}

		var body struct {
			Entries []json.RawMessage `json:"entries"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			log.Printf("[sync] PUT /v1/history account=%s corpo non valido: %v", account.ID, err)
			writeError(w, http.StatusBadRequest, msg(r, "err_bad_body_large"))
			return
		}

		count, err := store.setHistory(r.Context(), account, body.Entries, cfg.maxEntries)
		if err != nil {
			log.Printf("[sync] PUT /v1/history account=%s FALLITA: %v", account.ID, err)
			writeError(w, http.StatusInternalServerError, msg(r, "err_save_history"))
			return
		}
		log.Printf("[sync] PUT /v1/history account=%s ricevute=%d salvate=%d", account.ID, len(body.Entries), count)

		entries, err := store.getHistory(r.Context(), account.ID)
		if err != nil {
			log.Printf("[sync] PUT /v1/history account=%s rilettura post-salvataggio FALLITA: %v", account.ID, err)
			writeError(w, http.StatusInternalServerError, msg(r, "err_reread_history"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "count": count})
	})

	return corsMiddleware(mux)
}

// ---------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------

func main() {
	cfg := loadConfig()

	store, err := newStore(cfg.dataDir)
	if err != nil {
		log.Fatalf("impossibile avviare lo store SQLite: %v", err)
	}
	defer store.Close()

	if cfg.adminUser == "" || cfg.adminPassword == "" {
		log.Printf("ATTENZIONE: ADMIN_USER/ADMIN_PASSWORD non impostate — l'interfaccia web di amministrazione è SENZA PROTEZIONE. Impostale in produzione.")
	}

	// gzip solo per l'API di sync: le risposte sono scritte in streaming da
	// json.Encoder senza un Content-Length prefissato, quindi comprimerle
	// non crea nessun disallineamento. Il pannello admin invece serve i
	// suoi asset statici con http.FileServer, che calcola e invia
	// Content-Length in anticipo: comprimerlo con questo stesso approccio
	// manderebbe un Content-Length non più corrispondente ai byte
	// realmente inviati (il browser troncherebbe la risposta). Gli asset
	// del pannello admin sono comunque pochi KB l'uno, quindi il guadagno
	// sarebbe comunque trascurabile.
	webHandler := basicAuthMiddleware(cfg.adminUser, cfg.adminPassword, webUIRouter(store))
	apiHandler := gzipMiddleware(apiRouter(store, cfg))

	webServer := &http.Server{
		Addr:              ":" + cfg.webPort,
		Handler:           webHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	apiServer := &http.Server{
		Addr:              ":" + cfg.apiPort,
		Handler:           apiHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Printf("Interfaccia web di amministrazione in ascolto su :%s", cfg.webPort)
		if err := webServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("errore server web: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		log.Printf("API di sincronizzazione in ascolto su :%s", cfg.apiPort)
		if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("errore server API: %v", err)
		}
	}()

	wg.Wait()
}
