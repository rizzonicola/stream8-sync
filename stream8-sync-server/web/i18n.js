// Stream8 Sync — internazionalizzazione del pannello di amministrazione.
// Vanilla JS, nessuna dipendenza. Lingue supportate: it, en, fr.
// "Sistema" (valore salvato: "system") usa la lingua del browser
// (navigator.language), con l'Italiano come ultima risorsa.

const STREAM8_LANG_STORAGE_KEY = 'stream8admin.lang';
const STREAM8_SUPPORTED_LANGS = ['it', 'en', 'fr'];
const STREAM8_DEFAULT_LANG = 'it';

const STREAM8_DICT = {
  it: {
    title: 'Stream8 Sync',
    subtitle:
      'Server di sincronizzazione della cronologia. Ogni chiave API qui sotto corrisponde a un account isolato: cronologie diverse non si mescolano mai.',
    lang_label: 'Lingua',
    lang_system: 'Sistema',
    lang_it: 'Italiano',
    lang_en: 'Inglese',
    lang_fr: 'Francese',
    new_account_heading: 'Nuovo account',
    label_placeholder: 'Nome (es. Telefono di Marco)',
    create_button: 'Crea chiave API',
    copy_hint: 'Copia questa chiave ora: non verrà mostrata di nuovo.',
    copy_button: 'Copia',
    copied_button: 'Copiato!',
    existing_accounts_heading: 'Account esistenti',
    th_name: 'Nome',
    th_created: 'Creato il',
    th_last_sync: 'Ultima sync',
    th_entries: 'Voci',
    loading: 'Caricamento...',
    no_accounts: 'Nessun account ancora.',
    error_prefix: 'Errore',
    never: 'Mai',
    rotate_button: 'Rigenera chiave',
    delete_button: 'Elimina',
    confirm_rotate: (label) =>
      `Rigenerare la chiave per "${label}"? La vecchia chiave smetterà subito di funzionare su tutti i dispositivi già collegati.`,
    confirm_delete: (label) =>
      `Eliminare "${label}"? La sua cronologia sincronizzata verrà cancellata definitivamente dal server.`,
    err_load_accounts: 'Errore nel caricamento',
    err_create_account: "Impossibile creare l'account",
    err_rotate_key: 'Impossibile rigenerare la chiave',
    err_delete_account: "Impossibile eliminare l'account",
    footer_github: 'rizzonicola su GitHub',
    footer_credits: 'Go · SQLite (modernc.org/sqlite, BSD-3-Clause)',
  },
  en: {
    title: 'Stream8 Sync',
    subtitle:
      'History synchronization server. Each API key below belongs to an isolated account: histories are never mixed across accounts.',
    lang_label: 'Language',
    lang_system: 'System',
    lang_it: 'Italian',
    lang_en: 'English',
    lang_fr: 'French',
    new_account_heading: 'New account',
    label_placeholder: "Name (e.g. Marco's phone)",
    create_button: 'Create API key',
    copy_hint: 'Copy this key now: it will not be shown again.',
    copy_button: 'Copy',
    copied_button: 'Copied!',
    existing_accounts_heading: 'Existing accounts',
    th_name: 'Name',
    th_created: 'Created',
    th_last_sync: 'Last sync',
    th_entries: 'Entries',
    loading: 'Loading...',
    no_accounts: 'No accounts yet.',
    error_prefix: 'Error',
    never: 'Never',
    rotate_button: 'Rotate key',
    delete_button: 'Delete',
    confirm_rotate: (label) =>
      `Rotate the key for "${label}"? The old key will stop working immediately on every device already connected.`,
    confirm_delete: (label) =>
      `Delete "${label}"? Its synced history will be permanently erased from the server.`,
    err_load_accounts: 'Failed to load accounts',
    err_create_account: 'Unable to create the account',
    err_rotate_key: 'Unable to rotate the key',
    err_delete_account: 'Unable to delete the account',
    footer_github: 'rizzonicola on GitHub',
    footer_credits: 'Go · SQLite (modernc.org/sqlite, BSD-3-Clause)',
  },
  fr: {
    title: 'Stream8 Sync',
    subtitle:
      "Serveur de synchronisation de l'historique. Chaque clé API ci-dessous correspond à un compte isolé : les historiques ne sont jamais mélangés entre comptes.",
    lang_label: 'Langue',
    lang_system: 'Système',
    lang_it: 'Italien',
    lang_en: 'Anglais',
    lang_fr: 'Français',
    new_account_heading: 'Nouveau compte',
    label_placeholder: 'Nom (ex. Téléphone de Marco)',
    create_button: 'Créer une clé API',
    copy_hint: 'Copiez cette clé maintenant : elle ne sera plus affichée.',
    copy_button: 'Copier',
    copied_button: 'Copié !',
    existing_accounts_heading: 'Comptes existants',
    th_name: 'Nom',
    th_created: 'Créé le',
    th_last_sync: 'Dernière sync',
    th_entries: 'Entrées',
    loading: 'Chargement...',
    no_accounts: 'Aucun compte pour le moment.',
    error_prefix: 'Erreur',
    never: 'Jamais',
    rotate_button: 'Régénérer la clé',
    delete_button: 'Supprimer',
    confirm_rotate: (label) =>
      `Régénérer la clé de "${label}" ? L'ancienne clé cessera immédiatement de fonctionner sur tous les appareils déjà connectés.`,
    confirm_delete: (label) =>
      `Supprimer "${label}" ? Son historique synchronisé sera définitivement effacé du serveur.`,
    err_load_accounts: 'Échec du chargement',
    err_create_account: 'Impossible de créer le compte',
    err_rotate_key: 'Impossible de régénérer la clé',
    err_delete_account: 'Impossible de supprimer le compte',
    footer_github: 'rizzonicola sur GitHub',
    footer_credits: 'Go · SQLite (modernc.org/sqlite, BSD-3-Clause)',
  },
};

