// Stream8 Sync — pannello di amministrazione. Vanilla JS, nessuna
// dipendenza, nessun passaggio di build: caricato direttamente dal
// browser, servito dallo stesso binario Go via go:embed.
// Richiede i18n.js (caricato prima di questo file in index.html).

const REQUEST_TIMEOUT_MS = 10_000;

const accountsBody = document.getElementById('accounts-body');
const createForm = document.getElementById('create-form');
const labelInput = document.getElementById('label-input');
const newKeyBox = document.getElementById('new-key-box');
const newKeyValue = document.getElementById('new-key-value');
const copyKeyBtn = document.getElementById('copy-key-btn');
const langSelect = document.getElementById('lang-select');

// --- Rete -----------------------------------------------------------
// Ogni chiamata passa da qui: timeout esplicito (10s) così il pannello non
// resta bloccato su "Caricamento..." se il server non risponde, ed errori
// sempre loggati in console con il contesto della richiesta (endpoint e
// causa), invece di essere solo mostrati come alert generici.
async function apiFetch(path, options = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);

  const headers = { ...(options.headers || {}) };
  // La lingua esplicita (non "system") viene forzata anche sulle risposte
  // di errore del server tramite questo header; se l'utente ha scelto
  // "Sistema", non lo inviamo e il server userà Accept-Language del browser.
  const pref = getLangPreference();
  if (pref !== 'system') headers['X-Stream8-Lang'] = pref;

  let res;
  try {
    res = await fetch(path, { ...options, headers, signal: controller.signal });
  } catch (err) {
    console.error(`[Stream8 Admin] ${options.method || 'GET'} ${path} — rete/timeout:`, err);
    throw new Error(t('err_load_accounts'));
  } finally {
    clearTimeout(timer);
  }

  if (!res.ok) {
    let serverMessage;
    try {
      const body = await res.json();
      serverMessage = body?.error;
    } catch {
      // corpo non-JSON o vuoto: usiamo solo lo status
    }
    console.error(`[Stream8 Admin] ${options.method || 'GET'} ${path} — HTTP ${res.status}:`, serverMessage);
    throw new Error(serverMessage || `HTTP ${res.status}`);
  }

  if (res.status === 204) return null;
  return res.json();
}

function formatDate(iso) {
  if (!iso) return t('never');
  const d = new Date(iso);
  return d.toLocaleString(resolveActiveLang());
}

async function loadAccounts() {
  try {
    const accounts = await apiFetch('/api/accounts');
    renderAccounts(accounts || []);
  } catch (err) {
    accountsBody.innerHTML = `<tr><td colspan="5">${t('error_prefix')}: ${escapeHtml(err.message)}</td></tr>`;
  }
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function renderAccounts(accounts) {
  if (accounts.length === 0) {
    accountsBody.innerHTML = `<tr><td colspan="5" class="muted">${t('no_accounts')}</td></tr>`;
    return;
  }
  accountsBody.innerHTML = '';
  for (const acc of accounts) {
    const tr = document.createElement('tr');

    const nameTd = document.createElement('td');
    nameTd.textContent = acc.label;
    tr.appendChild(nameTd);

    const createdTd = document.createElement('td');
    createdTd.className = 'muted';
    createdTd.textContent = formatDate(acc.createdAt);
    tr.appendChild(createdTd);

    const syncTd = document.createElement('td');
    syncTd.className = 'muted';
    syncTd.textContent = formatDate(acc.lastSyncAt);
    tr.appendChild(syncTd);

    const countTd = document.createElement('td');
    countTd.textContent = String(acc.entryCount || 0);
    tr.appendChild(countTd);

    const actionsTd = document.createElement('td');

    const rotateBtn = document.createElement('button');
    rotateBtn.className = 'secondary';
    rotateBtn.type = 'button';
    rotateBtn.textContent = t('rotate_button');
    rotateBtn.addEventListener('click', () => rotateKey(acc.id, acc.label));
    actionsTd.appendChild(rotateBtn);

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'danger';
    deleteBtn.type = 'button';
    deleteBtn.textContent = t('delete_button');
    deleteBtn.addEventListener('click', () => deleteAccount(acc.id, acc.label));
    actionsTd.appendChild(deleteBtn);

    tr.appendChild(actionsTd);
    accountsBody.appendChild(tr);
  }
}

function showNewKey(key) {
  newKeyValue.textContent = key;
  newKeyBox.classList.remove('hidden');
}

createForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const label = labelInput.value.trim();
  if (!label) return;

  try {
    const data = await apiFetch('/api/accounts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ label }),
    });
    showNewKey(data.apiKey);
    labelInput.value = '';
    loadAccounts();
  } catch (err) {
    alert(err.message || t('err_create_account'));
  }
});

copyKeyBtn.addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(newKeyValue.textContent);
    copyKeyBtn.textContent = t('copied_button');
    setTimeout(() => {
      copyKeyBtn.textContent = t('copy_button');
    }, 1500);
  } catch {
    // clipboard API non disponibile (es. contesto non sicuro): l'utente
    // può comunque selezionare e copiare manualmente il testo.
  }
});

async function rotateKey(id, label) {
  if (!confirm(t('confirm_rotate', label))) return;
  try {
    const data = await apiFetch(`/api/accounts/${id}/rotate`, { method: 'POST' });
    showNewKey(data.apiKey);
    loadAccounts();
  } catch (err) {
    alert(err.message || t('err_rotate_key'));
  }
}

async function deleteAccount(id, label) {
  if (!confirm(t('confirm_delete', label))) return;
  try {
    await apiFetch(`/api/accounts/${id}`, { method: 'DELETE' });
    loadAccounts();
  } catch (err) {
    alert(err.message || t('err_delete_account'));
  }
}

// --- Selettore lingua -------------------------------------------------
function initLangSelector() {
  langSelect.value = getLangPreference();
  langSelect.addEventListener('change', () => {
    setLangPreference(langSelect.value);
    applyStaticTranslations();
    loadAccounts(); // rilegge/ritraduce anche righe già caricate (date, bottoni)
  });
}

applyStaticTranslations();
initLangSelector();
loadAccounts();
