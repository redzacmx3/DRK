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
 stockIntakeDraft=null;stockIntakeLoading=false;document.querySelector("#stock-import-file").value="";document.querySelector("#stock-import-rows").replaceChildren();document.querySelector("#stock-import-locations").replaceChildren();document.querySelector("#stock-import-status").textContent="";document.querySelector("#stock-import").hidden=true;
 intakeDraft=null;intakeLoading=false;document.querySelector("#device-import-file").value="";document.querySelector("#device-import-rows").replaceChildren();document.querySelector("#device-import-locations").replaceChildren();document.querySelector("#device-import-status").textContent="";document.querySelector("#device-import").hidden=true;
 deviceDraft = null; deviceRecords = []; document.querySelector('#document-file').value=''; document.querySelector('#document-list').replaceChildren(); document.querySelector('#document-status').textContent=''; document.querySelector('#document-upload-form').hidden=true; document.querySelector('#device-basis').textContent = ''; document.querySelector('#device-control-form').hidden = true; document.querySelector('#devices-list').replaceChildren(); document.querySelector('#devices-status').textContent = '';
 csrf = ''; user = null; deviceCheckDraft = null; packAssessment = null; document.querySelector('#physical-device-results').replaceChildren();document.querySelector('#device-proof-history').replaceChildren();document.querySelector('#last-device-proof').textContent='';document.querySelector('#physical-device-check').hidden=true; countDraft = null; checkDraft = null; packDraft = null; scannerReset(); list.replaceChildren(); document.querySelector('#stock-rows').replaceChildren(); document.querySelector('#history-rows').replaceChildren();
 document.querySelector('#assessment-status').textContent='';document.querySelector('#assessment-reasons').replaceChildren();document.querySelector('#pack-devices').replaceChildren();
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
   const amount = move.pack_count ? `${move.quantity} ${move.unit} · ${move.pack_count} Packungen × ${move.pack_quantity} ${move.unit} · ${move.identifier_kind.toUpperCase()} ${move.identifier_value}` : move.kind === 'inventory' ? `${move.basis_quantity} → ${move.counted_quantity} ${move.unit}` : `${move.quantity} ${move.unit}`;
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
  locations = await response.json();stockIntakeControls();
  for (const location of locations) {
   const item = document.createElement('li'); const name = document.createElement('strong'); name.textContent = location.name;
   const detail = document.createElement('p'); detail.textContent = `${location.site} · ${location.operator}`;
   item.append(name,detail); list.append(item);
  }
  status.textContent = (pending || packPending || identifierPending || intakePending || stockIntakePending) ? 'Ein Vorgang ist noch nicht bestätigt. Mit demselben Konto erneut senden.' : `${locations.length} Lagerorte geladen.`;
  await loadStock();
  if (user) { await loadPacklists(); if (user) await loadScannerData(); if(user) {await loadDevices();lockDeviceForm();} }
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
  if (response.ok || response.status === 401) signedOut((pending || packPending || identifierPending || intakePending || stockIntakePending) ? 'Abgemeldet. Eine unbestätigte Buchung benötigt erneute Anmeldung mit demselben Konto.' : 'Abgemeldet.');
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
  if (scanBookingBasis && scanBookingBasis.owner === user.id && ['receipts','transfers','consumptions'].includes(action)) {
   if (data.batch_id !== scanBookingBasis.batch || data.quantity !== scanBookingBasis.quantity) {status.textContent = 'Scanner-Vorauswahl verändert. Erneut scannen oder die Basismenge bewusst manuell eingeben.';return;}
   data.identifier_id = scanBookingBasis.identifier_id;data.pack_count = scanBookingBasis.pack_count;
  }
  pending = {owner:user.id,path,data,key:crypto.randomUUID(),uncertain:false};
 }
 lockBooking(true); const button = booking.querySelector('button[type=submit]'); button.disabled = true;
 try {
  const response = await fetch(pending.path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':pending.key},body:JSON.stringify(pending.data)});
  const result = await response.json();
  if (response.status === 401) { pending.uncertain = true; signedOut('Bitte mit demselben Konto erneut anmelden und die offene Buchung erneut senden.'); return; }
  if (response.status >= 500 || !response.ok && pending.uncertain) throw new Error('uncertain');
  pending = null; countDraft = null; scanBookingBasis = null; lockBooking(false); historyOffset = 0; await loadStock(); if (user) { await loadPacklists(); if (user) await loadScannerData(); }
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
window.addEventListener('pagehide',stopCamera);
window.addEventListener('beforeunload',event => {if (pending || packPending || identifierPending || devicePending || documentPending || intakePending || stockIntakePending) {event.preventDefault();event.returnValue = '';}});


let deviceCheckDraft = null, packAssessment = null;
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
 deviceCheckControls(target);
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
 for (const id of ['pack-title','pack-hours','pack-note','pack-add-item','pack-add-device']) document.querySelector(`#${id}`).disabled = Boolean(packPending) || !packDraft;
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
 material = null; packAssessment = null; document.querySelector('#readiness-status').textContent = 'Materialbewertung wird geladen …'; const id = document.querySelector('#pack-choice').value;
 document.querySelector('#assessment-status').textContent='';document.querySelector('#assessment-reasons').replaceChildren();document.querySelector('#pack-rows').replaceChildren();document.querySelector('#readiness-reasons').replaceChildren();document.querySelector('#proof-history').replaceChildren();document.querySelector('#last-proof').textContent = '';
 if (!id) {document.querySelector('#readiness-status').textContent = 'Noch keine Sollpackliste vorhanden.';packControls();return false;}
 try {
  const [response,proofResponse,assessmentResponse] = await Promise.all([fetch(`/api/v1/packlists/${id}/readiness`,{cache:'no-store'}),fetch(`/api/v1/packlists/${id}/checks`,{cache:'no-store'}),fetch(`/api/v1/packlists/${id}/assessment`,{cache:'no-store'})]);
  if (response.status === 401 || proofResponse.status === 401) {signedOut('Bitte erneut anmelden.');return false;}
  if (!response.ok || !proofResponse.ok) throw new Error('unavailable');
  if(!assessmentResponse.ok)throw new Error('assessment');const assessment=await assessmentResponse.json();
 const result = await response.json();if (document.querySelector('#pack-choice').value !== id) return false;material = result;packAssessment = assessment;await loadDeviceProofHistory(id);if(!user||document.querySelector("#pack-choice").value!==id)return false;
 const assessmentNames={blocked:'Sollanforderungen nicht erfüllt',unknown:'Geräte-/Zubehörbewertung ungeklärt',needs_device_check:'Physische Geräte-/Funktionsprüfung fehlt',physically_checked:'Physische Geräte-/Zubehörprüfung dokumentiert',obsolete:'Sollversion veraltet'};
 document.querySelector('#assessment-status').textContent=`${assessmentNames[assessment.status] || 'Bewertung ungeklärt'} · Vollständige Einsatzfreigabe nicht bewertet`;
 for(const reason of assessment.reasons){const li=document.createElement('li');li.textContent=reason;document.querySelector('#assessment-reasons').append(li);}
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
document.querySelector('#pack-choice').addEventListener('change',() => {checkDraft = null;deviceCheckDraft = null;loadMaterial();});
document.querySelector('#pack-refresh').addEventListener('click',loadPacklists);
document.querySelector('#pack-edit-start').addEventListener('click',async () => {
 if (!user || packPending) return;packDraft = null;if (!await loadPacklists() || !user || packPending) return;
 const location = document.querySelector('#pack-location').value;if (!location) return;
 const existing = packVersions.find(pack => pack.location_id === location);
 packDraft = {owner:user.id,location,version:existing?.version || 0};
 document.querySelector('#pack-title').value = existing?.title || 'Material-Sollpackliste';document.querySelector('#pack-hours').value = existing?.check_valid_hours || 24;document.querySelector('#pack-note').value = '';
 await loadDevices();if(!user || packPending)return;document.querySelector('#pack-devices').replaceChildren();for(const device of existing?.devices || [])addPackDevice(device);
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
   data = {location_id:packDraft.location,expected_version:packDraft.version,title:document.querySelector('#pack-title').value,check_valid_hours:document.querySelector('#pack-hours').valueAsNumber,note:document.querySelector('#pack-note').value,items,devices:[...document.querySelectorAll('#pack-devices > fieldset')].map(row=>({device_id:row.querySelector('.pack-device-id').value,accessories:[...row.querySelectorAll('.pack-accessory')].map(accessory=>({product_id:accessory.querySelector('.accessory-product').value,quantity:accessory.querySelector('.accessory-quantity').valueAsNumber}))}))};path = '/api/v1/packlists';
  } else if (kind === 'device') {
   if (!deviceCheckDraft || deviceCheckDraft.owner !== user.id || deviceCheckDraft.id !== document.querySelector('#pack-choice').value) return;
   const rows=[...document.querySelectorAll('#physical-device-results fieldset')];
   if(rows.some(row=>[...row.querySelectorAll('select')].some(select=>!['passed','failed'].includes(select.value)))){status.textContent='Alle Geräteergebnisse ausdrücklich auswählen.';return;}
   data={basis_hash:deviceCheckDraft.basis,results:rows.map(row=>({device_id:row.dataset.deviceId,present:row.querySelector('.device-present').value==='passed',functional:row.querySelector('.device-functional').value==='passed',accessories_complete:row.querySelector('.device-accessories').value==='passed'})),note:document.querySelector('#physical-device-note').value};path=`/api/v1/packlists/${deviceCheckDraft.id}/device-checks`;
  } else {
   if (!checkDraft || checkDraft.owner !== user.id || checkDraft.id !== document.querySelector('#pack-choice').value) return;
   if (!['passed','failed'].includes(document.querySelector('#check-result').value)) {status.textContent = 'Prüfergebnis ausdrücklich auswählen.';return;}
   data = {basis_hash:checkDraft.basis,physical_passed:document.querySelector('#check-result').value === 'passed',note:document.querySelector('#check-note').value};path = `/api/v1/packlists/${checkDraft.id}/checks`;
  }
  packPending = {owner:user.id,kind,path,data,key:crypto.randomUUID(),uncertain:false};
 }
 packControls();document.querySelector(`#${kind === 'publish' ? 'pack-editor' : kind === 'device' ? 'physical-device-check' : 'material-check'} button[type=submit]`).disabled = true;
 try {
  const response = await fetch(packPending.path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':packPending.key},body:JSON.stringify(packPending.data)});const result = await response.json();
  if (response.status === 401) {packPending.uncertain = true;signedOut('Mit demselben Konto anmelden und offenen Packlistenvorgang erneut senden.');return;}
  if (response.status >= 500 || !response.ok && packPending.uncertain) throw new Error('uncertain');
  packPending = null;packDraft = null;checkDraft = null;deviceCheckDraft = null;
  await loadPacklists();if (response.ok && kind === 'publish') {document.querySelector('#pack-choice').value = result.id;await loadMaterial();}
  status.textContent = response.ok ? 'Packlistenvorgang bestätigt.' : result.message || 'Vorgang abgelehnt. Neu laden und erneut prüfen.';
 } catch {if (packPending) packPending.uncertain = true;status.textContent = 'Vorgang nicht bestätigt. Erneut senden verwendet denselben Datenstand und Schlüssel. Diese Seite bis zur Klärung nicht neu laden.';}
 finally {if (user) packControls();}
}
document.querySelector('#pack-editor').addEventListener('submit',event => {event.preventDefault();sendPackMutation('publish');});
document.querySelector('#material-check').addEventListener('submit',event => {event.preventDefault();sendPackMutation('check');});


let scanBookingBasis = null, scannerResult = null, identifierPending = null, identifiers = [], scannerBusy = false, cameraStream = null, cameraGeneration = 0, cameraTimer = null;
const scanNames = {invalid:'Code ungültig',unknown:'Unbekanntes Format',ambiguous:'Format mehrdeutig – ausdrücklich auswählen',unregistered_identifier:'Kennung noch nicht zugeordnet',product_match:'Artikel erkannt – Charge ausdrücklich wählen',batch_match:'Artikel und Charge erkannt',unregistered_batch:'Artikel erkannt – Charge noch nicht angelegt',batch_mismatch:'Verfall widerspricht dem Chargendatum',requires_review:'Fachliche Klärung erforderlich',location_match:'Lagerort erkannt'};
function stopCamera() {
 cameraGeneration++;clearTimeout(cameraTimer);cameraTimer = null;
 cameraStream?.getTracks().forEach(track => track.stop());cameraStream = null;
 document.querySelector('#camera-video').srcObject = null;document.querySelector('#camera-video').hidden = true;
 document.querySelector('#camera-stop').hidden = true;document.querySelector('#camera-start').disabled = Boolean(identifierPending);
}
function scannerReset() {stopCamera();scanBookingBasis = null;scannerResult = null;document.querySelector('#scan-details').replaceChildren();document.querySelector('#scan-handoff').hidden = true;document.querySelector('#label-preview').hidden = true;document.querySelector('#label-image').removeAttribute('src');document.querySelector('#label-print').disabled = true;document.querySelector('#label-copy').disabled = true;}
function scannerControls() {
 if (!user) return;
 const manager = locations.some(location => can(location,['material_manager']));
 const canMap = manager && scannerResult?.status === 'unregistered_identifier' && scannerResult.parsed.value;
 document.querySelector('#identifier-form').hidden = identifierPending?.kind !== 'add' && !canMap;
 document.querySelector('#identifier-revoke').hidden = identifierPending?.kind !== 'revoke' && !manager;
 for (const id of ['identifier-form','identifier-revoke','scan-form']) for (const element of document.querySelector(`#${id}`).elements) element.disabled = Boolean(identifierPending);
 document.querySelector('#camera-start').disabled = Boolean(identifierPending || scannerBusy);
 const supported = 'BarcodeDetector' in window && navigator.mediaDevices?.getUserMedia;
 if (!supported) {document.querySelector('#camera-start').disabled = true;document.querySelector('#camera-status').textContent = 'Kamera-Erkennung in diesem Browser nicht verfügbar. Handscanner oder Texteingabe verwenden.';}
 if (identifierPending) {const form = document.querySelector(identifierPending.kind === 'add' ? '#identifier-form' : '#identifier-revoke');const button = form.querySelector('button[type=submit]');button.disabled = identifierPending.owner !== user.id;button.textContent = 'Vorgang erneut senden';}
 else {document.querySelector('#identifier-form button').textContent = 'Kennung zuordnen';document.querySelector('#identifier-revoke button').textContent = 'Zuordnung widerrufen';}
 document.querySelector('#scan-form button[type=submit]').disabled = Boolean(identifierPending || scannerBusy);
}
function handoffChoices() {
 if (!user || !scannerResult) return;
 const purpose = document.querySelector('#scan-purpose').value;
 const selectedBatch = document.querySelector('#scan-batch').value;
 const sources = balances.filter(balance => balance.batch_id === selectedBatch && balance.quantity > 0 && (purpose !== 'consumptions' || balance.usable_quantity > 0) && locations.some(location => location.id === balance.location_id && can(location,purpose === 'consumptions' ? ['helper','material_manager'] : ['material_manager'])));
 options(document.querySelector('#scan-source'),[{id:'',label:'Quellbestand ausdrücklich auswählen'},...sources.map(balance => ({id:`${balance.batch_id}|${balance.location_id}|${balance.state}`,label:`${balance.location} · ${balance.lot} · ${balance.quantity} ${balance.unit} · ${stateNames[balance.state]}`}))]);
 options(document.querySelector('#scan-target'),[{id:'',label:'Zielort ausdrücklich auswählen'},...locations.filter(location => can(location,['material_manager'])).map(location => ({id:location.id,label:location.name}))]);
 document.querySelector('#scan-source-field').hidden = !['transfers','consumptions'].includes(purpose);document.querySelector('#scan-source').required = ['transfers','consumptions'].includes(purpose);
 document.querySelector('#scan-target-field').hidden = !['receipts','transfers'].includes(purpose);document.querySelector('#scan-target').required = ['receipts','transfers'].includes(purpose);
}
function renderScan(result) {
 scannerResult = result;const details = document.querySelector('#scan-details');details.replaceChildren();
 document.querySelector('#scan-status').textContent = `${scanNames[result.status] || 'Auflösung nicht verfügbar'} · Keine Buchung ausgeführt.`;
 for (const text of [result.product ? `${result.product} · ${result.unit}${result.pack_quantity ? ` · ${result.pack_quantity} Basiseinheiten je Packung` : ''}` : '',result.location ? `${result.location.name} · ${result.location.site}` : '',result.parsed.lot ? `Gescannte Charge: ${result.parsed.lot}` : '',result.parsed.expiry_raw ? `GS1-Verfall: ${result.parsed.expiry_raw} (YYMMDD; ${result.parsed.expiry_precision === 'month' ? 'Monatsangabe' : 'Tagesangabe'})` : '',result.parsed.serial ? `Seriennummer: ${result.parsed.serial}` : '',result.batch ? `Hinterlegte Charge: ${result.batch.lot} · Verfall ${result.batch.expires_on || 'ungeklärt'}${result.batch.blocked ? ' · gesperrt' : ''}` : '',...result.warnings].filter(Boolean)) {const li = document.createElement('li');li.textContent = text;details.append(li);}
 const helper = locations.some(location => can(location,['helper','material_manager']));const manager = locations.some(location => can(location,['material_manager']));
 const handoff = document.querySelector('#scan-handoff');handoff.hidden = !helper || !['batch_match','product_match'].includes(result.status);
 document.querySelector('#scan-open-location').hidden = result.status !== 'location_match';
 document.querySelector('#scan-confirm').checked = false;
 options(document.querySelector('#scan-batch'),[{id:'',label:'Charge ausdrücklich auswählen'},...catalog.filter(batch => batch.product_id === result.product_id).map(batch => ({id:batch.id,label:`${batch.lot} · Verfall ${batch.expires_on || 'ungeklärt'}${batch.blocked ? ' · gesperrt' : ''}`}))]);
 if (result.status === 'batch_match' && result.batch) document.querySelector('#scan-batch').value = result.batch.id;
 options(document.querySelector('#scan-purpose'),[{id:'',label:'Buchungsaktion ausdrücklich auswählen'},...(manager ? [{id:'receipts',label:'Wareneingang'},{id:'transfers',label:'Umlagerung'}] : []),...(helper ? [{id:'consumptions',label:'Entnahme'}] : [])]);
 document.querySelector('#scan-packs-field').hidden = !result.pack_quantity;document.querySelector('#scan-packs').value = '1';document.querySelector('#scan-packs').required = Boolean(result.pack_quantity);
 options(document.querySelector('#identifier-product'),[{id:'',label:'Artikel ausdrücklich auswählen'},...productChoices()]);document.querySelector('#identifier-quantity').value = '';document.querySelector('#identifier-note').value = '';
 handoffChoices();scannerControls();
}
async function resolveScan() {
 if (!user || scannerBusy || identifierPending) return;
 const owner = user.id, text = document.querySelector('#scan-text').value, format = document.querySelector('#scan-format').value;scannerBusy = true;scannerResult = null;document.querySelector('#scan-handoff').hidden = true;document.querySelector('#identifier-form').hidden = true;scannerControls();
 try {
  const response = await fetch('/api/v1/scanner/resolve',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({text,format})});const result = await response.json();
  if (response.status === 401) {signedOut('Bitte erneut anmelden.');return;}if (!user || user.id !== owner) return;
  if (document.querySelector('#scan-text').value !== text || document.querySelector('#scan-format').value !== format) {document.querySelector('#scan-status').textContent = 'Eingabe geändert. Code erneut auflösen.';return;}
  if (!response.ok) {document.querySelector('#scan-status').textContent = result.message || 'Auflösung nicht verfügbar.';return;}
  renderScan(result);
 } catch {document.querySelector('#scan-status').textContent = 'Scannerauflösung nicht verfügbar. Verbindung prüfen; keine Buchung ausgeführt.';}
 finally {scannerBusy = false;scannerControls();}
}
async function loadScannerData() {
 if (!user) return;
 try {
  const response = await fetch('/api/v1/product-identifiers',{cache:'no-store'});if (response.status === 401) {signedOut('Bitte erneut anmelden.');return;}
  if (!response.ok) throw new Error('unavailable');identifiers = await response.json();
  const list = document.querySelector('#identifier-list');list.replaceChildren();
  for (const item of identifiers) {const li = document.createElement('li');li.textContent = `${item.kind.toUpperCase()} ${item.value} · ${item.product} · ${item.pack_quantity} ${item.unit} je Packung · ${item.active ? 'aktiv' : 'widerrufen'}`;list.append(li);}
  document.querySelector('#identifier-status').textContent = `${identifiers.length} Zuordnungen${response.headers.get('X-Has-More') === 'true' ? ' · Anzeige auf 500 begrenzt' : ''}.`;
  if (!identifierPending) options(document.querySelector('#identifier-revoke-choice'),[{id:'',label:'Zuordnung ausdrücklich auswählen'},...identifiers.filter(item => item.active).map(item => ({id:item.id,label:`${item.kind.toUpperCase()} ${item.value} · ${item.product} · ${item.pack_quantity} ${item.unit}`}))]);
  labelChoices();scannerControls();
 } catch {document.querySelector('#identifier-status').textContent = 'Produktkennungen nicht verfügbar.';scannerControls();}
}
async function sendIdentifier(kind) {
 if (!user || identifierPending && (identifierPending.owner !== user.id || identifierPending.kind !== kind)) return;
 if (!identifierPending) {
  let data,path;
  if (kind === 'add') {
   if (scannerResult?.status !== 'unregistered_identifier' || !scannerResult.parsed.value) return;
   data = {product_id:document.querySelector('#identifier-product').value,kind:scannerResult.parsed.kind === 'gs1' ? 'gtin' : scannerResult.parsed.kind,value:scannerResult.parsed.value,pack_quantity:document.querySelector('#identifier-quantity').valueAsNumber,note:document.querySelector('#identifier-note').value};path = '/api/v1/product-identifiers';
  } else {const id = document.querySelector('#identifier-revoke-choice').value;if (!id) return;path = `/api/v1/product-identifiers/${id}/revoke`;data = {note:document.querySelector('#identifier-revoke-note').value};}
  identifierPending = {owner:user.id,kind,path,data,key:crypto.randomUUID(),uncertain:false};stopCamera();
 }
 scannerControls();document.querySelector(`#${kind === 'add' ? 'identifier-form' : 'identifier-revoke'} button`).disabled = true;
 try {
  const response = await fetch(identifierPending.path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':identifierPending.key},body:JSON.stringify(identifierPending.data)});const result = await response.json();
  if (response.status === 401) {identifierPending.uncertain = true;signedOut('Mit demselben Konto anmelden und offenen Kennungsvorgang erneut senden.');return;}
  if (response.status >= 500 || !response.ok && identifierPending.uncertain) throw new Error('uncertain');
  identifierPending = null;await loadScannerData();if (user && document.querySelector('#scan-text').value) await resolveScan();
  status.textContent = response.ok ? 'Kennungsvorgang bestätigt. Keine Bestandsbuchung ausgeführt.' : result.message || 'Kennungsvorgang abgelehnt.';
 } catch {if (identifierPending) identifierPending.uncertain = true;status.textContent = 'Kennungsvorgang nicht bestätigt. Erneut senden verwendet denselben Inhalt und Schlüssel. Seite bis zur Klärung nicht neu laden.';}
 finally {scannerControls();}
}
document.querySelector('#scan-form').addEventListener('submit',event => {event.preventDefault();resolveScan();});
document.querySelector('#scan-text').addEventListener('keydown',event => {if (event.key === 'Enter' && !event.shiftKey) {event.preventDefault();document.querySelector('#scan-form').requestSubmit();}});
for (const id of ['scan-text','scan-format']) document.querySelector(`#${id}`).addEventListener('input',() => {scannerResult = null;document.querySelector('#scan-handoff').hidden = true;document.querySelector('#identifier-form').hidden = true;document.querySelector('#scan-open-location').hidden = true;document.querySelector('#scan-status').textContent = 'Eingabe geändert. Code erneut auflösen.';});
document.querySelector('#scan-separator').addEventListener('click',() => {const input = document.querySelector('#scan-text');input.setRangeText('\x1d',input.selectionStart,input.selectionEnd,'end');input.dispatchEvent(new Event('input'));input.focus();});
for (const id of ['scan-purpose','scan-batch']) document.querySelector(`#${id}`).addEventListener('change',() => {document.querySelector('#scan-confirm').checked = false;handoffChoices();});
document.querySelector('#scan-handoff').addEventListener('submit',event => {
 event.preventDefault();if (!user || !scannerResult || !['batch_match','product_match'].includes(scannerResult.status) || pending || countDraft || identifierPending || !document.querySelector('#scan-confirm').checked) {status.textContent = 'Scan prüfen und offene Buchung/Zählung zuerst abschließen.';return;}
 const batch = catalog.find(batch => batch.id === document.querySelector('#scan-batch').value && batch.product_id === scannerResult.product_id), action = document.querySelector('#scan-purpose').value;
 if (!batch || !['receipts','transfers','consumptions'].includes(action)) return;
 let quantity = null;
 if (scannerResult.pack_quantity) {const packs = document.querySelector('#scan-packs').valueAsNumber;quantity = packs * scannerResult.pack_quantity;if (!Number.isSafeInteger(packs) || packs < 1 || !Number.isSafeInteger(quantity) || quantity > 1000000000) {status.textContent = 'Packungsanzahl beziehungsweise Basismenge außerhalb des Wertebereichs.';return;}}
 document.querySelector('#action').value = action;bookingChoices();
 if (document.querySelector('#action').value !== action) {status.textContent = 'Buchungsaktion nicht erlaubt.';return;}
 if (action === 'receipts') {document.querySelector('#batch').value = batch.id;document.querySelector('#target').value = document.querySelector('#scan-target').value;}
 else {document.querySelector('#source').value = document.querySelector('#scan-source').value;if (!document.querySelector('#source').value) {status.textContent = 'Quellbestand neu laden und ausdrücklich auswählen.';return;}if (action === 'transfers') document.querySelector('#target').value = document.querySelector('#scan-target').value;}
 scanBookingBasis = scannerResult.identifier_id && quantity !== null ? {owner:user.id,batch:batch.id,identifier_id:scannerResult.identifier_id,pack_count:document.querySelector('#scan-packs').valueAsNumber,quantity} : null;
 document.querySelector('#quantity').value = quantity === null ? '' : String(quantity);lockBooking(false);booking.scrollIntoView({behavior:'smooth'});status.textContent = 'Scan in Buchungsformular übernommen. Menge, Orte und Zustand nochmals prüfen und dort ausdrücklich buchen.';
});
for (const id of ['action','batch','source','quantity']) document.querySelector(`#${id}`).addEventListener('input',() => {if (scanBookingBasis && !pending) {scanBookingBasis = null;status.textContent = 'Scanner-Vorauswahl geändert. Menge wird nun als manuelle Basismenge gebucht.';}});
document.querySelector('#scan-open-location').addEventListener('click',async () => {const id = scannerResult?.location?.id;if (!id) return;const pack = packVersions.find(pack => pack.location_id === id);if (!pack) {status.textContent = 'Für diesen Lagerort ist noch keine Sollpackliste hinterlegt.';return;}if (packPending) {status.textContent = 'Offenen Packlistenvorgang zuerst abschließen.';return;}checkDraft = null;document.querySelector('#pack-choice').value = pack.id;await loadMaterial();document.querySelector('#packlists').scrollIntoView({behavior:'smooth'});});
document.querySelector('#identifier-form').addEventListener('submit',event => {event.preventDefault();sendIdentifier('add');});
document.querySelector('#identifier-revoke').addEventListener('submit',event => {event.preventDefault();sendIdentifier('revoke');});
function labelChoices() {
 const kind = document.querySelector('#label-kind').value;
 const items = kind === 'location' ? locations.map(location => ({id:location.id,label:location.name})) : kind === 'product' ? productChoices() : catalog.map(batch => ({id:batch.id,label:`${batch.product} · ${batch.lot}`}));
 options(document.querySelector('#label-object'),items);
}
document.querySelector('#label-kind').addEventListener('change',labelChoices);
document.querySelector('#label-show').addEventListener('click',() => {
 const kind = document.querySelector('#label-kind').value,id = document.querySelector('#label-object').value;if (!id) return;
 const code = `drk:v1:${kind}:${id}`,image = document.querySelector('#label-image');document.querySelector('#label-print').disabled = true;document.querySelector('#label-copy').disabled = true;
 document.querySelector('#label-preview').hidden = false;document.querySelector('#label-caption').textContent = document.querySelector('#label-object').selectedOptions[0].textContent;document.querySelector('#label-code').textContent = code;
 image.onload = () => {document.querySelector('#label-print').disabled = false;document.querySelector('#label-copy').disabled = false;document.querySelector('#label-status').textContent = 'QR-Code enthält nur Objektart, Schema-Version und opake ID. Er erteilt keine Rechte.';};
 image.onerror = () => {document.querySelector('#label-status').textContent = 'Etikett nicht verfügbar. Anmeldung, Objektberechtigung und Verbindung prüfen.';};image.src = `/api/v1/labels/${kind}/${encodeURIComponent(id)}`;
});
document.querySelector('#label-print').addEventListener('click',() => window.print());
document.querySelector('#label-copy').addEventListener('click',async () => {try {await navigator.clipboard.writeText(document.querySelector('#label-code').textContent);document.querySelector('#label-status').textContent = 'Interne Kennung kopiert.';} catch {document.querySelector('#label-status').textContent = 'Kopieren nicht verfügbar. Angezeigte Kennung manuell kopieren.';}});
document.querySelector('#camera-stop').addEventListener('click',() => {stopCamera();document.querySelector('#camera-status').textContent = 'Kamera beendet.';});
document.querySelector('#camera-start').addEventListener('click',async () => {
 if (!user || identifierPending || scannerBusy) return;stopCamera();const generation = cameraGeneration;
 try {
  const supported = await BarcodeDetector.getSupportedFormats();const formats = ['qr_code','ean_13','ean_8','upc_a','code_128','data_matrix','code_39'].filter(format => supported.includes(format));if (!formats.length) throw new Error('formats');
  const stream = await navigator.mediaDevices.getUserMedia({video:{facingMode:{ideal:'environment'}},audio:false});if (!user || generation !== cameraGeneration) {stream.getTracks().forEach(track => track.stop());return;}cameraStream = stream;
  const video = document.querySelector('#camera-video');video.srcObject = stream;video.hidden = false;await video.play();document.querySelector('#camera-stop').hidden = false;document.querySelector('#camera-start').disabled = true;document.querySelector('#camera-status').textContent = 'Kamera aktiv. Code vollständig in den Bildausschnitt halten.';
  const detector = new BarcodeDetector({formats});
  async function detect() {if (generation !== cameraGeneration) return;try {const codes = await detector.detect(video);if (generation !== cameraGeneration) return;if (codes.length === 1) {const found = codes[0];stopCamera();document.querySelector('#scan-text').value = found.rawValue;document.querySelector('#scan-format').value = ['ean_13','ean_8','upc_a'].includes(found.format) ? 'gtin' : 'auto';document.querySelector('#camera-status').textContent = 'Code erkannt. Scanergebnis prüfen.';await resolveScan();return;}if (codes.length > 1) document.querySelector('#camera-status').textContent = 'Mehrere Codes erkannt. Genau einen Code in den Bildausschnitt halten.';cameraTimer = setTimeout(detect,200);} catch {stopCamera();document.querySelector('#camera-status').textContent = 'Kamera-Erkennung fehlgeschlagen. Handscanner oder Texteingabe verwenden.';}}
  await detect();
 } catch {stopCamera();document.querySelector('#camera-status').textContent = 'Kamera nicht verfügbar oder Zugriff nicht erlaubt. Handscanner oder Texteingabe verwenden.';}
});
document.addEventListener('visibilitychange',() => {if (document.hidden) stopCamera();});
restore();


let intakeDraft = null, intakePending = null, intakeLoading = false;
let stockIntakeDraft = null, stockIntakePending = null, stockIntakeLoading = false;
let deviceRecords = [], deviceDraft = null, devicePending = null, documentPending = null;
const deadlineNames = {UNKNOWN:'ungeklärt',NOT_APPLICABLE:'nachgewiesene Ausnahme',CURRENT:'aktuell',DUE_SOON:'bald fällig',OVERDUE:'überfällig'};
async function loadDevices() {
 const owner = user?.id; if (!owner) return;intakeControls();
 const message = document.querySelector('#devices-status');
 const entries = document.querySelector('#devices-list'); entries.replaceChildren();
 try {
 const response = await fetch('/api/v1/devices');
 if (response.status === 401) {signedOut('Bitte erneut anmelden.');return;}
 if (!response.ok) throw new Error('Geräteübersicht konnte nicht geladen werden.');
 const devices = await response.json(); if (user?.id !== owner) return; deviceRecords = devices;
 for (const device of devices) {
 const item = document.createElement('li');
 const controls = ['STK','MTK','electrical'].map(kind => {const d=device.controls?.[kind];return `${kind}: ${deadlineNames[d?.state] || 'ungeklärt'}${d?.due_on ? ' bis '+d.due_on : ''}`;}).join(' · ');
 item.textContent = `${device.inventory_number} · ${device.name} · ${device.manufacturer || "Hersteller ungeklärt"} / ${device.model || "Modell ungeklärt"} · Serien-/Losnummer ${device.serial_or_lot || "ungeklärt"}${device.intake ? " · "+intakeKindNames[device.intake.asset_kind]+" · Erstbeobachtung: "+intakeConditionNames[device.intake.observed_condition]+(device.intake.external_code?" · Fremdcode: "+device.intake.external_code:"") : ""} · ${device.commissioned_on ? 'Inbetriebnahme '+device.commissioned_on : 'Inbetriebnahme nicht dokumentiert'} · ${controls} · Einweisungsnachweise: ${(device.instructions || []).length} · Aktive Sperren: ${(device.active_blocks || []).map(block=>block.reason).join(", ") || "keine erfasste"} · Vollständige Einsatzfreigabe ungeklärt`;
 for(const instruction of device.instructions || []){const proof=document.createElement('p');proof.textContent=`Einweisung ${instruction.performed_on}: ${instruction.instructor} → ${instruction.trainee} · ${instruction.scope==='model'?'Bauart':'Gerät'} · Unterlage ${instruction.document_version} · Anhang in Quarantäne`;item.append(proof);}
 entries.append(item);
 }
 if (!devicePending && !documentPending) options(document.querySelector('#device-choice'),devices.filter(device => locations.some(location => location.id === device.location_id && can(location,['device_manager','helper']))).map(device => ({id:device.id,label:device.inventory_number+' · '+device.name})));
 if(!devicePending && !documentPending) deviceAllowedActions();
 document.querySelector('#device-control-form').hidden = !devicePending && !document.querySelector('#device-choice').options.length;
 if(documentPending)document.querySelector('#document-upload-form').hidden=false;
 message.textContent = devices.length ? `${devices.length} Geräte angezeigt.${response.headers.get('X-Has-More') === 'true' ? ' Weitere Geräte vorhanden; Übersicht begrenzt.' : ''}` : 'Keine Geräte im freigegebenen Bereich registriert.';
 } catch (error) { if (user?.id === owner) message.textContent = error.message; }
}
document.querySelector('#devices-refresh').addEventListener('click',loadDevices);
for (const kind of ['STK','MTK','electrical']) {
 const field = document.createElement('fieldset'); const legend = document.createElement('legend');legend.textContent=kind;field.append(legend);
 for (const [name,title,type,choices] of [
 ['applicability','Anwendbarkeit','select',[['unknown','Ungeklärt'],['required','Erforderlich'],['exempt','Nachgewiesene Ausnahme']]],
 ['mode','Berechnung','select',[['','Keine Frist'],['month_end','Monatsende'],['year_end','Jahresende'],['exact_date','Exakter Termin']]],
 ['interval_months','Intervall in Monaten','number'],['exact_date','Exakter Termin','date'],['earlier_date','Kürzere / Anlassfrist','date'],['earlier_evidence','Nachweis zur kürzeren Frist','text'],['evidence','Nachweis zur Kontrollpflicht','text']
 ]) {
 const label=document.createElement('label');label.textContent=title;const control=document.createElement(type==='select'?'select':'input');control.id=`device-${kind}-${name}`;
 if (type==='select') for(const [value,text] of choices){const option=document.createElement('option');option.value=value;option.textContent=text;control.append(option);}else{control.type=type;if(type==='number'){control.min='1';control.max='120';control.step='1';}if(type==='text')control.maxLength=500;}
 label.append(control);field.append(label);
 }
 document.querySelector('#device-rule-fields').append(field);
}
function lockDeviceForm() {
 const form=document.querySelector('#device-control-form');
 for(const control of form.elements)control.disabled=Boolean(devicePending || documentPending);
 const submit=form.querySelector('button[type=submit]');submit.disabled=documentPending ? true : devicePending ? devicePending.owner!==user?.id : !deviceDraft;
 lockDocumentForm();
 submit.textContent=devicePending?'Nachweis identisch erneut senden':'Nachweis speichern';
}
function deviceAllowedActions(){
 const record=deviceRecords.find(record=>record.id===document.querySelector('#device-choice').value);
 const manager=record && locations.some(location=>location.id===record.location_id && can(location,['device_manager']));
 options(document.querySelector('#device-action'),manager?[{id:'commission',label:'Inbetriebnahme dokumentieren'},{id:'rules',label:'Kontrollpflichten versionieren'},{id:'inspection',label:'Prüfung dokumentieren'},{id:'defect',label:'Sperre melden'},{id:'release',label:'Eine Sperre ausdrücklich freigeben'},{id:'instruction',label:'Einweisung dokumentieren'}]:[{id:'defect',label:'Defekt melden'}]);
 options(document.querySelector('#device-block-reason'),manager?[{id:'defect',label:'Defekt'},{id:'recall',label:'Rückruf'},{id:'quarantine',label:'Quarantäne'}]:[{id:'defect',label:'Defekt'}]);
 document.querySelector('#document-upload-form').hidden=!manager && !documentPending;
 deviceActionFields();
}
function deviceActionFields(){
 const action=document.querySelector('#device-action').value;
 document.querySelector('#device-rule-fields').hidden=action!=='rules';
 document.querySelector('#device-date-field').hidden=!['commission','inspection','instruction'].includes(action);
 document.querySelector('#device-date').required=['commission','inspection','instruction'].includes(action);
 document.querySelector('#device-inspection-fields').hidden=action!=='inspection';
 document.querySelector('#device-instruction-fields').hidden=action!=='instruction';
 for(const id of ['device-instructor','device-trainee','device-document-version','device-instruction-document'])document.querySelector('#'+id).required=action==='instruction';
 for(const id of ['device-inspection-result','device-examiner','device-method'])document.querySelector('#'+id).required=action==='inspection';
 document.querySelector('#device-block-reason-field').hidden=action!=='defect';document.querySelector('#device-release-field').hidden=action!=='release';document.querySelector('#device-release-block').required=action==='release';
 document.querySelector('#device-qualification').required=action!=='defect';document.querySelector('#device-assignment').required=action!=='defect';
}
document.querySelector('#device-action').addEventListener('change',deviceActionFields);
document.querySelector('#device-choice').addEventListener('change',()=>{deviceDraft=null;deviceAllowedActions();lockDeviceForm();});
document.querySelector('#device-begin').addEventListener('click',async()=>{
 if(!user || devicePending || documentPending)return;const owner=user.id,id=document.querySelector('#device-choice').value;deviceDraft=null;lockDeviceForm();
 try{const response=await fetch(`/api/v1/devices/${encodeURIComponent(id)}/book`);if(!response.ok)throw new Error('Geräteakte nicht verfügbar.');const record=await response.json();if(user?.id!==owner || document.querySelector('#device-choice').value!==id)return;deviceDraft={id,version:record.version};options(document.querySelector('#device-release-block'),(record.active_blocks || []).map(block=>({id:block.id,label:block.reason+' · '+block.note})));document.querySelector('#device-basis').textContent=`Geladene Geräteversion ${record.version}. Änderungen erfordern einen neuen Abgleich.`;lockDeviceForm();await loadDeviceDocuments(id,owner);}catch(error){if(user?.id===owner)document.querySelector('#device-basis').textContent=error.message;}
});
document.querySelector('#device-control-form').addEventListener('submit',async event=>{
 event.preventDefault();if(documentPending || !user || devicePending && devicePending.owner!==user.id || !devicePending && !deviceDraft)return;
 if(!devicePending){const action=document.querySelector('#device-action').value;const body={expected_version:deviceDraft.version,evidence:document.querySelector('#device-evidence').value,qualification:document.querySelector('#device-qualification').value,assignment:document.querySelector('#device-assignment').value,note:document.querySelector('#device-note').value};
 if(action==='commission')body.performed_on=document.querySelector('#device-date').value;
 else if(action==='rules') body.rules=['STK','MTK','electrical'].map(kind=>({control_type:kind,applicability:document.querySelector(`#device-${kind}-applicability`).value,mode:document.querySelector(`#device-${kind}-mode`).value,interval_months:Number(document.querySelector(`#device-${kind}-interval_months`).value),exact_date:document.querySelector(`#device-${kind}-exact_date`).value,earlier_date:document.querySelector(`#device-${kind}-earlier_date`).value,earlier_evidence:document.querySelector(`#device-${kind}-earlier_evidence`).value,evidence:document.querySelector(`#device-${kind}-evidence`).value}));
 if(action==='inspection')Object.assign(body,{performed_on:document.querySelector('#device-date').value,control_type:document.querySelector('#device-inspection-kind').value,result:document.querySelector('#device-inspection-result').value,examiner:document.querySelector('#device-examiner').value,method:document.querySelector('#device-method').value,next_due_on:document.querySelector('#device-next-due').value});
 if(action==='defect')body.block_reason=document.querySelector('#device-block-reason').value;
 if(action==='instruction')Object.assign(body,{performed_on:document.querySelector('#device-date').value,instructor:document.querySelector('#device-instructor').value,trainee:document.querySelector('#device-trainee').value,instruction_scope:document.querySelector('#device-instruction-scope').value,transfer_evidence:document.querySelector('#device-transfer-evidence').value,document_version:document.querySelector('#device-document-version').value,document_id:document.querySelector('#device-instruction-document').value});
 if(action==='release')body.block_id=document.querySelector('#device-release-block').value;
 const route={inspection:'inspections',defect:'defects',release:'releases',instruction:'instructions'}[action] || action;
 devicePending={owner:user.id,path:`/api/v1/devices/${encodeURIComponent(deviceDraft.id)}/${route}`,key:crypto.randomUUID(),body:JSON.stringify(body)};
 }
 lockDeviceForm();const saved=devicePending;
 try{const response=await fetch(saved.path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':saved.key},body:saved.body});const result=await response.json();if(user?.id!==saved.owner)return;
 if(response.status===401){signedOut('Bitte erneut anmelden; Gerätenachweis identisch wiederholen.');return;}
 if(response.status>=500){document.querySelector('#device-basis').textContent='Nachweis nicht bestätigt. Mit demselben Schlüssel erneut senden.';return;}
 devicePending=null;deviceDraft=null;document.querySelector('#device-basis').textContent=response.ok?'Nachweis gespeichert. Für weitere Änderungen Geräteakte neu laden.':result.message;lockDeviceForm();await loadDevices();
 }catch{if(user?.id===saved.owner)document.querySelector('#device-basis').textContent='Nachweis nicht bestätigt. Mit demselben Schlüssel erneut senden.';}
});
deviceActionFields();

function lockDocumentForm(){
 const form=document.querySelector('#document-upload-form');for(const control of form.elements)control.disabled=Boolean(documentPending || devicePending);
 const button=form.querySelector('button[type=submit]');button.disabled=documentPending ? documentPending.owner!==user?.id : !deviceDraft || Boolean(devicePending);
 button.textContent=documentPending?'Anhang identisch erneut senden':'Anhang speichern';
}
async function loadDeviceDocuments(id,owner){
 const response=await fetch(`/api/v1/devices/${encodeURIComponent(id)}/documents`);if(!response.ok)throw new Error('Anhänge nicht verfügbar.');const documents=await response.json();if(user?.id!==owner || document.querySelector('#device-choice').value!==id)return;
 const list=document.querySelector('#document-list');list.replaceChildren();
 const manager=locations.some(location=>deviceRecords.some(record=>record.id===id && record.location_id===location.id) && can(location,['device_manager']));
 for(const document of documents){const item=window.document.createElement('li');item.textContent=`${document.filename} · ${document.size} Bytes · Quarantäne · SHA-256 ${document.sha256}`;
 if(manager){const link=window.document.createElement('a');link.textContent=' Ungeprüften Anhang zur Prüfung herunterladen';link.href=`/api/v1/devices/${encodeURIComponent(id)}/documents/${encodeURIComponent(document.id)}/download?review=true`;item.append(link);}list.append(item);}
 if(!devicePending)options(document.querySelector('#device-instruction-document'),[{id:'',label:'Anhang ausdrücklich auswählen'},...documents.map(document=>({id:document.id,label:document.filename+' · Quarantäne'}))]);
}
document.querySelector('#document-upload-form').addEventListener('submit',async event=>{
 event.preventDefault();if(!user || devicePending || documentPending && documentPending.owner!==user.id || !documentPending && !deviceDraft)return;
 if(!documentPending){const file=document.querySelector('#document-file').files[0];if(!file || file.size<1 || file.size>4194304){document.querySelector('#document-status').textContent='Datei muss zwischen 1 Byte und 4 MiB groß sein.';return;}
 const owner=user.id,draft={...deviceDraft};let bytes;try{bytes=new Uint8Array(await file.arrayBuffer());}catch{document.querySelector('#document-status').textContent='Datei konnte nicht gelesen werden.';return;}
 if(user?.id!==owner || deviceDraft?.id!==draft.id || deviceDraft?.version!==draft.version || documentPending || devicePending)return;
 let raw='';for(let index=0;index<bytes.length;index+=32768)raw+=String.fromCharCode(...bytes.subarray(index,index+32768));
 const extension=file.name.split('.').pop().toLowerCase();const type={pdf:'application/pdf',png:'image/png',jpg:'image/jpeg',jpeg:'image/jpeg',txt:'text/plain'}[extension];
 documentPending={owner,id:draft.id,key:crypto.randomUUID(),body:JSON.stringify({expected_version:draft.version,filename:file.name,media_type:type || 'unsupported',content:btoa(raw),note:document.querySelector('#document-note').value})};
 }
 lockDeviceForm();const saved=documentPending;
 try{const response=await fetch(`/api/v1/devices/${encodeURIComponent(saved.id)}/documents`,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':saved.key},body:saved.body});const result=await response.json();if(user?.id!==saved.owner)return;
 if(response.status===401){signedOut('Bitte erneut anmelden; Anhang identisch wiederholen.');return;}
 if(response.status>=500){document.querySelector('#document-status').textContent='Upload nicht bestätigt. Identisch erneut senden.';return;}
 documentPending=null;document.querySelector('#document-status').textContent=response.ok?'Anhang gespeichert und in Quarantäne.':result.message;if(!response.ok)deviceDraft=null;else document.querySelector('#document-file').value='';lockDeviceForm();if(response.ok)await loadDeviceDocuments(saved.id,saved.owner);
 }catch{if(user?.id===saved.owner)document.querySelector('#document-status').textContent='Upload nicht bestätigt. Identisch erneut senden.';}
});

function addPackDevice(requirement={}){
 if(!packDraft)return;
 const row=document.createElement('fieldset');const choice=document.createElement('select');choice.className='pack-device-id';choice.required=true;
 options(choice,[{id:'',label:'Gerät ausdrücklich wählen'},...deviceRecords.filter(device=>device.location_id===packDraft.location).map(device=>({id:device.id,label:device.inventory_number+' · '+device.name}))]);if(requirement.device_id)choice.value=requirement.device_id;
 const label=document.createElement('label');label.textContent='Verpflichtendes Gerät am Sollort';label.append(choice);row.append(label);
 function addAccessory(item={}){const accessory=document.createElement('div');accessory.className='pack-accessory';const product=document.createElement('select');product.className='accessory-product';product.required=true;options(product,productChoices());if(item.product_id)product.value=item.product_id;
 const quantity=document.createElement('input');quantity.type='number';quantity.className='accessory-quantity';quantity.min='1';quantity.max='1000000000';quantity.step='1';quantity.required=true;quantity.value=item.quantity || 1;
 for(const [text,control]of [['Zubehörartikel',product],['Zubehörmenge in Basiseinheiten',quantity]]){const label=document.createElement('label');label.textContent=text;label.append(control);accessory.append(label);}
 const remove=document.createElement('button');remove.type='button';remove.textContent='Zubehör entfernen';remove.addEventListener('click',()=>accessory.remove());accessory.append(remove);row.insertBefore(accessory,add);
 }
 const add=document.createElement('button');add.type='button';add.textContent='Zubehör hinzufügen';add.addEventListener('click',()=>{if(row.querySelectorAll('.pack-accessory').length<25)addAccessory();});row.append(add);
 const remove=document.createElement('button');remove.type='button';remove.textContent='Geräteanforderung entfernen';remove.addEventListener('click',()=>row.remove());row.append(remove);
 document.querySelector('#pack-devices').append(row);for(const accessory of requirement.accessories || [])addAccessory(accessory);
}
document.querySelector('#pack-add-device').addEventListener('click',()=>{if(packDraft && !packPending && document.querySelectorAll('#pack-devices > fieldset').length<25)addPackDevice();});

function deviceCheckControls(target) {
 const form=document.querySelector('#physical-device-check');
 form.hidden=packPending?.kind!=='device'&&(!target||!can(target,['helper','material_manager'])||!material?.current_version||!packAssessment?.devices?.length);
 for(const control of form.elements)control.disabled=Boolean(packPending)||!deviceCheckDraft;
 document.querySelector('#physical-device-start').disabled=Boolean(packPending)||!material?.current_version||!packAssessment?.devices?.length;
 const submit=form.querySelector('button[type=submit]');submit.disabled=packPending?packPending.kind!=='device'||packPending.owner!==user?.id:!deviceCheckDraft;
 submit.textContent=packPending?.kind==='device'?'Geräteprüfnachweis erneut senden':'Geräteprüfnachweis speichern';
 document.querySelector('#physical-device-basis').textContent=deviceCheckDraft?'Geräteprüfung begonnen. Änderungen erfordern eine neue Prüfung.':'Vor der Prüfung ausdrücklich beginnen.';
 const proof=packAssessment?.last_device_check;document.querySelector('#last-device-proof').textContent=proof?`Letzte Geräteprüfung: ${proof.username} · ${new Date(proof.created_at).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})} · ${proof.current?'aktuell':'veraltet'} · ${proof.physical_passed?'Keine Abweichung':'Abweichung'} · ${proof.note}`:'';
}
async function loadDeviceProofHistory(id){
 const list=document.querySelector('#device-proof-history');list.replaceChildren();
 const response=await fetch(`/api/v1/packlists/${id}/device-checks`,{cache:'no-store'});if(!response.ok)throw new Error('device-proof-history');
 const proofs=await response.json();if(document.querySelector('#pack-choice').value!==id)return;
 for(const proof of proofs){const li=document.createElement('li');li.textContent=`${new Date(proof.created_at).toLocaleString('de-DE',{timeZone:'Europe/Berlin'})} · ${proof.username} · ${proof.current?'aktuell':'veraltet'} · ${proof.physical_passed?'Keine Abweichung':'Abweichung'} · ${proof.note}`;list.append(li);for(const result of proof.results||[]){const detail=document.createElement('li');detail.textContent=`${result.device_id}: Anwesenheit ${result.present?'bestätigt':'Abweichung'}, Funktion ${result.functional?'bestätigt':'Abweichung'}, Zubehör ${result.accessories_complete?'vollständig':'unvollständig'}`;list.append(detail);}}
 if(response.headers.get('X-Has-More')==='true'){const li=document.createElement('li');li.textContent='Anzeige auf die letzten 50 Geräteprüfungen begrenzt.';list.append(li);}
}
document.querySelector('#physical-device-start').addEventListener('click',async()=>{
 if(!user||packPending)return;deviceCheckDraft=null;if(!await loadMaterial()||!user||packPending||!material.current_version||!packAssessment.devices.length)return;
 deviceCheckDraft={owner:user.id,id:material.packlist.id,basis:packAssessment.device_basis_hash};document.querySelector('#physical-device-note').value='';
 const list=document.querySelector('#physical-device-results');list.replaceChildren();
 for(const item of packAssessment.devices){const row=document.createElement('fieldset');row.dataset.deviceId=item.requirement.device_id;const legend=document.createElement('legend');legend.textContent=`${item.device.inventory_number} · ${item.device.name}`;row.append(legend);
 const accessories=document.createElement('p');accessories.textContent='Zubehör-Soll: '+(item.requirement.accessories||[]).map(a=>`${productChoices().find(p=>p.id===a.product_id)?.label||a.product_id}: ${a.quantity}`).join(', ');row.append(accessories);
 for(const [labelText,cls] of [['Am Sollort vorhanden','device-present'],['Funktion geprüft','device-functional'],['Zugeordnetes Zubehör vollständig','device-accessories']]){const label=document.createElement('label');label.textContent=labelText;const select=document.createElement('select');select.className=cls;select.required=true;for(const [value,text]of [['','Ausdrücklich auswählen'],['passed','Ja, geprüft'],['failed','Nein / Abweichung']]){const option=document.createElement('option');option.value=value;option.textContent=text;select.append(option);}label.append(select);row.append(label);}list.append(row);
 }packControls();
});
document.querySelector('#physical-device-check').addEventListener('submit',event=>{event.preventDefault();sendPackMutation('device');});

const intakeKindNames={device:'Gerät',equipment:'Equipment',unknown:'Art ungeklärt'};
const intakeConditionNames={unknown:'ungeklärt',unremarkable:'unauffällig (keine Freigabe)',defective:'defekt',quarantined:'Quarantäne'};
function intakeControls(){
 if(!user)return;
 const panel=document.querySelector('#device-import');panel.hidden=!intakePending&&!locations.some(location=>can(location,['device_manager']));
 for(const control of document.querySelector('#device-import-form').elements)control.disabled=Boolean(intakePending)||intakeLoading;
 if(intakePending?.owner===user.id&&!document.querySelector('#device-import-status').textContent)document.querySelector('#device-import-status').textContent='Ein CSV-Import ist noch nicht bestätigt. Mit denselben Daten erneut senden.';
 const commit=document.querySelector('#device-import-commit');commit.disabled=intakeLoading||(intakePending?intakePending.owner!==user.id:!intakeDraft?.preview||intakeDraft.preview.error_count>0);
 commit.textContent=intakePending?'Import mit denselben Daten erneut senden':'Geprüften Import bestätigen';
}
function renderIntakePreview(preview){
 const rows=document.querySelector('#device-import-rows');rows.replaceChildren();
 for(const row of preview.rows){const line=document.createElement('tr');for(const value of [row.row,row.device.inventory_number,`${intakeKindNames[row.intake.asset_kind]||'ungeklärt'} / ${intakeConditionNames[row.intake.observed_condition]||'ungeklärt'}`,row.device.name,row.intake.source_location,row.message]){const cell=document.createElement('td');cell.textContent=value;line.append(cell);}rows.append(line);}
 const mappings=document.querySelector('#device-import-locations');mappings.replaceChildren();
 for(const value of preview.location_values){const label=document.createElement('label');label.textContent=`CSV-Standort „${value}“ zuordnen`;const select=document.createElement('select');select.dataset.source=value;options(select,[{id:'',label:'Name automatisch zuordnen; sonst ausdrücklich wählen'},...preview.location_choices.map(location=>({id:location.id,label:`${location.site} / ${location.name}`}))]);select.value=intakeDraft.locations[value]||'';select.addEventListener('change',()=>{if(select.value)intakeDraft.locations[value]=select.value;else delete intakeDraft.locations[value];intakeDraft.preview=null;document.querySelector('#device-import-status').textContent='Standortzuordnung geändert. Vorschau erneut prüfen.';intakeControls();});label.append(select);mappings.append(label);}
 document.querySelector('#device-import-status').textContent=`${preview.new_count} neu · ${preview.unchanged_count} unverändert · ${preview.error_count} Fehler · ${preview.unused_count} leere vorbereitete Zeilen ignoriert. ${preview.error_count?'Fehler korrigieren; erst danach bestätigen.':'Vorschau prüfen und Import ausdrücklich bestätigen.'}`;
}
document.querySelector('#device-import-file').addEventListener('change',()=>{if(intakePending)return;intakeDraft=null;document.querySelector('#device-import-rows').replaceChildren();document.querySelector('#device-import-locations').replaceChildren();document.querySelector('#device-import-status').textContent='Neue Datei ausgewählt. CSV zuerst prüfen.';intakeControls();});
document.querySelector('#device-import-form').addEventListener('submit',async event=>{
 event.preventDefault();const owner=user?.id;if(!owner||intakePending||intakeLoading)return;const message=document.querySelector('#device-import-status');intakeLoading=true;intakeControls();
 try{
  if(!intakeDraft){const file=document.querySelector('#device-import-file').files[0];if(!file||file.size>1048576)throw new Error('CSV auswählen, höchstens 1 MiB.');const bytes=await file.arrayBuffer();const csv=new TextDecoder('utf-8',{fatal:true}).decode(bytes);if(user?.id!==owner)return;intakeDraft={owner,csv,locations:{},preview:null};}
  const response=await fetch('/api/v1/device-import/preview',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':crypto.randomUUID()},body:JSON.stringify({csv:intakeDraft.csv,locations:intakeDraft.locations})});const result=await response.json();if(user?.id!==owner)return;
  if(response.status===401){signedOut('Bitte erneut anmelden.');return;}if(!response.ok)throw new Error(result.message||'CSV-Vorschau nicht verfügbar.');intakeDraft.preview=result;renderIntakePreview(result);
 }catch(error){if(user?.id===owner){if(intakeDraft)intakeDraft.preview=null;message.textContent=error instanceof TypeError?'Datei muss CSV UTF-8 sein.':error.message;}}
 finally{intakeLoading=false;intakeControls();}
});
document.querySelector('#device-import-commit').addEventListener('click',async()=>{
 const owner=user?.id;if(!owner||intakeLoading||intakePending&&intakePending.owner!==owner)return;
 if(!intakePending){if(intakeDraft?.owner!==owner||!intakeDraft.preview||intakeDraft.preview.error_count)return;intakePending={owner,key:crypto.randomUUID(),data:{csv:intakeDraft.csv,locations:{...intakeDraft.locations},basis_hash:intakeDraft.preview.basis_hash},uncertain:false};}
 intakeLoading=true;intakeControls();const message=document.querySelector('#device-import-status');
 try{
  const response=await fetch('/api/v1/device-import/commit',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':intakePending.key},body:JSON.stringify(intakePending.data)});const result=await response.json();
  if(response.status===401){intakePending.uncertain=true;signedOut('Mit demselben Konto anmelden und offenen CSV-Import erneut senden.');return;}
  if(response.status>=500||!response.ok&&intakePending.uncertain)throw new Error('uncertain');intakePending=null;if(user?.id!==owner)return;
  if(!response.ok){if(intakeDraft)intakeDraft.preview=null;message.textContent=result.message||'Import abgelehnt; Vorschau erneut prüfen.';return;}
  intakeDraft=null;document.querySelector('#device-import-file').value='';document.querySelector('#device-import-rows').replaceChildren();document.querySelector('#device-import-locations').replaceChildren();message.textContent=`Import bestätigt: ${result.created_count} neu, ${result.unchanged_count} unverändert. Keine Prüf- oder Einsatzfreigabe erteilt.`;await loadDevices();
 }catch{if(intakePending)intakePending.uncertain=true;if(user?.id===owner)message.textContent='Import nicht bestätigt. Erneut senden verwendet dieselben Daten und denselben Schlüssel. Seite bis zur Klärung nicht neu laden.';}
 finally{intakeLoading=false;intakeControls();}
});

function stockIntakeControls(){
 if(!user)return;
 const panel=document.querySelector('#stock-import');panel.hidden=!stockIntakePending&&!locations.some(location=>can(location,['material_manager']));
 for(const control of document.querySelector('#stock-import-form').elements)control.disabled=Boolean(stockIntakePending)||stockIntakeLoading;
 if(stockIntakePending?.owner===user.id&&!document.querySelector('#stock-import-status').textContent)document.querySelector('#stock-import-status').textContent='Ein CSV-Import ist noch nicht bestätigt. Mit denselben Daten erneut senden.';
 const commit=document.querySelector('#stock-import-commit');commit.disabled=stockIntakeLoading||(stockIntakePending?stockIntakePending.owner!==user.id:!stockIntakeDraft?.preview||stockIntakeDraft.preview.error_count>0);
 commit.textContent=stockIntakePending?'Import mit denselben Daten erneut senden':'Geprüften Import bestätigen';
}
function renderStockIntakePreview(preview){
 const rows=document.querySelector('#stock-import-rows');rows.replaceChildren();
 for(const row of preview.rows){const line=document.createElement('tr');for(const value of [row.row,row.data.source_id,row.data.name,`${row.data.quantity} ${row.data.base_unit}`,row.data.source_location,`${row.data.lot||'Charge ungeklärt'} / ${row.data.expires_on||'Verfall ungeklärt'}`,row.message]){const cell=document.createElement('td');cell.textContent=value;line.append(cell);}rows.append(line);}
 const mappings=document.querySelector('#stock-import-locations');mappings.replaceChildren();
 for(const value of preview.location_values){const label=document.createElement('label');label.textContent=`CSV-Standort „${value}“ zuordnen`;const select=document.createElement('select');select.dataset.source=value;options(select,[{id:'',label:'Name automatisch zuordnen; sonst ausdrücklich wählen'},...preview.location_choices.map(location=>({id:location.id,label:`${location.site} / ${location.name}`}))]);select.value=stockIntakeDraft.locations[value]||'';select.addEventListener('change',()=>{if(select.value)stockIntakeDraft.locations[value]=select.value;else delete stockIntakeDraft.locations[value];stockIntakeDraft.preview=null;document.querySelector('#stock-import-status').textContent='Standortzuordnung geändert. Vorschau erneut prüfen.';stockIntakeControls();});label.append(select);mappings.append(label);}
 document.querySelector('#stock-import-status').textContent=`${preview.new_count} neu · ${preview.unchanged_count} unverändert · ${preview.error_count} Fehler · ${preview.unused_count} leere vorbereitete Zeilen ignoriert. ${preview.error_count?'Fehler korrigieren; erst danach bestätigen.':'Vorschau prüfen und Import ausdrücklich bestätigen.'}`;
}
document.querySelector('#stock-import-file').addEventListener('change',()=>{if(stockIntakePending)return;stockIntakeDraft=null;document.querySelector('#stock-import-rows').replaceChildren();document.querySelector('#stock-import-locations').replaceChildren();document.querySelector('#stock-import-status').textContent='Neue Datei ausgewählt. CSV zuerst prüfen.';stockIntakeControls();});
document.querySelector('#stock-import-form').addEventListener('submit',async event=>{
 event.preventDefault();const owner=user?.id;if(!owner||stockIntakePending||stockIntakeLoading)return;const message=document.querySelector('#stock-import-status');stockIntakeLoading=true;stockIntakeControls();
 try{
  if(!stockIntakeDraft){const file=document.querySelector('#stock-import-file').files[0];if(!file||file.size>1048576)throw new Error('CSV auswählen, höchstens 1 MiB.');const bytes=await file.arrayBuffer();const csv=new TextDecoder('utf-8',{fatal:true}).decode(bytes);if(user?.id!==owner)return;stockIntakeDraft={owner,csv,locations:{},mode:document.querySelector("#stock-import-mode").value,preview:null};}
  const response=await fetch('/api/v1/stock-import/preview',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':crypto.randomUUID()},body:JSON.stringify({csv:stockIntakeDraft.csv,locations:stockIntakeDraft.locations,mode:stockIntakeDraft.mode})});const result=await response.json();if(user?.id!==owner)return;
  if(response.status===401){signedOut('Bitte erneut anmelden.');return;}if(!response.ok)throw new Error(result.message||'CSV-Vorschau nicht verfügbar.');stockIntakeDraft.preview=result;renderStockIntakePreview(result);
 }catch(error){if(user?.id===owner){if(stockIntakeDraft)stockIntakeDraft.preview=null;message.textContent=error instanceof TypeError?'Datei muss CSV UTF-8 sein.':error.message;}}
 finally{stockIntakeLoading=false;stockIntakeControls();}
});
document.querySelector('#stock-import-commit').addEventListener('click',async()=>{
 const owner=user?.id;if(!owner||stockIntakeLoading||stockIntakePending&&stockIntakePending.owner!==owner)return;
 if(!stockIntakePending){if(stockIntakeDraft?.owner!==owner||!stockIntakeDraft.preview||stockIntakeDraft.preview.error_count)return;stockIntakePending={owner,key:crypto.randomUUID(),data:{csv:stockIntakeDraft.csv,locations:{...stockIntakeDraft.locations},mode:stockIntakeDraft.mode,basis_hash:stockIntakeDraft.preview.basis_hash},uncertain:false};}
 stockIntakeLoading=true;stockIntakeControls();const message=document.querySelector('#stock-import-status');
 try{
  const response=await fetch('/api/v1/stock-import/commit',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'Idempotency-Key':stockIntakePending.key},body:JSON.stringify(stockIntakePending.data)});const result=await response.json();
  if(response.status===401){stockIntakePending.uncertain=true;signedOut('Mit demselben Konto anmelden und offenen CSV-Import erneut senden.');return;}
  if(response.status>=500||!response.ok&&stockIntakePending.uncertain)throw new Error('uncertain');stockIntakePending=null;if(user?.id!==owner)return;
  if(!response.ok){if(stockIntakeDraft)stockIntakeDraft.preview=null;message.textContent=result.message||'Import abgelehnt; Vorschau erneut prüfen.';return;}
  stockIntakeDraft=null;document.querySelector('#stock-import-file').value='';document.querySelector('#stock-import-rows').replaceChildren();document.querySelector('#stock-import-locations').replaceChildren();message.textContent=`Import bestätigt: ${result.created_count} neu, ${result.unchanged_count} unverändert. Mengen in Basiseinheiten gebucht; Quarantäne gemäß Vorschau.`;await loadStock();if(user)await loadScannerData();
 }catch{if(stockIntakePending)stockIntakePending.uncertain=true;if(user?.id===owner)message.textContent='Import nicht bestätigt. Erneut senden verwendet dieselben Daten und denselben Schlüssel. Seite bis zur Klärung nicht neu laden.';}
 finally{stockIntakeLoading=false;stockIntakeControls();}
});

document.querySelector("#stock-import-mode").addEventListener("change",()=>{if(stockIntakePending)return;if(stockIntakeDraft){stockIntakeDraft.mode=document.querySelector("#stock-import-mode").value;stockIntakeDraft.preview=null;}document.querySelector("#stock-import-status").textContent="Importart geändert. Vorschau erneut prüfen.";stockIntakeControls();});