// Rileva la lingua di sistema/browser e la riduce a una delle lingue
// supportate, con fallback all'Italiano.
function detectSystemLang() {
  const raw = (navigator.language || STREAM8_DEFAULT_LANG).slice(0, 2).toLowerCase();
  return STREAM8_SUPPORTED_LANGS.includes(raw) ? raw : STREAM8_DEFAULT_LANG;
}

// Legge la preferenza salvata ("system" | "it" | "en" | "fr"); default "system".
function getLangPreference() {
  try {
    return localStorage.getItem(STREAM8_LANG_STORAGE_KEY) || 'system';
  } catch {
    return 'system';
  }
}

function setLangPreference(pref) {
  try {
    localStorage.setItem(STREAM8_LANG_STORAGE_KEY, pref);
  } catch {
    // storage non disponibile: la scelta vale solo per la sessione corrente
  }
}

// Lingua effettiva usata per il rendering (mai "system": già risolta).
function resolveActiveLang() {
  const pref = getLangPreference();
  return pref === 'system' ? detectSystemLang() : pref;
}

function t(key, ...args) {
  const lang = resolveActiveLang();
  const entry = (STREAM8_DICT[lang] || STREAM8_DICT[STREAM8_DEFAULT_LANG])[key];
  if (typeof entry === 'function') return entry(...args);
  return entry ?? key;
}

// Applica le traduzioni a tutti gli elementi statici marcati con
// data-i18n / data-i18n-placeholder nel DOM, ed espone la lingua attiva
// nell'header "X-Stream8-Lang" inviato dalle chiamate API (vedi app.js),
// così anche i messaggi di errore restituiti dal server sono coerenti con
// la lingua scelta nel pannello.
function applyStaticTranslations() {
  document.documentElement.lang = resolveActiveLang();
  document.querySelectorAll('[data-i18n]').forEach((el) => {
    el.textContent = t(el.getAttribute('data-i18n'));
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach((el) => {
    el.setAttribute('placeholder', t(el.getAttribute('data-i18n-placeholder')));
  });
}
