const status = document.querySelector('#status');
const login = document.querySelector('#login');
const inventory = document.querySelector('#inventory');
const list = document.querySelector('#locations');
let csrf = '';
function signedOut(message) {
 csrf = ''; list.replaceChildren();
 login.hidden = false; inventory.hidden = true; status.textContent = message;
}
async function load() {
 list.replaceChildren(); status.textContent = 'Lagerorte werden geladen …';
 try {
  const response = await fetch('/api/v1/locations', {cache: 'no-store'});
  if (response.status === 401) { signedOut('Bitte erneut anmelden.'); return; }
  if (!response.ok) throw new Error('unavailable');
  const locations = await response.json();
  for (const location of locations) {
   const item = document.createElement('li');
   const name = document.createElement('strong'); name.textContent = location.name;
   const detail = document.createElement('p'); detail.textContent = `${location.site} · ${location.operator}`;
   item.append(name, detail); list.append(item);
  }
  status.textContent = locations.length ? `${locations.length} Lagerorte geladen.` : 'Keine Lagerorte im zugewiesenen Bereich.';
 } catch { status.textContent = 'Daten konnten nicht geladen werden. Verbindung prüfen und erneut laden.'; }
}
async function restore() {
 try {
  const response = await fetch('/api/v1/me', {cache: 'no-store'});
  if (response.status === 401) { signedOut('Bitte anmelden.'); return; }
  if (!response.ok) throw new Error('unavailable');
  const user = await response.json();
  csrf = user.csrf_token;
  document.querySelector('#account').textContent = `Angemeldet als ${user.username}`;
  login.hidden = true; inventory.hidden = false;
  document.querySelector('#logout').disabled = !csrf;
  await load();
 } catch { status.textContent = 'Verbindung zum Server nicht verfügbar.'; }
}
login.addEventListener('submit', async event => {
 event.preventDefault(); const button = login.querySelector('button'); button.disabled = true;
 const values = new FormData(login);
 try {
  const response = await fetch('/api/v1/auth/login', {method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({username:values.get('username'),password:values.get('password')})});
  const result = await response.json();
  if (!response.ok) { signedOut(result.message || 'Anmeldung fehlgeschlagen.'); return; }
  csrf = result.csrf_token; await restore();
 } catch { status.textContent = 'Anmeldung nicht möglich. Verbindung prüfen.'; }
 finally { login.elements.password.value = ''; button.disabled = false; }
});
document.querySelector('#logout').addEventListener('click', async () => {
 try {
  const response = await fetch('/api/v1/auth/logout', {method:'POST',headers:{'X-CSRF-Token':csrf}});
  if (response.ok || response.status === 401) signedOut('Abgemeldet.');
  else status.textContent = 'Abmeldung fehlgeschlagen. Bitte erneut versuchen.';
 } catch { status.textContent = 'Abmeldung konnte nicht bestätigt werden. Verbindung prüfen.'; }
});
document.querySelector('#reload').addEventListener('click', load);
restore();
