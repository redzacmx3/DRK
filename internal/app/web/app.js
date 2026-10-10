const status = document.querySelector('#status');
const login = document.querySelector('#login');
const inventory = document.querySelector('#inventory');
const list = document.querySelector('#locations');
const booking = document.querySelector('#booking');
const stockStatus = document.querySelector('#stock-status');
let csrf = '', user = null, locations = [], balances = [], catalog = [], movements = [], historyOffset = 0, countDraft = null, pending = null;
const kindNames = {receipt:'Wareneingang',transfer:'Umlagerung',consumption:'Entnahme',inventory:'Inventur',reversal:'Gegenbuchung'};
const stateNames = {available:'Verfügbar',quarantined:'Quarantäne',damaged:'Beschädigt'};
function signedOut(message) {
 csrf = ''; user = null; countDraft = null; list.replaceChildren(); document.querySelector('#stock-rows').replaceChildren(); document.querySelector('#history-rows').replaceChildren();
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
 booking.querySelector('button[type=submit]').disabled = pending && pending.owner !== user?.id;
 booking.querySelector('button[type=submit]').textContent = pending ? 'Buchung erneut senden' : 'Buchung bestätigen';
 if (!pending && document.querySelector('#action').value === 'inventories') {
  for (const id of ['batch','target','receipt-state']) document.querySelector(`#${id}`).disabled = Boolean(countDraft);
  document.querySelector('#quantity').disabled = !countDraft;
  booking.querySelector('button[type=submit]').disabled = !countDraft;
 }

}
function bookingChoices() {
 if (!user || pending) return;
 const action = document.querySelector('#action').value;
 const managers = locations.filter(location => can(location,['material_manager']));
 document.querySelector('#batch-field').hidden = !['receipts','inventories'].includes(action);
 document.querySelector('#receipt-state-field').hidden = !['receipts','inventories'].includes(action);
 document.querySelector('#source-field').hidden = !['transfers','consumptions'].includes(action);
 document.querySelector('#target-field').hidden = !['receipts','transfers','inventories'].includes(action);
 document.querySelector('#movement-field').hidden = action !== 'reversals';
 document.querySelector('#start-count').hidden = action !== 'inventories';
 document.querySelector('#count-basis').textContent = action === 'inventories' ? (countDraft ? `Zählung gestartet. Basis: ${countDraft.quantity} Basiseinheiten. Änderungen nach dem Zählstart erfordern einen neuen Abgleich.` : 'Zählziel auswählen und Zählung beginnen.') : '';
 document.querySelector('#start-count').textContent = countDraft ? 'Zählung neu beginnen' : 'Zählung beginnen';
 document.querySelector('#quantity-field').hidden = action === 'reversals';
 document.querySelector('#quantity-label').textContent = action === 'inventories' ? 'Physisch gezählte Menge' : 'Menge';
 document.querySelector('#quantity').min = action === 'inventories' ? '0' : '1';
 document.querySelector('#quantity').required = action !== 'reversals';
 document.querySelector('#note').required = ['inventories','reversals'].includes(action);
 options(document.querySelector('#batch'), catalog.map(batch => ({id:batch.id,label:`${batch.product} · ${batch.lot} · ${batch.unit}`})));
 options(document.querySelector('#target'), managers.map(location => ({id:location.id,label:location.name})));
 const reversible = movements.filter(move => !move.reversed_by && move.kind !== 'reversal' && move.quantity > 0 && [move.from_location_id,move.to_location_id].filter(Boolean).every(id => managers.some(location => location.id === id)));
 options(document.querySelector('#movement'),reversible.map(move => ({id:move.id,label:`${kindNames[move.kind]} · ${move.product} · ${move.lot} · ${move.quantity} ${move.unit} · ${new Date(move.created_at).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})}`})));
 const sources = balances.filter(balance => {
  const location = locations.find(value => value.id === balance.location_id);
  return location && balance.quantity > 0 && can(location,action === 'consumptions' ? ['helper','material_manager'] : ['material_manager']) && (action !== 'consumptions' || balance.usable_quantity > 0);
 });
 options(document.querySelector('#source'), sources.map(balance => ({id:`${balance.batch_id}|${balance.location_id}|${balance.state}`,label:`${balance.product} · ${balance.lot} · ${balance.location} · ${balance.quantity} ${balance.unit} · ${stateNames[balance.state]}`})));
}
async function loadHistory() {
 const body = document.querySelector('#history-rows'); body.replaceChildren();
 const message = document.querySelector('#history-status'); message.textContent = 'Journal wird geladen …';
 try {
  const response = await fetch(`/api/v1/stock/movements?limit=50&offset=${historyOffset}`,{cache:'no-store'});
  if (response.status === 401) {signedOut('Bitte erneut anmelden.');return;}
  if (!response.ok) throw new Error('unavailable');
  movements = await response.json();
  for (const move of movements) {
   const row = document.createElement('tr');
   const amount = move.kind === 'inventory' ? `${move.basis_quantity} → ${move.counted_quantity} ${move.unit}` : `${move.quantity} ${move.unit}`;
   for (const text of [new Date(move.created_at).toLocaleString('de-DE',{timeZone:'Europe/Berlin'}),`${kindNames[move.kind]}${move.reversed_by ? ' · Gegengebucht' : ''}`,`${move.product} · ${move.lot}`,[move.from_location,move.to_location].filter(Boolean).join(' → '),amount,move.note]) {
    const cell = document.createElement('td');cell.textContent = text;row.append(cell);
   }
   body.append(row);
  }
  message.textContent = movements.length ? `Einträge ${historyOffset+1}–${historyOffset+movements.length}` : 'Keine weiteren Einträge.';
  document.querySelector('#history-newer').disabled = historyOffset === 0;
  document.querySelector('#history-older').disabled = response.headers.get('X-Has-More') !== 'true';
 } catch {movements = [];message.textContent = 'Bewegungsjournal nicht verfügbar.';}
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
   options(document.querySelector('#action'), [...(hasManager ? [{id:'receipts',label:'Wareneingang'},{id:'transfers',label:'Umlagerung'},{id:'inventories',label:'Inventur'},{id:'reversals',label:'Gegenbuchung'}] : []),...(hasHelper ? [{id:'consumptions',label:'Entnahme'}] : [])]);
   bookingChoices();
  }
  await loadHistory();
  if (user && !pending) bookingChoices();
  if (user) lockBooking(Boolean(pending));
  return Boolean(user);
 } catch { stockStatus.textContent = 'Bestandsdaten nicht verfügbar. Keine Buchung ohne aktuellen Datenstand.'; if (!pending) booking.hidden = true; return false; }
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
  if (action !== 'reversals' && (!Number.isSafeInteger(quantity) || quantity < (action === 'inventories' ? 0 : 1) || quantity > 1000000000)) return;
  const data = {quantity,note:document.querySelector('#note').value};
  let path = `/api/v1/stock/${action}`;
  if (action === 'inventories') {
   delete data.quantity;
   Object.assign(data,{batch_id:document.querySelector('#batch').value,location_id:document.querySelector('#target').value,state:document.querySelector('#receipt-state').value,counted_quantity:quantity});
   if (!countDraft || countDraft.owner !== user.id || countDraft.batch !== data.batch_id || countDraft.location !== data.location_id || countDraft.state !== data.state) {status.textContent = 'Zählung für dieses Ziel zuerst beginnen.';return;}
   data.expected_version = countDraft.version;
   if (!data.batch_id || !data.location_id) {status.textContent = 'Charge und Zählort auswählen.';return;}
  } else if (action === 'reversals') {
   delete data.quantity;
   const original = movements.find(move => move.id === document.querySelector('#movement').value);
   if (!original) {status.textContent = 'Eine noch nicht gegengebuchte Bewegung auswählen.';return;}
   const affected = [...new Set([original.from_location_id,original.to_location_id].filter(Boolean))];
   data.expected_versions = [];
   for (const location of affected) {
    const balance = balances.find(row => row.batch_id === original.batch_id && row.location_id === location && row.state === original.state);
    if (!balance) {status.textContent = 'Betroffene Bestände neu laden, bevor gegengebucht wird.';return;}
    data.expected_versions.push({location_id:location,state:original.state,version:balance.version});
   }
   path = `/api/v1/stock/movements/${original.id}/reversal`;
  } else if (action === 'receipts') {
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
  pending = {owner:user.id,path,data,key:crypto.randomUUID(),uncertain:false};
 }
 lockBooking(true); const button = booking.querySelector('button[type=submit]'); button.disabled = true;
 try {
  const response = await fetch(pending.path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':pending.key},body:JSON.stringify(pending.data)});
  const result = await response.json();
  if (response.status === 401) { pending.uncertain = true; signedOut('Bitte mit demselben Konto erneut anmelden und die offene Buchung erneut senden.'); return; }
  if (response.status >= 500 || !response.ok && pending.uncertain) throw new Error('uncertain');
  pending = null; countDraft = null; lockBooking(false); historyOffset = 0; await loadStock();
  status.textContent = response.ok ? `Buchung bestätigt: ${result.movement_id}` : result.message || 'Buchung abgelehnt.';
 } catch { if (pending) pending.uncertain = true; status.textContent = 'Buchung nicht bestätigt. Erneut senden verwendet denselben Schlüssel. Diese Seite bis zur Klärung nicht neu laden.'; }
 finally { if (user) lockBooking(Boolean(pending)); }
});
document.querySelector('#action').addEventListener('change',() => {countDraft = null;document.querySelector('#quantity').value = document.querySelector('#action').value === 'inventories' ? '' : '1'; bookingChoices();lockBooking(false);});
document.querySelector('#start-count').addEventListener('click',async () => {
 if (!user || pending || document.querySelector('#action').value !== 'inventories') return;
 countDraft = null; if (!await loadStock()) return;
 if (!user || pending || document.querySelector('#action').value !== 'inventories') return;
 const batch = document.querySelector('#batch').value, location = document.querySelector('#target').value, state = document.querySelector('#receipt-state').value;
 if (!batch || !location) {status.textContent = 'Charge und Zählort auswählen.';return;}
 const basis = balances.find(balance => balance.batch_id === batch && balance.location_id === location && balance.state === state);
 countDraft = {owner:user.id,batch,location,state,version:basis?.version || 0,quantity:basis?.quantity || 0};
 document.querySelector('#quantity').value = '';bookingChoices();lockBooking(false);
});
document.querySelector('#reload').addEventListener('click',load);
 document.querySelector('#history-older').addEventListener('click',async () => {historyOffset += 50;await loadHistory();bookingChoices();});
 document.querySelector('#history-newer').addEventListener('click',async () => {historyOffset = Math.max(0,historyOffset-50);await loadHistory();bookingChoices();});
window.addEventListener('beforeunload',event => {if (pending) {event.preventDefault();event.returnValue = '';}});
restore();
