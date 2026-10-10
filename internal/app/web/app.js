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
 csrf = ''; user = null; countDraft = null; checkDraft = null; packDraft = null; list.replaceChildren(); document.querySelector('#stock-rows').replaceChildren(); document.querySelector('#history-rows').replaceChildren();
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
  status.textContent = (pending || packPending) ? 'Ein Vorgang ist noch nicht bestätigt. Mit demselben Konto erneut senden.' : `${locations.length} Lagerorte geladen.`;
  await loadStock();
  if (user) await loadPacklists();
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
  if (response.ok || response.status === 401) signedOut((pending || packPending) ? 'Abgemeldet. Eine unbestätigte Buchung benötigt erneute Anmeldung mit demselben Konto.' : 'Abgemeldet.');
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
  pending = null; countDraft = null; lockBooking(false); historyOffset = 0; await loadStock(); if (user) await loadPacklists();
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
window.addEventListener('beforeunload',event => {if (pending || packPending) {event.preventDefault();event.returnValue = '';}});


let packVersions = [], packDraft = null, checkDraft = null, packPending = null, material = null;
const materialNames = {ready:'Materialbestand erfüllt · physisch geprüft',needs_check:'Physische Prüfung erforderlich',shortfall:'Nutzbarer Sollbestand unterschritten',unknown:'Kritische Materialdaten ungeklärt',physical_failed:'Physische Abweichung festgestellt',obsolete:'Sollversion ersetzt'};
function productChoices() {
 return [...new Map(catalog.map(batch => [batch.product_id,{id:batch.product_id,label:`${batch.product} · ${batch.unit}`}])).values()];
}
function packControls() {
 if (!user) return;
 const manager = locations.some(location => can(location,['material_manager']));
 const chosen = packVersions.find(pack => pack.id === document.querySelector('#pack-choice').value);
 const target = locations.find(location => location.id === chosen?.location_id);
 document.querySelector('#pack-editor').hidden = !manager && !packPending;
 document.querySelector('#material-check').hidden = packPending?.kind !== 'check' && (!target || !can(target,['helper','material_manager']) || !material?.current_version);
 for (const form of [document.querySelector('#pack-editor'),document.querySelector('#material-check')]) for (const control of form.elements) control.disabled = Boolean(packPending);
 document.querySelector('#pack-choice').disabled = Boolean(packPending);
 document.querySelector('#pack-location').disabled = Boolean(packPending || packDraft);
 document.querySelector('#check-start').disabled = Boolean(packPending) || !material?.current_version;
 document.querySelector('#check-result').disabled = Boolean(packPending) || !checkDraft;
 document.querySelector('#check-note').disabled = Boolean(packPending) || !checkDraft;
 const publish = document.querySelector('#pack-editor button[type=submit]');
 const check = document.querySelector('#material-check button[type=submit]');
 publish.disabled = packPending ? packPending.owner !== user?.id || packPending.kind !== 'publish' : !packDraft;
 check.disabled = packPending ? packPending.owner !== user?.id || packPending.kind !== 'check' : !checkDraft;
 publish.textContent = packPending?.kind === 'publish' ? 'Sollversion erneut senden' : 'Neue Sollversion speichern';
 check.textContent = packPending?.kind === 'check' ? 'Prüfnachweis erneut senden' : 'Prüfnachweis speichern';
 for (const id of ['pack-title','pack-hours','pack-note','pack-add-item']) document.querySelector(`#${id}`).disabled = Boolean(packPending) || !packDraft;
 document.querySelector('#pack-edit-basis').textContent = packDraft ? `Bearbeitung auf Basis von Sollversion ${packDraft.version}.` : 'Lagerort wählen und Bearbeitung beginnen.';
 document.querySelector('#check-basis').textContent = checkDraft ? 'Prüfung gestartet. Der Datenstand bleibt bis zur Bestätigung fest; Änderungen erfordern eine erneute Prüfung.' : 'Vor dem Prüfen ausdrücklich beginnen.';
}
function addPackItem(item = {}) {
 const row = document.createElement('fieldset'); const article = document.createElement('select'); article.className = 'pack-product'; article.required = true; options(article,productChoices()); if (item.product_id) article.value = item.product_id;
 const quantity = document.createElement('input'); quantity.className = 'pack-quantity'; quantity.type = 'number'; quantity.min = '1'; quantity.max = '1000000000'; quantity.step = '1'; quantity.required = true; quantity.value = item.quantity || 1;
 const days = document.createElement('input'); days.className = 'pack-days'; days.type = 'number'; days.min = '0'; days.max = '3650'; days.step = '1'; days.required = true; days.value = item.min_remaining_days || 0;
 for (const [text,control] of [['Artikel',article],['Sollmenge in Basiseinheiten',quantity],['Mindestrestlaufzeit in Tagen',days]]) {const label = document.createElement('label');label.textContent = text;label.append(control);row.append(label);}
 const remove = document.createElement('button'); remove.type = 'button'; remove.textContent = 'Sollposition entfernen'; remove.addEventListener('click',() => row.remove());row.append(remove);document.querySelector('#pack-items').append(row);
}
async function loadMaterial() {
 material = null; document.querySelector('#readiness-status').textContent = 'Materialbewertung wird geladen …'; const id = document.querySelector('#pack-choice').value;
 document.querySelector('#pack-rows').replaceChildren();document.querySelector('#readiness-reasons').replaceChildren();document.querySelector('#proof-history').replaceChildren();document.querySelector('#last-proof').textContent = '';
 if (!id) {document.querySelector('#readiness-status').textContent = 'Noch keine Sollpackliste vorhanden.';packControls();return false;}
 try {
  const [response,proofResponse] = await Promise.all([fetch(`/api/v1/packlists/${id}/readiness`,{cache:'no-store'}),fetch(`/api/v1/packlists/${id}/checks`,{cache:'no-store'})]);
  if (response.status === 401 || proofResponse.status === 401) {signedOut('Bitte erneut anmelden.');return false;}
  if (!response.ok || !proofResponse.ok) throw new Error('unavailable');
  const result = await response.json();if (document.querySelector('#pack-choice').value !== id) return false;material = result;
  document.querySelector('#readiness-status').textContent = `${materialNames[result.status]} · Datenstand ${new Date(result.data_as_of).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})}`;
  for (const reason of result.reasons) {const li = document.createElement('li');li.textContent = reason;document.querySelector('#readiness-reasons').append(li);}
  for (const item of result.items) {const row = document.createElement('tr');for (const text of [item.product,`${item.quantity} ${item.unit}`,item.physical_quantity,item.usable_quantity,item.shortfall,`${item.min_remaining_days} Tage${item.unknown_quantity ? ` · ${item.unknown_quantity} ungeklärt` : ''}`]) {const td = document.createElement('td');td.textContent = text;row.append(td);}document.querySelector('#pack-rows').append(row);}
  if (result.last_check) {const proof = result.last_check;document.querySelector('#last-proof').textContent = `Letzte physische Prüfung: ${proof.username} · ${new Date(proof.created_at).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})} · ${proof.current ? 'aktuell' : 'veraltet'} · ${proof.note}`;}
  for (const proof of await proofResponse.json()) {const li = document.createElement('li');li.textContent = `${new Date(proof.created_at).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})} · ${proof.username} · ${proof.physical_passed ? 'Keine Abweichung' : 'Abweichung'} · ${proof.note}`;document.querySelector('#proof-history').append(li);}
  if (proofResponse.headers.get('X-Has-More') === 'true') {const li = document.createElement('li');li.textContent = 'Anzeige auf die letzten 50 Prüfnachweise begrenzt.';document.querySelector('#proof-history').append(li);}
  packControls();return true;
 } catch {document.querySelector('#readiness-status').textContent = 'Materialbewertung nicht verfügbar. Keine Prüfung mit ungeklärtem Datenstand.';packControls();return false;}
}
async function loadPacklists() {
 try {
  const response = await fetch('/api/v1/packlists?history=true',{cache:'no-store'});
  if (response.status === 401) {signedOut('Bitte erneut anmelden.');return false;}
  if (!response.ok) throw new Error('unavailable');packVersions = await response.json();
  options(document.querySelector('#pack-choice'),packVersions.map(pack => ({id:pack.id,label:`${pack.title} · ${locations.find(location => location.id === pack.location_id)?.name || ''} · Sollversion ${pack.version}`})));
  if (!packDraft) options(document.querySelector('#pack-location'),locations.filter(location => can(location,['material_manager'])).map(location => ({id:location.id,label:location.name})));
  document.querySelector('#pack-status').textContent = `${packVersions.length} Sollversionen geladen.${response.headers.get('X-Has-More') === 'true' ? ' Anzeige auf 100 Versionen begrenzt.' : ''}`;
  await loadMaterial(); return true;
 } catch {material = null;document.querySelector('#pack-status').textContent = 'Sollpacklisten nicht verfügbar.';packControls();return false;}
}
document.querySelector('#pack-choice').addEventListener('change',() => {checkDraft = null;loadMaterial();});
document.querySelector('#pack-refresh').addEventListener('click',loadPacklists);
document.querySelector('#pack-edit-start').addEventListener('click',async () => {
 if (!user || packPending) return;packDraft = null;if (!await loadPacklists() || !user || packPending) return;
 const location = document.querySelector('#pack-location').value;if (!location) return;
 const existing = packVersions.find(pack => pack.location_id === location);
 packDraft = {owner:user.id,location,version:existing?.version || 0};
 document.querySelector('#pack-title').value = existing?.title || 'Material-Sollpackliste';document.querySelector('#pack-hours').value = existing?.check_valid_hours || 24;document.querySelector('#pack-note').value = '';
 document.querySelector('#pack-items').replaceChildren();for (const item of existing?.items || [{}]) addPackItem(item);packControls();
});
document.querySelector('#pack-add-item').addEventListener('click',() => {if (packDraft && document.querySelectorAll('#pack-items fieldset').length < 100) addPackItem();});
document.querySelector('#check-start').addEventListener('click',async () => {
 if (!user || packPending) return;checkDraft = null;if (!await loadMaterial() || !user || !material.current_version) return;
 checkDraft = {owner:user.id,id:material.packlist.id,basis:material.basis_hash};document.querySelector('#check-result').value = '';document.querySelector('#check-note').value = '';packControls();
});
async function sendPackMutation(kind) {
 if (!user || packPending && (packPending.owner !== user.id || packPending.kind !== kind)) return;
 if (!packPending) {
  let data,path;
  if (kind === 'publish') {
   if (!packDraft || packDraft.owner !== user.id) return;
   const items = [...document.querySelectorAll('#pack-items fieldset')].map(row => ({product_id:row.querySelector('.pack-product').value,quantity:row.querySelector('.pack-quantity').valueAsNumber,min_remaining_days:row.querySelector('.pack-days').valueAsNumber}));
   if (!items.length || new Set(items.map(item => item.product_id)).size !== items.length) {status.textContent = 'Mindestens eine eindeutige Sollposition angeben.';return;}
   data = {location_id:packDraft.location,expected_version:packDraft.version,title:document.querySelector('#pack-title').value,check_valid_hours:document.querySelector('#pack-hours').valueAsNumber,note:document.querySelector('#pack-note').value,items};path = '/api/v1/packlists';
  } else {
   if (!checkDraft || checkDraft.owner !== user.id || checkDraft.id !== document.querySelector('#pack-choice').value) return;
   if (!['passed','failed'].includes(document.querySelector('#check-result').value)) {status.textContent = 'Prüfergebnis ausdrücklich auswählen.';return;}
   data = {basis_hash:checkDraft.basis,physical_passed:document.querySelector('#check-result').value === 'passed',note:document.querySelector('#check-note').value};path = `/api/v1/packlists/${checkDraft.id}/checks`;
  }
  packPending = {owner:user.id,kind,path,data,key:crypto.randomUUID(),uncertain:false};
 }
 packControls();document.querySelector(`#${kind === 'publish' ? 'pack-editor' : 'material-check'} button[type=submit]`).disabled = true;
 try {
  const response = await fetch(packPending.path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':packPending.key},body:JSON.stringify(packPending.data)});const result = await response.json();
  if (response.status === 401) {packPending.uncertain = true;signedOut('Mit demselben Konto anmelden und offenen Packlistenvorgang erneut senden.');return;}
  if (response.status >= 500 || !response.ok && packPending.uncertain) throw new Error('uncertain');
  packPending = null;packDraft = null;checkDraft = null;
  await loadPacklists();if (response.ok && kind === 'publish') {document.querySelector('#pack-choice').value = result.id;await loadMaterial();}
  status.textContent = response.ok ? 'Packlistenvorgang bestätigt.' : result.message || 'Vorgang abgelehnt. Neu laden und erneut prüfen.';
 } catch {if (packPending) packPending.uncertain = true;status.textContent = 'Vorgang nicht bestätigt. Erneut senden verwendet denselben Datenstand und Schlüssel. Diese Seite bis zur Klärung nicht neu laden.';}
 finally {if (user) packControls();}
}
document.querySelector('#pack-editor').addEventListener('submit',event => {event.preventDefault();sendPackMutation('publish');});
document.querySelector('#material-check').addEventListener('submit',event => {event.preventDefault();sendPackMutation('check');});
restore();
