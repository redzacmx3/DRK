const status = document.querySelector('#status');
const login = document.querySelector('#login');
const inventory = document.querySelector('#inventory');
const list = document.querySelector('#locations');
const booking = document.querySelector('#booking');
const stockStatus = document.querySelector('#stock-status');
let csrf = '', user = null, locations = [], balances = [], catalog = [], pending = null;
const stateNames = {available:'Verfügbar',quarantined:'Quarantäne',damaged:'Beschädigt'};
function signedOut(message) {
 csrf = ''; user = null; list.replaceChildren(); document.querySelector('#stock-rows').replaceChildren();
 login.hidden = false; inventory.hidden = true; status.textContent = message;
}
function can(location, roles) { return user.rights.some(right => right.site_id === location.site_id && roles.includes(right.role)); }
function options(select, items) {
 const previous = select.value; select.replaceChildren();
 for (const item of items) { const option = document.createElement('option'); option.value = item.id; option.textContent = item.label; select.append(option); }
 if (items.some(item => item.id === previous)) select.value = previous;
}
function lockBooking(locked) {
 for (const control of booking.elements) control.disabled = locked;
 booking.querySelector('button').disabled = pending && pending.owner !== user?.id;
 booking.querySelector('button').textContent = pending ? 'Buchung erneut senden' : 'Buchung bestätigen';
}
function bookingChoices() {
 if (!user || pending) return;
 const action = document.querySelector('#action').value;
 const managers = locations.filter(location => can(location,['material_manager']));
 document.querySelector('#batch-field').hidden = action !== 'receipts';
 document.querySelector('#receipt-state-field').hidden = action !== 'receipts';
 document.querySelector('#source-field').hidden = action === 'receipts';
 document.querySelector('#target-field').hidden = action === 'consumptions';
 options(document.querySelector('#batch'), catalog.map(batch => ({id:batch.id,label:`${batch.product} · ${batch.lot} · ${batch.unit}`})));
 options(document.querySelector('#target'), managers.map(location => ({id:location.id,label:location.name})));
 const sources = balances.filter(balance => {
  const location = locations.find(value => value.id === balance.location_id);
  return location && balance.quantity > 0 && can(location,action === 'consumptions' ? ['helper','material_manager'] : ['material_manager']) && (action !== 'consumptions' || balance.usable_quantity > 0);
 });
 options(document.querySelector('#source'), sources.map(balance => ({id:`${balance.batch_id}|${balance.location_id}|${balance.state}`,label:`${balance.product} · ${balance.lot} · ${balance.location} · ${balance.quantity} ${balance.unit} · ${stateNames[balance.state]}`})));
}
async function loadStock() {
 const body = document.querySelector('#stock-rows'); body.replaceChildren(); stockStatus.textContent = 'Bestände werden geladen …';
 try {
  const [catalogResponse,balanceResponse] = await Promise.all([fetch('/api/v1/stock/catalog',{cache:'no-store'}),fetch('/api/v1/stock/balances',{cache:'no-store'})]);
  if (catalogResponse.status === 401 || balanceResponse.status === 401) { signedOut('Bitte erneut anmelden. Eine offene Buchung anschließend mit demselben Konto erneut senden.'); return; }
  if (!catalogResponse.ok || !balanceResponse.ok) throw new Error('unavailable');
  catalog = await catalogResponse.json(); balances = await balanceResponse.json();
  for (const balance of balances) {
   const row = document.createElement('tr');
   for (const text of [`${balance.product} · ${balance.lot}`,balance.location,stateNames[balance.state],`${balance.quantity} ${balance.unit}`,`${balance.usable_quantity} ${balance.unit}`,`${balance.expires_on || 'Ungeklärt'}${balance.blocked ? ' · Gesperrt' : ''}`]) {
    const cell = document.createElement('td'); cell.textContent = text; row.append(cell);
   }
   body.append(row);
  }
  stockStatus.textContent = `${balances.length} Bestandszeilen · Datenstand ${new Date(balanceResponse.headers.get('X-Data-As-Of')).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})}${balances.length === 500 ? ' · Anzeige auf 500 Zeilen begrenzt' : ''}`;
  if (!pending) {
   const hasManager = locations.some(location => can(location,['material_manager']));
   const hasHelper = locations.some(location => can(location,['helper','material_manager']));
   booking.hidden = !hasHelper;
   options(document.querySelector('#action'), [...(hasManager ? [{id:'receipts',label:'Wareneingang'},{id:'transfers',label:'Umlagerung'}] : []),...(hasHelper ? [{id:'consumptions',label:'Entnahme'}] : [])]);
   bookingChoices();
  }
  lockBooking(Boolean(pending));
 } catch { stockStatus.textContent = 'Bestandsdaten nicht verfügbar. Keine Buchung ohne aktuellen Datenstand.'; if (!pending) booking.hidden = true; }
}
async function load() {
 list.replaceChildren(); status.textContent = 'Lagerorte werden geladen …';
 try {
  const response = await fetch('/api/v1/locations', {cache:'no-store'});
  if (response.status === 401) { signedOut('Bitte erneut anmelden.'); return; }
  if (!response.ok) throw new Error('unavailable');
  locations = await response.json();
  for (const location of locations) {
   const item = document.createElement('li'); const name = document.createElement('strong'); name.textContent = location.name;
   const detail = document.createElement('p'); detail.textContent = `${location.site} · ${location.operator}`;
   item.append(name,detail); list.append(item);
  }
  status.textContent = pending ? 'Eine Buchung ist noch nicht bestätigt. Mit demselben Konto erneut senden.' : `${locations.length} Lagerorte geladen.`;
  await loadStock();
 } catch { status.textContent = 'Daten konnten nicht geladen werden. Verbindung prüfen und erneut laden.'; if (!pending) booking.hidden = true; }
}
async function restore() {
 try {
  const response = await fetch('/api/v1/me',{cache:'no-store'});
  if (response.status === 401) { signedOut('Bitte anmelden.'); return; }
  if (!response.ok) throw new Error('unavailable');
  user = await response.json(); csrf = user.csrf_token;
  document.querySelector('#account').textContent = `Angemeldet als ${user.username}`;
  login.hidden = true; inventory.hidden = false;
  document.querySelector('#logout').disabled = !csrf;
  await load();
 } catch { status.textContent = 'Verbindung zum Server nicht verfügbar.'; }
}
login.addEventListener('submit',async event => {
 event.preventDefault(); const button = login.querySelector('button'); button.disabled = true;
 const values = new FormData(login);
 try {
  const response = await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:values.get('username'),password:values.get('password')})});
  const result = await response.json();
  if (!response.ok) { signedOut(result.message || 'Anmeldung fehlgeschlagen.'); return; }
  csrf = result.csrf_token; await restore();
 } catch { status.textContent = 'Anmeldung nicht möglich. Verbindung prüfen.'; }
 finally { login.elements.password.value = ''; button.disabled = false; }
});
document.querySelector('#logout').addEventListener('click',async () => {
 try {
  const response = await fetch('/api/v1/auth/logout',{method:'POST',headers:{'X-CSRF-Token':csrf}});
  if (response.ok || response.status === 401) signedOut(pending ? 'Abgemeldet. Eine unbestätigte Buchung benötigt erneute Anmeldung mit demselben Konto.' : 'Abgemeldet.');
  else status.textContent = 'Abmeldung fehlgeschlagen. Bitte erneut versuchen.';
 } catch { status.textContent = 'Abmeldung konnte nicht bestätigt werden. Verbindung prüfen.'; }
});
booking.addEventListener('submit',async event => {
 event.preventDefault(); if (!user || pending && pending.owner !== user.id) return;
 if (!pending) {
  const action = document.querySelector('#action').value;
  const quantity = document.querySelector('#quantity').valueAsNumber;
  if (!Number.isSafeInteger(quantity) || quantity < 1 || quantity > 1000000000) return;
  const data = {quantity,note:document.querySelector('#note').value};
  if (action === 'receipts') {
   data.state = document.querySelector('#receipt-state').value;
   data.batch_id = document.querySelector('#batch').value; data.to_location_id = document.querySelector('#target').value;
   if (!data.batch_id || !data.to_location_id) { status.textContent = 'Charge und Zielort auswählen.'; return; }
  } else {
   const selection = document.querySelector('#source').value;
   const source = balances.find(balance => `${balance.batch_id}|${balance.location_id}|${balance.state}` === selection);
   if (!source) { status.textContent = 'Einen zulässigen Quellbestand auswählen.'; return; }
   Object.assign(data,{batch_id:source.batch_id,from_location_id:source.location_id,state:source.state,expected_version:source.version});
   if (action === 'transfers') {
    data.to_location_id = document.querySelector('#target').value;
    if (!data.to_location_id || data.to_location_id === source.location_id) { status.textContent = 'Einen anderen Zielort auswählen.'; return; }
   }
  }
  pending = {owner:user.id,action,data,key:crypto.randomUUID(),uncertain:false};
 }
 lockBooking(true); const button = booking.querySelector('button'); button.disabled = true;
 try {
  const response = await fetch(`/api/v1/stock/${pending.action}`,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':pending.key},body:JSON.stringify(pending.data)});
  const result = await response.json();
  if (response.status === 401) { pending.uncertain = true; signedOut('Bitte mit demselben Konto erneut anmelden und die offene Buchung erneut senden.'); return; }
  if (response.status >= 500 || !response.ok && pending.uncertain) throw new Error('uncertain');
  pending = null; lockBooking(false); await loadStock();
  status.textContent = response.ok ? `Buchung bestätigt: ${result.movement_id}` : result.message || 'Buchung abgelehnt.';
 } catch { if (pending) pending.uncertain = true; status.textContent = 'Buchung nicht bestätigt. Erneut senden verwendet denselben Schlüssel. Diese Seite bis zur Klärung nicht neu laden.'; }
 finally { if (user) lockBooking(Boolean(pending)); }
});
document.querySelector('#action').addEventListener('change',bookingChoices);
document.querySelector('#reload').addEventListener('click',load);
window.addEventListener('beforeunload',event => {if (pending) {event.preventDefault();event.returnValue = '';}});
restore();
