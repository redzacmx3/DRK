// Browser regression check for inventory basis locking. The API is simulated;
// real SQLite/HTTP behavior is covered by Go tests and the Compose smoke check.
import {spawn} from 'node:child_process';
import {createServer} from 'node:http';
import {mkdtemp,readFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import assert from 'node:assert/strict';

const root = resolve(import.meta.dirname,'../internal/app/web');
const profile = await mkdtemp(join(tmpdir(),'drk-ui-'));
const server = createServer(async (request,response) => {
 try {
  const path = request.url === '/' ? 'index.html' : request.url.slice(1);
  if (!['index.html','app.js','style.css'].includes(path)) {response.writeHead(404);response.end();return;}
  response.setHeader('Content-Type',path.endsWith('.js') ? 'text/javascript' : path.endsWith('.css') ? 'text/css' : 'text/html');
  response.end(await readFile(join(root,path)));
 } catch {response.writeHead(500);response.end();}
});
await new Promise(resolve => server.listen(0,'127.0.0.1',resolve));
const url = `http://127.0.0.1:${server.address().port}`;
const browser = spawn(process.env.CHROMIUM_BIN || 'chromium',['--headless=new','--no-sandbox','--disable-gpu','--disable-background-networking',`--user-data-dir=${profile}`,'--remote-debugging-port=0','about:blank'],{stdio:'ignore'});
let socket;
try {
 let port;
 for (let attempt=0;attempt<100;attempt++) {
  if (browser.exitCode !== null) throw new Error('Chromium did not start');
  try {port = (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0];break;} catch {await new Promise(resolve => setTimeout(resolve,100));}
 }
 assert.ok(port,'Chromium debugging endpoint unavailable');
 const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
 socket = new WebSocket(pages.find(page => page.type === 'page').webSocketDebuggerUrl);
 await new Promise((resolve,reject) => {socket.addEventListener('open',resolve,{once:true});socket.addEventListener('error',reject,{once:true});});
 let sequence=0;const waiting = new Map();
 socket.addEventListener('message',event => {
  const message = JSON.parse(event.data);if (!message.id) return;
  const pending = waiting.get(message.id);waiting.delete(message.id);
  if (message.error) pending.reject(new Error(message.error.message));else pending.resolve(message.result);
 });
 function call(method,params={}) {return new Promise((resolve,reject) => {const id=++sequence;waiting.set(id,{resolve,reject});socket.send(JSON.stringify({id,method,params}));});}
 async function evaluate(expression) {
  const result = await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
 }
 async function until(expression) {
  for (let attempt=0;attempt<100;attempt++) {if (await evaluate(expression)) return;await new Promise(resolve => setTimeout(resolve,50));}
  throw new Error(`UI did not reach expected state: ${expression}`);
 }
 await call('Page.enable');
 await call('Page.addScriptToEvaluateOnNewDocument',{source:`
 const realFetch = window.fetch.bind(window);
 window.testBalances = [{batch_id:'batch',location_id:'warehouse',state:'available',quantity:10,usable_quantity:10,version:1,product:'Demo',unit:'Stück',lot:'DEMO',location:'Lager',expires_on:'2030-01-01',blocked:false}];
 window.posted = []; window.devicePosted=[];window.deviceReplies=new Map();window.loseDeviceReply=false;window.testDocuments=[];window.documentPosted=[];window.documentReplies=new Map();window.loseDocumentReply=false;window.deviceVersion=0;window.deviceDetails={name:"<img src=x onerror=alert(1)>",manufacturer:"Demo",model:"Model",serial_or_lot:"SERIAL"};window.testIncidents=[];window.deviceHistory=[];window.deviceMasterData=null;window.deviceBlocks=[{id:"recall-block",reason:"recall",note:"Synthetic recall"}];
 window.stockIntakePosted=[];window.stockIntakePreviewPosted=[];window.stockIntakeReplies=new Map();window.stockIntakeBasis='1'.repeat(64);window.loseStockIntakeReply=false;
 window.intakePosted=[];window.intakePreviewPosted=[];window.intakeReplies=new Map();window.intakeBasis='1'.repeat(64);window.loseIntakeReply=false;
 window.pack = {id:'pack',location_id:'warehouse',version:1,title:'Material',check_valid_hours:24,created_at:new Date().toISOString(),note:'Synthetic',items:[{product_id:'product',quantity:5,min_remaining_days:30}]};
 window.testIdentifiers = [];window.identifierPosted = [];window.identifierReplies = new Map();window.loseIdentifierReply = false;window.nonInventoryStockPosts = [];
 window.materialBasis = 'a'.repeat(64);window.packPosted = [];window.packReplies = new Map();window.losePackReply = false;

 window.fetch = async (input,options={}) => {
  const path = typeof input === 'string' ? input : input.url;
  let data;
  if (path === '/api/v1/me') data = {id:'synthetic-user',username:'test',csrf_token:'synthetic-csrf',rights:[{site_id:'site',role:'material_manager'},{site_id:'site',role:'device_manager'}]};
  else if (path === '/api/v1/devices/reminder-mail') {
   data=[{id:'delivery',device_id:'device',inventory_number:'TEST',name:'<svg onload=alert(1)>',control_type:'STK',due_on:'2026-10-10',message_id:'<synthetic@drk-inventory.invalid>',state:'uncertain',attempts:1,created_at:'2026-10-11T08:00:00Z',last_attempt_at:'2026-10-11T08:00:00Z',next_attempt_at:'2026-10-11T08:01:00Z'}];
   if(window.holdMail){window.holdMail=false;await new Promise(resolve=>window.releaseMail=resolve);}
   if(window.failMail){window.failMail=false;return new Response(JSON.stringify({message:'Synthetic mail status unavailable'}),{status:503,headers:{'Content-Type':'application/json'}});}
  }
  else if (path === '/api/v1/devices/reminders') {
   data={as_of:'2026-10-11T12:00:00Z',last_worker_success:'2026-10-11T08:00:00Z',thresholds:[60,30,7],reminders:[{id:'reminder',device_id:'device',inventory_number:'TEST',name:'<svg onload=alert(1)>',control_type:'STK',due_on:'2026-10-10',threshold_days:-1,days_remaining:-1,source_version:2,created_at:'2026-10-11T08:00:00Z'}]};
   if(window.holdReminders){window.holdReminders=false;await new Promise(resolve=>window.releaseReminders=resolve);}
   if(window.failReminders){window.failReminders=false;return new Response(JSON.stringify({message:'Synthetic worker unavailable'}),{status:503,headers:{'Content-Type':'application/json'}});}
  }
  else if (path.startsWith('/api/v1/devices/attention?')) {
   data={as_of:'2026-10-11T12:00:00Z',day:'2026-10-11',horizon_days:Number(new URL(path,location.origin).searchParams.get('days')),scanned_devices:1,counts:{block:1,overdue:1},devices:[{id:'device',inventory_number:'TEST',name:'<img src=x onerror=alert(1)>',location_id:'warehouse',version:window.deviceVersion,reasons:[{kind:'block',description:'Synthetic block'},{kind:'overdue',control_type:'STK',due_on:'2026-10-10',days_remaining:-1,description:'Synthetic overdue'}]}]};
   if(window.holdAttention){window.holdAttention=false;await new Promise(resolve=>window.releaseAttention=resolve);}
  }
  else if (path === '/api/v1/locations') data = [{id:'warehouse',name:'Lager',site:'Demo',operator:'Demo',site_id:'site'}];
  else if (path === '/api/v1/stock/catalog') data = [{id:'batch',product_id:'product',product:'Demo',unit:'Stück',lot:'DEMO'}];
  else if (path === '/api/v1/stock/balances') data = window.testBalances;
  else if (path.startsWith('/api/v1/stock/movements?')) data = [];
  else if (path === '/api/v1/product-identifiers' && !options.method) data = window.testIdentifiers;
  else if (path === '/api/v1/product-identifiers' && options.method === 'POST') {
   const request = JSON.parse(options.body),key = options.headers['Idempotency-Key'];window.identifierPosted.push({request,key});
   if (window.identifierReplies.has(key)) return new Response(JSON.stringify(window.identifierReplies.get(key)),{status:201,headers:{'Content-Type':'application/json'}});
   data = {...request,id:'identifier',product:'Demo',unit:'Stück',active:true,created_at:new Date().toISOString()};window.testIdentifiers.push(data);window.identifierReplies.set(key,data);
   if (window.loseIdentifierReply) {window.loseIdentifierReply=false;throw new Error('Simulated lost identifier response');}
  }
  else if (path === '/api/v1/scanner/resolve') {
   const request = JSON.parse(options.body);
   data = {status:'unknown',parsed:{kind:'unknown',warnings:[]},warnings:['Keine Buchung.'],data_as_of:new Date().toISOString()};
   if (request.text === '4006381333931') {
    data.parsed = {kind:'gtin',value:'04006381333931',warnings:[]};data.status = window.testIdentifiers.length ? 'product_match' : 'unregistered_identifier';
    if (window.testIdentifiers.length) Object.assign(data,{product_id:'product',product:'Demo',unit:'Stück',identifier_id:'identifier',pack_quantity:window.testIdentifiers[0].pack_quantity});
   } else if (request.text === 'conflicting-GS1') Object.assign(data,{status:'batch_mismatch',parsed:{kind:'gs1',value:'04006381333931',lot:'DEMO',expiry_raw:'301230',expiry_precision:'day'},product_id:'product',product:'Demo',unit:'Stück',batch:{id:'batch',lot:'DEMO',expires_on:'2030-01-01',blocked:false},warnings:['Verfall widerspricht Chargendatum.']});
   else if (request.text === '00001175') Object.assign(data,{status:'ambiguous',parsed:{kind:'ambiguous'},warnings:['Format ausdrücklich wählen.']});
  }
  else if (path.startsWith('/api/v1/stock/') && options.method === 'POST' && path !== '/api/v1/stock/inventories') {window.nonInventoryStockPosts.push({path,body:options.body});data = {movement_id:'not-expected'};}
  else if (path === '/api/v1/device-import/preview') {
   const request=JSON.parse(options.body);window.intakePreviewPosted.push(request);const mapped=request.locations?.['Offline Lager']==='warehouse';
   data={basis_hash:window.intakeBasis,new_count:mapped?1:0,unchanged_count:0,error_count:mapped?0:1,unused_count:99,location_values:['Offline Lager'],location_choices:[{id:'warehouse',name:'Lager',site:'Demo'}],rows:[{row:2,device:{inventory_number:'CSV-UI-1',name:'<img src=x onerror=alert(1)>',location_id:mapped?'warehouse':''},intake:{asset_kind:'equipment',observed_condition:'unknown',source_location:'Offline Lager'},status:mapped?'new':'error',message:mapped?'Wird neu angelegt.':'Standort zuordnen.'}]};
  }
  else if(path==='/api/v1/device-import/commit'){
   const request=JSON.parse(options.body),key=options.headers['Idempotency-Key'];window.intakePosted.push({key,body:options.body});
   if(window.intakeReplies.has(key))return new Response(JSON.stringify(window.intakeReplies.get(key)),{status:201,headers:{'Content-Type':'application/json'}});
   if(request.basis_hash!==window.intakeBasis)return new Response(JSON.stringify({message:'Vorschau geändert; CSV erneut prüfen.'}),{status:409,headers:{'Content-Type':'application/json'}});
   data={import_id:'import',created_count:1,unchanged_count:0,created_device_ids:['created-device']};window.intakeReplies.set(key,data);if(window.loseIntakeReply){window.loseIntakeReply=false;throw new Error('Lost import reply');}
  }
  else if (path === '/api/v1/stock-import/preview') {
   const request=JSON.parse(options.body);window.stockIntakePreviewPosted.push(request);const mapped=request.locations?.['Offline Lager']==='warehouse';
   data={basis_hash:window.stockIntakeBasis,new_count:mapped?1:0,unchanged_count:0,error_count:mapped?0:1,unused_count:99,location_values:['Offline Lager'],location_choices:[{id:'warehouse',name:'Lager',site:'Demo'}],rows:[{row:2,data:{source_id:'M-UI-1',name:'<img src=x onerror=alert(1)>',quantity:12,base_unit:'Stück',lot:'000123',expires_on:'2030-12-31',source_location:'Offline Lager'},status:mapped?'new':'error',message:mapped?'Einmaliger Zugang.':'Standort zuordnen.'}]};
  }
  else if(path==='/api/v1/stock-import/commit'){
   const request=JSON.parse(options.body),key=options.headers['Idempotency-Key'];window.stockIntakePosted.push({key,body:options.body});
   if(window.stockIntakeReplies.has(key))return new Response(JSON.stringify(window.stockIntakeReplies.get(key)),{status:201,headers:{'Content-Type':'application/json'}});
   if(request.basis_hash!==window.stockIntakeBasis)return new Response(JSON.stringify({message:'Vorschau geändert; CSV erneut prüfen.'}),{status:409,headers:{'Content-Type':'application/json'}});
   data={import_id:'import',created_count:1,unchanged_count:0,created_device_ids:['created-device']};window.stockIntakeReplies.set(key,data);if(window.loseStockIntakeReply){window.loseStockIntakeReply=false;throw new Error('Lost import reply');}
  }
  else if (path === '/api/v1/devices') data = [{id:'device',location_id:'warehouse',version:window.deviceVersion,incidents:window.testIncidents,active_blocks:window.deviceBlocks,inventory_number:'SYNTH-1',...window.deviceDetails}];
  else if(path==='/api/v1/devices/device/documents' && !options.method)data=window.testDocuments;
  else if(path==='/api/v1/devices/device/documents' && options.method==='POST'){
   const key=options.headers['Idempotency-Key'],request=JSON.parse(options.body);window.documentPosted.push({key,body:options.body});
   if(window.documentReplies.has(key))return new Response(JSON.stringify(window.documentReplies.get(key)),{status:200,headers:{'Content-Type':'application/json'}});
   data={id:'document',device_id:'device',filename:request.filename,size:atob(request.content).length,sha256:'a'.repeat(64),status:'quarantined'};window.testDocuments.push(data);window.documentReplies.set(key,data);
   if(window.loseDocumentReply){window.loseDocumentReply=false;throw new Error('Lost upload response');}
  }
  else if (path === '/api/v1/devices/device/book') data={id:'device',version:window.deviceVersion,incidents:window.testIncidents,active_blocks:window.deviceBlocks,master_data:window.deviceMasterData,...window.deviceDetails};
  else if(path==='/api/v1/devices/device/events')data=window.deviceHistory;
  else if (path === '/api/v1/devices/device/incidents' || path === '/api/v1/devices/device/reports' || path === '/api/v1/devices/device/corrections' || path === '/api/v1/devices/device/decommission' || path === '/api/v1/devices/device/maintenance' || path === '/api/v1/devices/device/master-data' || path === '/api/v1/devices/device/details' || path === '/api/v1/devices/device/commission' || path === '/api/v1/devices/device/rules' || path === '/api/v1/devices/device/inspections' || path === '/api/v1/devices/device/defects' || path === '/api/v1/devices/device/releases' || path === '/api/v1/devices/device/instructions') {
   const key=options.headers['Idempotency-Key'],request=JSON.parse(options.body);window.devicePosted.push({path,key,body:options.body});
   if(window.deviceReplies.has(key))return new Response(JSON.stringify(window.deviceReplies.get(key)),{status:200,headers:{'Content-Type':'application/json'}});
   if(request.expected_version!==window.deviceVersion)return new Response(JSON.stringify({message:'Geräteakte wurde geändert; erneut prüfen.'}),{status:409,headers:{'Content-Type':'application/json'}});
   if(path.endsWith('/details')){window.deviceHistory.unshift({version:window.deviceVersion+1,action:'details',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:{...request,before_details:window.deviceDetails,identity_review:true}});window.deviceDetails={...request.details};}
   if(path.endsWith('/incidents')){window.testIncidents.push({version:window.deviceVersion+1,occurred_on:request.performed_on,...request.incident,report:{incident_version:window.deviceVersion+1,status:'ASSESSMENT_REQUIRED'}});window.deviceHistory.unshift({version:window.deviceVersion+1,action:'incident',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:request});}
   if(path.endsWith('/reports')){window.testIncidents.find(i=>i.version===request.report.incident_version).report={...request.report};window.deviceHistory.unshift({version:window.deviceVersion+1,action:'report',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:request});}
   if(path.endsWith('/corrections')){window.deviceHistory.unshift({version:window.deviceVersion+1,action:'correction',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:request});}
   if(path.endsWith('/decommission')){window.deviceHistory.unshift({version:window.deviceVersion+1,action:'decommission',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:request});}
   if(path.endsWith('/maintenance')){window.deviceHistory.unshift({version:window.deviceVersion+1,action:'maintenance',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:request});}
   if(path.endsWith('/master-data')){window.deviceHistory.unshift({version:window.deviceVersion+1,action:'master_data',actor_id:'synthetic-user',created_at:new Date().toISOString(),proof:{...request,before_master_data:window.deviceMasterData}});window.deviceMasterData={...request.master_data};}
   data={id:'device',version:++window.deviceVersion};window.deviceReplies.set(key,data);
   if(window.loseDeviceReply){window.loseDeviceReply=false;throw new Error('Lost device response');}
  }
  else if (path.startsWith('/api/v1/packlists?')) data = [window.pack];
  else if (path === '/api/v1/packlists/pack/assessment') data={status:'blocked',overall_readiness:'not_assessed',reasons:['SYNTH-1: Aktive Sperre: recall'],device_basis_hash:window.devicePhysicalBasis||'d'.repeat(64),devices:[{requirement:{device_id:'device',accessories:[{product_id:'product',quantity:2}]},device:{inventory_number:'SYNTH-1',name:'Demo'}}]};
  else if (path === '/api/v1/packlists/pack/readiness') data = {scope:'material_only',overall_readiness:'not_assessed',status:'needs_check',data_as_of:new Date().toISOString(),packlist:window.pack,current_version:true,basis_hash:window.materialBasis,reasons:['Physischer Prüfnachweis fehlt.'],items:[{product:'Demo',unit:'Stück',quantity:5,physical_quantity:10,usable_quantity:10,shortfall:0,min_remaining_days:30,unknown_quantity:0}],last_check:null};
  else if (path === '/api/v1/packlists/pack/device-checks' && !options.method) data=[];
  else if (path === '/api/v1/packlists/pack/checks' && !options.method) data = [];
  else if (path === '/api/v1/packlists' || path === '/api/v1/packlists/pack/checks' || path === '/api/v1/packlists/pack/device-checks') {
   const request = JSON.parse(options.body),key = options.headers['Idempotency-Key'];window.packPosted.push({path,request,key});
   if (window.packReplies.has(key)) return new Response(JSON.stringify(window.packReplies.get(key)),{status:201,headers:{'Content-Type':'application/json'}});
   if (path.endsWith('/device-checks') && request.basis_hash !== (window.devicePhysicalBasis||'d'.repeat(64)) || path.endsWith('/checks') && request.basis_hash !== window.materialBasis || path === '/api/v1/packlists' && request.expected_version !== window.pack.version) return new Response(JSON.stringify({message:'Datenstand geändert; erneut prüfen.'}),{status:409,headers:{'Content-Type':'application/json'}});
   data = path.endsWith('/checks') || path.endsWith('/device-checks') ? {id:'proof'} : window.pack;
   window.packReplies.set(key,data);
   if (window.losePackReply) {window.losePackReply=false;throw new Error('Simulated lost response');}
  }
  else if (path === '/api/v1/stock/inventories') {
   const request = JSON.parse(options.body);window.posted.push(request);
   if (request.expected_version !== window.testBalances[0].version) return new Response(JSON.stringify({code:'stock_conflict',message:'Neue Zählung erforderlich.'}),{status:409,headers:{'Content-Type':'application/json'}});
   window.testBalances[0].quantity = request.counted_quantity;window.testBalances[0].usable_quantity = request.counted_quantity;window.testBalances[0].version++;
   data = {movement_id:'synthetic-inventory'};
  } else return realFetch(input,options);
  return new Response(JSON.stringify(data),{status:options.method === 'POST' ? 201 : 200,headers:{'Content-Type':'application/json','X-Data-As-Of':new Date().toISOString(),'X-Has-More':'false'}});
 };
 `});
 await call('Page.navigate',{url});
 await until("document.querySelector('#booking') && !document.querySelector('#booking').hidden");
 await until("document.querySelector('#attention-list').textContent.includes('Synthetic overdue')");
 assert.equal(await evaluate("document.querySelector('#attention-list img')===null"),true);
 await until("document.querySelector('#reminder-list').textContent.includes('<svg')");
 await until("document.querySelector('#reminder-mail-list').textContent.includes('Annahme unklar')");
 assert.equal(await evaluate("document.querySelector('#reminder-mail-list svg')===null"),true);
 await evaluate('window.failMail=true;void loadDeviceAttention()');
 await until("document.querySelector('#reminder-mail-status').textContent.includes('Synthetic mail status unavailable')");
 assert.equal(await evaluate("document.querySelector('#reminder-mail-list').children.length"),0);
 await evaluate('void loadDeviceAttention()');
 await until("document.querySelector('#reminder-mail-list').children.length===1");

 assert.equal(await evaluate("document.querySelector('#reminder-list svg')===null"),true);
 assert.equal(await evaluate("document.querySelector('#reminder-status').textContent.includes('seit über zwei Stunden')"),true);
 await evaluate("window.failReminders=true;void loadDeviceAttention()");
 await until("document.querySelector('#reminder-status').textContent.includes('Synthetic worker unavailable')");
 assert.equal(await evaluate("document.querySelector('#reminder-list').children.length"),0);
 await evaluate('void loadDeviceAttention()');
 await until("document.querySelector('#reminder-list').children.length===1");

 await evaluate("document.querySelector('#attention-filter').value='block';document.querySelector('#attention-filter').dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#attention-list').textContent.includes('Synthetic overdue')"),false);
 await evaluate("window.holdAttention=true;void loadDeviceAttention()");
 await until('typeof window.releaseAttention === "function"');
 await evaluate("document.querySelector('#attention-days').value='0';document.querySelector('#attention-days').dispatchEvent(new Event('change'))");
 await until("document.querySelector('#attention-status').textContent.includes('Zeitraum 0 Tage')");
 await evaluate('window.releaseAttention()');
 await evaluate('new Promise(resolve=>setTimeout(resolve,50))');
 assert.equal(await evaluate('attentionBoard.horizon_days'),0);
 await evaluate("document.querySelector('#attention-filter').value='all';document.querySelector('#attention-days').value='60';document.querySelector('#attention-days').dispatchEvent(new Event('change'))");
 await until("document.querySelector('#attention-list').textContent.includes('Synthetic overdue')");
 await evaluate("document.querySelector('#action').value='inventories';document.querySelector('#action').dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#quantity').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#booking button[type=submit]').disabled"),true);
 await evaluate("document.querySelector('#start-count').click()");
 await until("!document.querySelector('#quantity').disabled");
 assert.equal(await evaluate("document.querySelector('#quantity').value"),'');
 assert.equal(await evaluate("document.querySelector('#target').disabled"),true);
 await evaluate("window.testBalances[0].quantity=12;window.testBalances[0].version=2;document.querySelector('#reload').click()");
 await until("document.querySelector('#stock-rows').textContent.includes('12 Stück')");
 await evaluate("document.querySelector('#quantity').value='7';document.querySelector('#note').value='Synthetische Zählung';document.querySelector('#booking').requestSubmit()");
 await until('window.posted.length === 1');
 assert.deepEqual(await evaluate('({version:window.posted[0].expected_version,count:window.posted[0].counted_quantity})'),{version:1,count:7});
 await until("document.querySelector('#status').textContent.includes('Neue Zählung erforderlich.')");
 assert.equal(await evaluate("document.querySelector('#quantity').disabled"),true);
 await evaluate("document.querySelector('#start-count').click()");
 await until("!document.querySelector('#quantity').disabled");
 await evaluate("document.querySelector('#quantity').value='0';document.querySelector('#note').value='Leer gezählt';document.querySelector('#booking').requestSubmit()");
 await until('window.posted.length === 2');
 assert.deepEqual(await evaluate('({version:window.posted[1].expected_version,count:window.posted[1].counted_quantity})'),{version:2,count:0});
 await until("document.querySelector('#status').textContent.includes('Buchung bestätigt')");
 await until("document.querySelector('#pack-choice').value === 'pack'");
 assert.equal(await evaluate("document.querySelector('#material-check button[type=submit]').disabled"),true);
 await evaluate("document.querySelector('#check-start').click()");
 await until("!document.querySelector('#check-result').disabled");
 assert.equal(await evaluate("document.querySelector('#check-result').value"),'');
 await evaluate("window.materialBasis='b'.repeat(64);document.querySelector('#pack-refresh').click()");
 await until("material && material.basis_hash === 'b'.repeat(64)");
 await evaluate("document.querySelector('#check-result').value='passed';document.querySelector('#check-note').value='Physisch geprüft';document.querySelector('#material-check').requestSubmit()");
 await until('window.packPosted.length === 1');
 assert.equal(await evaluate('window.packPosted[0].request.basis_hash'),'a'.repeat(64));
 await until("document.querySelector('#status').textContent.includes('Datenstand geändert')");
 assert.equal(await evaluate("document.querySelector('#material-check button[type=submit]').disabled"),true);
 await evaluate("document.querySelector('#check-start').click()");
 await until("!document.querySelector('#check-result').disabled");
 await evaluate("window.losePackReply=true;document.querySelector('#check-result').value='failed';document.querySelector('#check-note').value='Verpackung beschädigt';document.querySelector('#material-check').requestSubmit()");
 await until("document.querySelector('#status').textContent.includes('Vorgang nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#check-result').disabled"),true);
 await evaluate("document.querySelector('#material-check').requestSubmit()");
 await until('window.packPosted.length === 3');
 assert.deepEqual(await evaluate('window.packPosted[1]'),await evaluate('window.packPosted[2]'));
 assert.equal(await evaluate('window.packPosted[2].request.physical_passed'),false);
 await until("document.querySelector('#status').textContent.includes('Packlistenvorgang bestätigt')");
 await evaluate("document.querySelector('#pack-edit-start').click()");
 await until("!document.querySelector('#pack-title').disabled");
 await evaluate("document.querySelector('#pack-add-device').click();document.querySelector('.pack-device-id').value='device';[...document.querySelector('#pack-devices fieldset').querySelectorAll('button')].find(button=>button.textContent==='Zubehör hinzufügen').click();document.querySelector('.accessory-product').value='product';document.querySelector('.accessory-quantity').value='2'");
 await evaluate("window.pack.version=2;document.querySelector('#pack-refresh').click()");
 await until("document.querySelector('#pack-choice').textContent.includes('Sollversion 2')");
 await evaluate("document.querySelector('#pack-note').value='Sollmenge geändert';document.querySelector('#pack-editor').requestSubmit()");
 await until('window.packPosted.length === 4');
 assert.equal(await evaluate('window.packPosted[3].request.expected_version'),1);
 assert.deepEqual(await evaluate('window.packPosted[3].request.devices'),[{device_id:'device',accessories:[{product_id:'product',quantity:2}]}]);
 assert.equal(await evaluate("document.querySelector('#assessment-reasons').textContent.includes('recall')"),true);
 await until("document.querySelector('#status').textContent.includes('Datenstand geändert')");
 assert.equal(await evaluate("document.querySelector('#pack-editor button[type=submit]').disabled"),true);
 await evaluate("document.querySelector('#physical-device-start').click()");
 await until("document.querySelectorAll('#physical-device-results select').length===3 && !document.querySelector('.device-present').disabled");
 assert.deepEqual(await evaluate("[...document.querySelectorAll('#physical-device-results select')].map(s=>s.value)"),['','','']);
 await evaluate("window.devicePhysicalBasis='e'.repeat(64);document.querySelector('#pack-refresh').click()");
 await until("packAssessment?.device_basis_hash==='e'.repeat(64)");
 await evaluate("for(const s of document.querySelectorAll('#physical-device-results select'))s.value='passed';document.querySelector('#physical-device-note').value='Physisch am Gerät geprüft';document.querySelector('#physical-device-check').requestSubmit()");
 await until('window.packPosted.length===5');
 assert.equal(await evaluate('window.packPosted[4].request.basis_hash'),'d'.repeat(64));
 await until("document.querySelector('#status').textContent.includes('Datenstand geändert')");
 await until('packPending === null');
 await evaluate("document.querySelector('#physical-device-start').click()");
 await until("document.querySelectorAll('#physical-device-results select').length===3 && !document.querySelector('.device-present').disabled");
 await evaluate("window.losePackReply=true;for(const s of document.querySelectorAll('#physical-device-results select'))s.value='passed';document.querySelector('.device-functional').value='failed';document.querySelector('#physical-device-note').value='Funktion fehlt';document.querySelector('#physical-device-check').requestSubmit()");
 await until("document.querySelector('#status').textContent.includes('Vorgang nicht bestätigt')");
 await evaluate("document.querySelector('#physical-device-check').requestSubmit()");
 await until('window.packPosted.length===7');
 assert.deepEqual(await evaluate('window.packPosted[5]'),await evaluate('window.packPosted[6]'));
 assert.deepEqual(await evaluate('window.packPosted[6].request.results'),[{device_id:'device',present:true,functional:false,accessories_complete:true}]);
 await until("document.querySelector('#status').textContent.includes('Packlistenvorgang bestätigt')");
 await evaluate("document.querySelector('#scan-text').value='4006381333931';document.querySelector('#scan-form').requestSubmit()");
 await until("document.querySelector('#scan-status').textContent.includes('noch nicht zugeordnet')");
 assert.equal(await evaluate("document.querySelector('#identifier-quantity').value"),'');
 await evaluate("window.loseIdentifierReply=true;document.querySelector('#identifier-product').value='product';document.querySelector('#identifier-quantity').value='10';document.querySelector('#identifier-note').value='Packung enthält zehn Stück';document.querySelector('#identifier-form').requestSubmit()");
 await until("document.querySelector('#status').textContent.includes('Kennungsvorgang nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#identifier-product').disabled"),true);
 await evaluate("document.querySelector('#identifier-form').requestSubmit()");
 await until('window.identifierPosted.length === 2');
 assert.deepEqual(await evaluate('window.identifierPosted[0]'),await evaluate('window.identifierPosted[1]'));
 await until("document.querySelector('#scan-status').textContent.includes('Artikel erkannt')");
 assert.equal(await evaluate("document.querySelector('#scan-batch').value"),'');
 assert.equal(await evaluate("document.querySelector('#scan-confirm').checked"),false);
 await evaluate("document.querySelector('#scan-batch').value='batch';document.querySelector('#scan-batch').dispatchEvent(new Event('change'));document.querySelector('#scan-purpose').value='receipts';document.querySelector('#scan-purpose').dispatchEvent(new Event('change'));document.querySelector('#scan-target').value='warehouse';document.querySelector('#scan-packs').value='3';document.querySelector('#scan-confirm').checked=true;document.querySelector('#scan-handoff').requestSubmit()");
 await until("document.querySelector('#status').textContent.includes('Scan in Buchungsformular übernommen')");
 assert.equal(await evaluate("document.querySelector('#quantity').value"),'30');
 assert.equal(await evaluate('window.nonInventoryStockPosts.length'),0);
 assert.equal(await evaluate('window.posted.length'),2);
 await evaluate("document.querySelector('#booking').requestSubmit()");
 await until('window.nonInventoryStockPosts.length === 1');
 assert.deepEqual(await evaluate('({quantity:JSON.parse(window.nonInventoryStockPosts[0].body).quantity,identifier:JSON.parse(window.nonInventoryStockPosts[0].body).identifier_id,packs:JSON.parse(window.nonInventoryStockPosts[0].body).pack_count})'),{quantity:30,identifier:'identifier',packs:3});
 await until("document.querySelector('#status').textContent.includes('Buchung bestätigt')");
 await evaluate("document.querySelector('#scan-text').value='conflicting-GS1';document.querySelector('#scan-form').requestSubmit()");
 await until("document.querySelector('#scan-status').textContent.includes('Verfall widerspricht')");
 assert.equal(await evaluate("document.querySelector('#scan-handoff').hidden"),true);
 await evaluate("document.querySelector('#scan-text').value='00001175';document.querySelector('#scan-form').requestSubmit()");
 await until("document.querySelector('#scan-status').textContent.includes('mehrdeutig')");
 assert.equal(await evaluate("document.querySelector('#scan-handoff').hidden"),true);
 const beforeURL = await evaluate('location.href');
 await evaluate("document.querySelector('#scan-text').value='https://example.invalid/unsafe';document.querySelector('#scan-form').requestSubmit()");
 await until("document.querySelector('#scan-status').textContent.includes('Unbekanntes Format')");
 assert.equal(await evaluate('location.href'),beforeURL);
 assert.equal(await evaluate('window.nonInventoryStockPosts.length'),1);
 await evaluate("document.querySelector('#devices-refresh').click()");
 await until("document.querySelector('#devices-status').textContent.includes('1 Geräte')");
 assert.equal(await evaluate("document.querySelector('#devices-list a').getAttribute('href')"),'/api/v1/devices/device/export.zip');
 assert.equal(await evaluate("document.querySelector('#devices-list a').download"),'geraeteakte.zip');
 assert.equal(await evaluate("document.querySelector('#devices-list').textContent.includes('ungeklärt')"),true);
 assert.equal(await evaluate("document.querySelector('#devices-list').textContent.includes('<img src=x onerror=alert(1)>')"),true);
 assert.equal(await evaluate("document.querySelector('#devices-list img') === null"),true);
 assert.equal(await evaluate("document.querySelector('#device-control-form button[type=submit]').disabled"),true);
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteversion 0')");
 await evaluate("document.querySelector('#device-date').value='2020-03-15';document.querySelector('#device-evidence').value='protocol';document.querySelector('#device-qualification').value='qualification';document.querySelector('#device-assignment').value='assignment';document.querySelector('#device-note').value='synthetic proof';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#device-date').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 2');
 assert.deepEqual(await evaluate('window.devicePosted[0]'),await evaluate('window.devicePosted[1]'));
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 assert.equal(await evaluate("document.querySelector('#device-control-form button[type=submit]').disabled"),true);
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteversion 1')");
 await evaluate("window.deviceVersion=2;document.querySelector('#devices-refresh').click()");
 await until("document.querySelector('#devices-status').textContent.includes('1 Geräte')");
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteakte wurde geändert')");
 assert.equal(await evaluate('JSON.parse(window.devicePosted[2].body).expected_version'),1);
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteversion 2')");
 await evaluate("document.querySelector('#device-action').value='rules';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-STK-applicability').value='required';document.querySelector('#device-STK-mode').value='month_end';document.querySelector('#device-STK-interval_months').value='24';document.querySelector('#device-STK-evidence').value='STK product rule';document.querySelector('#device-MTK-applicability').value='required';document.querySelector('#device-MTK-mode').value='year_end';document.querySelector('#device-MTK-interval_months').value='24';document.querySelector('#device-MTK-evidence').value='MTK product rule';document.querySelector('#device-electrical-applicability').value='exempt';document.querySelector('#device-electrical-evidence').value='assessed exemption';document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 4');
 assert.deepEqual(await evaluate('({path:window.devicePosted[3].path,version:JSON.parse(window.devicePosted[3].body).expected_version,types:JSON.parse(window.devicePosted[3].body).rules.map(r=>r.control_type)})'),{path:'/api/v1/devices/device/rules',version:2,types:['STK','MTK','electrical']});
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteversion 3')");
 await evaluate("document.querySelector('#device-action').value='inspection';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-inspection-result').value='passed';document.querySelector('#device-examiner').value='Examiner';document.querySelector('#device-method').value='Protocol';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 6');
 assert.deepEqual(await evaluate('window.devicePosted[4]'),await evaluate('window.devicePosted[5]'));
 assert.deepEqual(await evaluate('({path:window.devicePosted[4].path,result:JSON.parse(window.devicePosted[4].body).result,kind:JSON.parse(window.devicePosted[4].body).control_type})'),{path:'/api/v1/devices/device/inspections',result:'passed',kind:'STK'});
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteversion 4')");
 await evaluate("document.querySelector('#device-action').value='release';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 7');
 assert.deepEqual(await evaluate('({path:window.devicePosted[6].path,block:JSON.parse(window.devicePosted[6].body).block_id})'),{path:'/api/v1/devices/device/releases',block:'recall-block'});
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-basis').textContent.includes('Geräteversion 5')");
 await evaluate("const transfer=new DataTransfer();transfer.items.add(new File(['Synthetic instruction proof\\n'],'proof.txt',{type:'text/plain'}));document.querySelector('#document-file').files=transfer.files;document.querySelector('#document-note').value='Synthetic proof';window.loseDocumentReply=true;document.querySelector('#document-upload-form').requestSubmit()");
 await until("document.querySelector('#document-status').textContent.includes('Upload nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#document-file').disabled"),true);
 await evaluate("document.querySelector('#document-upload-form').requestSubmit()");
 await until('window.documentPosted.length === 2');
 assert.deepEqual(await evaluate('window.documentPosted[0]'),await evaluate('window.documentPosted[1]'));
 await until("document.querySelector('#document-status').textContent.includes('in Quarantäne')");
 await until("document.querySelector('#device-instruction-document').options.length === 2");
 assert.equal(await evaluate("document.querySelector('#document-list a').getAttribute('href').endsWith('/download?review=true')"),true);
 await evaluate("document.querySelector('#device-action').value='instruction';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-instructor').value='Instructor';document.querySelector('#device-trainee').value='Trainee';document.querySelector('#device-document-version').value='Revision 2';document.querySelector('#device-instruction-document').value='document';document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 8');
 assert.deepEqual(await evaluate('({path:window.devicePosted[7].path,doc:JSON.parse(window.devicePosted[7].body).document_id,scope:JSON.parse(window.devicePosted[7].body).instruction_scope})'),{path:'/api/v1/devices/device/instructions',doc:'document',scope:'device'});
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("deviceDraft && !document.querySelector('#device-control-form button[type=submit]').disabled");
 await evaluate("document.querySelector('#device-action').value='details';document.querySelector('#device-action').dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#device-details-fields').hidden"),false);
 assert.equal(await evaluate("document.querySelector('#device-detail-serial').value"),'SERIAL');
 assert.equal(await evaluate("document.querySelector('#device-qualification').required"),false);
 await evaluate("document.querySelector('#device-detail-name').value='<svg onload=alert(1)>';document.querySelector('#device-detail-model').value='Corrected model';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#device-detail-model').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 assert.deepEqual(await evaluate('window.devicePosted.at(-1)'),await evaluate('window.devicePosted.at(-2)'));
 assert.deepEqual(await evaluate('JSON.parse(window.devicePosted.at(-1).body).details'),{name:'<svg onload=alert(1)>',manufacturer:'Demo',model:'Corrected model',serial_or_lot:'SERIAL'});
 assert.equal(await evaluate('JSON.parse(window.devicePosted.at(-1).body).qualification===undefined'),true);
 await until("document.querySelector('#device-history').textContent.includes('Corrected model')");
 assert.equal(await evaluate("document.querySelector('#device-history svg')===null"),true);
 assert.equal(await evaluate("document.querySelector('#device-history').textContent.includes('Quarantänesperre')"),true);
 await evaluate("document.querySelector('#device-begin').click()");
 await until("!document.querySelector('#device-control-form button[type=submit]').disabled");
 await evaluate("document.querySelector('#device-action').value='master_data';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#master-acquisition-year').value='2024';document.querySelector('#master-asset-type').value='Defibrillator';document.querySelector('#master-actor-role').value='manufacturer';document.querySelector('#master-actor-name').value='Synthetic Maker';document.querySelector('#master-actor-address').value='<img src=x onerror=alert(1)>';document.querySelector('#master-operational-assignment').value='Bereitschaft';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#master-actor-address').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 12');
 assert.deepEqual(await evaluate('window.devicePosted[10]'),await evaluate('window.devicePosted[11]'));
 assert.equal(await evaluate('window.devicePosted[11].path'),'/api/v1/devices/device/master-data');
 assert.equal(await evaluate('JSON.parse(window.devicePosted[11].body).master_data.acquisition_year'),'2024');
 assert.equal(await evaluate("'qualification' in JSON.parse(window.devicePosted[11].body)"),false);
 await until("document.querySelector('#device-history').textContent.includes('Wirtschaftsakteur-Anschrift')");
 assert.equal(await evaluate("document.querySelector('#device-history img')===null"),true);
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#master-actor-name').value==='Synthetic Maker'");
 await evaluate("document.querySelector('#device-action').value='maintenance';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-date').value='2024-02-01';document.querySelector('#maintenance-kind').value='repair';document.querySelector('#maintenance-executor').value='Synthetic company';document.querySelector('#maintenance-measure').value='<svg onload=alert(1)>';document.querySelector('#maintenance-result').value='completed';document.querySelector('#maintenance-document').value='document';document.querySelector('#device-qualification').value='Synthetic qualification';document.querySelector('#device-assignment').value='Synthetic assignment';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#maintenance-result').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#maintenance-document').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 14');
 assert.deepEqual(await evaluate('window.devicePosted[12]'),await evaluate('window.devicePosted[13]'));
 assert.deepEqual(await evaluate("({path:window.devicePosted[13].path,kind:JSON.parse(window.devicePosted[13].body).maintenance_kind,doc:JSON.parse(window.devicePosted[13].body).document_id})"),{path:'/api/v1/devices/device/maintenance',kind:'repair',doc:'document'});
 await until("document.querySelector('#device-history').textContent.includes('Synthetic company')");
 assert.equal(await evaluate("document.querySelector('#device-history svg')===null"),true);
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("deviceDraft && !document.querySelector('#device-control-form button[type=submit]').disabled");
 await evaluate("document.querySelector('#device-action').value='decommission';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-date').value='2024-02-29';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#device-date').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 16');
 assert.deepEqual(await evaluate('window.devicePosted[14]'),await evaluate('window.devicePosted[15]'));
 assert.equal(await evaluate('window.devicePosted[15].path'),'/api/v1/devices/device/decommission');
 assert.equal(await evaluate("'qualification' in JSON.parse(window.devicePosted[15].body)"),false);
 await until("document.querySelector('#device-history').textContent.includes('Außerbetriebnahme: 2024-02-29')");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("deviceDraft && !document.querySelector('#device-control-form button[type=submit]').disabled && [...document.querySelector('#correction-target').options].some(o=>o.value==='9')");
 assert.equal(await evaluate("[...document.querySelector('#correction-target').options].find(o=>o.value==='9').textContent.includes('Instandhaltung')"),true);
 await evaluate("document.querySelector('#device-action').value='correction';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#correction-target').value='9';document.querySelector('#correction-statement').value='<svg onload=alert(1)> corrected examiner';document.querySelector('#correction-document').value='document';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#correction-target').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#correction-statement').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 18');
 assert.deepEqual(await evaluate('window.devicePosted[16]'),await evaluate('window.devicePosted[17]'));
 assert.equal(await evaluate('window.devicePosted[17].path'),'/api/v1/devices/device/corrections');
 assert.equal(await evaluate('JSON.parse(window.devicePosted[17].body).correction.target_version'),9);
 await until("document.querySelector('#device-history').textContent.includes('corrected examiner')");
 assert.equal(await evaluate("document.querySelector('#device-history svg')===null"),true);
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("deviceDraft && !document.querySelector('#device-control-form button[type=submit]').disabled");
 await evaluate("document.querySelector('#device-action').value='incident';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#device-date').value='2024-02-01';document.querySelector('#incident-kind').value='suspected_serious_incident';document.querySelector('#incident-description').value='<svg onload=alert(1)> synthetic incident';document.querySelector('#incident-consequences').value='Consequences unknown; synthetic case';document.querySelector('#incident-document').value='document';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#incident-kind').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 20');
 assert.deepEqual(await evaluate('window.devicePosted[18]'),await evaluate('window.devicePosted[19]'));
 assert.equal(await evaluate("'qualification' in JSON.parse(window.devicePosted[19].body)"),false);
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 assert.equal(await evaluate("document.querySelector('#device-history svg')===null"),true);
 await evaluate("document.querySelector('#device-begin').click()");
 await until("deviceDraft?.incidents?.length===1 && !document.querySelector('#device-control-form button[type=submit]').disabled");
 await evaluate("document.querySelector('#device-action').value='report';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#report-incident').value='12';document.querySelector('#report-incident').dispatchEvent(new Event('change'));document.querySelector('#report-status').value='REPORTED';document.querySelector('#report-responsible').value='Synthetic safety officer';document.querySelector('#report-authority').value='Synthetic authority';document.querySelector('#report-authority-reference').value='SYNTH-A-1';document.querySelector('#report-authority-reported-at').value='2024-02-02T12:00:00';window.loseDeviceReply=true;document.querySelector('#device-control-form').requestSubmit()");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#report-status').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#report-authority-reference').disabled"),true);
 await evaluate("document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length === 22');
 assert.deepEqual(await evaluate('window.devicePosted[20]'),await evaluate('window.devicePosted[21]'));
 assert.equal(await evaluate("JSON.parse(window.devicePosted[21].body).report.incident_version"),12);
 assert.equal(await evaluate("Date.parse(JSON.parse(window.devicePosted[21].body).report.authority_reported_at)===new Date('2024-02-02T12:00:00').getTime()"),true);
 await until("document.querySelector('#device-history').textContent.includes('SYNTH-A-1')");
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");
 await evaluate("document.querySelector('#device-begin').click()");
 await until("deviceDraft?.incidents?.[0]?.report?.status==='REPORTED'");
 await evaluate("document.querySelector('#report-incident').value='12';document.querySelector('#report-incident').dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#report-authority-reference').value"),'SYNTH-A-1');
 await evaluate("document.querySelector('#device-action').value='report';document.querySelector('#device-action').dispatchEvent(new Event('change'));document.querySelector('#report-status').value='FOLLOW_UP';document.querySelector('#device-control-form').requestSubmit()");
 await until('window.devicePosted.length===23');
 assert.equal(await evaluate('JSON.parse(window.devicePosted[22].body).report.authority_reported_at'),await evaluate('JSON.parse(window.devicePosted[21].body).report.authority_reported_at'));
 await until("document.querySelector('#device-basis').textContent.includes('Nachweis gespeichert')");

 assert.equal(await evaluate("document.querySelector('#device-import').hidden"),false);
 assert.equal(await evaluate("document.querySelector('#device-import-commit').disabled"),true);
 await evaluate("const intakeTransfer=new DataTransfer();intakeTransfer.items.add(new File(['Inventarnummer;Bezeichnung;Standort;Seriennummer\\nCSV-UI-1;Equipment;Offline Lager;000123\\n'],'intake.csv',{type:'text/csv'}));document.querySelector('#device-import-file').files=intakeTransfer.files;document.querySelector('#device-import-file').dispatchEvent(new Event('change'));document.querySelector('#device-import-form').requestSubmit()");
 await until("document.querySelector('#device-import-status').textContent.includes('1 Fehler')");
 assert.equal(await evaluate("document.querySelector('#device-import-commit').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#device-import-rows img')===null"),true);
 assert.equal(await evaluate("document.querySelector('#device-import-rows').textContent.includes('<img src=x onerror=alert(1)>')"),true);
 await evaluate("const select=document.querySelector('#device-import-locations select');select.value='warehouse';select.dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#device-import-commit').disabled"),true);
 await evaluate("document.querySelector('#device-import-form').requestSubmit()");
 await until("!document.querySelector('#device-import-commit').disabled");
 await evaluate("window.intakeBasis='2'.repeat(64);document.querySelector('#device-import-commit').click()");
 await until("document.querySelector('#device-import-status').textContent.includes('Vorschau geändert')");
 assert.equal(await evaluate("document.querySelector('#device-import-commit').disabled"),true);
 assert.equal(await evaluate("JSON.parse(window.intakePosted[0].body).basis_hash"),'1'.repeat(64));
 await evaluate("document.querySelector('#device-import-form').requestSubmit()");
 await until("!document.querySelector('#device-import-commit').disabled");
 await evaluate("window.loseIntakeReply=true;document.querySelector('#device-import-commit').click()");
 await until("document.querySelector('#device-import-status').textContent.includes('Import nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#device-import-file').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#device-import-locations select').disabled"),true);
 await evaluate("document.querySelector('#device-import-commit').click()");
 await until("document.querySelector('#device-import-status').textContent.includes('Import bestätigt: 1 neu')");
 assert.equal(await evaluate('window.intakePosted.length'),3);
 assert.deepEqual(await evaluate('window.intakePosted[1]'),await evaluate('window.intakePosted[2]'));
 assert.equal(await evaluate("JSON.parse(window.intakePosted[2].body).locations['Offline Lager']"),'warehouse');
 assert.equal(await evaluate("JSON.parse(window.intakePosted[2].body).csv.includes('000123')"),true);
 assert.equal(await evaluate("document.querySelector('#stock-import').hidden"),false);
 assert.equal(await evaluate("document.querySelector('#stock-import-commit').disabled"),true);
 await evaluate("const stockCSVTransfer=new DataTransfer();stockCSVTransfer.items.add(new File(['Erfassungsnummer;Bezeichnung;Standort;Menge;Basiseinheit;Fremdcode\\nM-UI-1;Material;Offline Lager;12;Stück;000123\\n'],'intake.csv',{type:'text/csv'}));document.querySelector('#stock-import-file').files=stockCSVTransfer.files;document.querySelector('#stock-import-file').dispatchEvent(new Event('change'));document.querySelector('#stock-import-form').requestSubmit()");
 await until("document.querySelector('#stock-import-status').textContent.includes('1 Fehler')");
 assert.equal(await evaluate("document.querySelector('#stock-import-commit').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#stock-import-rows img')===null"),true);
 assert.equal(await evaluate("document.querySelector('#stock-import-rows').textContent.includes('<img src=x onerror=alert(1)>')"),true);
 await evaluate("const stockSelect=document.querySelector('#stock-import-locations select');stockSelect.value='warehouse';stockSelect.dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#stock-import-commit').disabled"),true);
 await evaluate("document.querySelector('#stock-import-form').requestSubmit()");
 await until("!document.querySelector('#stock-import-commit').disabled");
 await evaluate("window.stockIntakeBasis='2'.repeat(64);document.querySelector('#stock-import-commit').click()");
 await until("document.querySelector('#stock-import-status').textContent.includes('Vorschau geändert')");
 assert.equal(await evaluate("document.querySelector('#stock-import-commit').disabled"),true);
 assert.equal(await evaluate("JSON.parse(window.stockIntakePosted[0].body).basis_hash"),'1'.repeat(64));
 await evaluate("document.querySelector('#stock-import-form').requestSubmit()");
 await until("!document.querySelector('#stock-import-commit').disabled");
 await evaluate("document.querySelector('#stock-import-mode').value='receipt';document.querySelector('#stock-import-mode').dispatchEvent(new Event('change'))");
 assert.equal(await evaluate("document.querySelector('#stock-import-commit').disabled"),true);
 await evaluate("document.querySelector('#stock-import-form').requestSubmit()");
 await until("!document.querySelector('#stock-import-commit').disabled");
 assert.equal(await evaluate("window.stockIntakePreviewPosted.at(-1).mode"),'receipt');
 await evaluate("window.loseStockIntakeReply=true;document.querySelector('#stock-import-commit').click()");
 await until("document.querySelector('#stock-import-status').textContent.includes('Import nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#stock-import-file').disabled"),true);
 assert.equal(await evaluate("document.querySelector('#stock-import-locations select').disabled"),true);
 await evaluate("document.querySelector('#stock-import-commit').click()");
 await until("document.querySelector('#stock-import-status').textContent.includes('Import bestätigt: 1 neu')");
 assert.equal(await evaluate('window.stockIntakePosted.length'),3);
 assert.deepEqual(await evaluate('window.stockIntakePosted[1]'),await evaluate('window.stockIntakePosted[2]'));
 assert.equal(await evaluate("JSON.parse(window.stockIntakePosted[2].body).locations['Offline Lager']"),'warehouse');
 assert.equal(await evaluate("JSON.parse(window.stockIntakePosted[2].body).csv.includes('000123')"),true);
 await evaluate('window.holdMail=true;void loadDeviceAttention()');
 await until('typeof window.releaseMail === "function"');
 await evaluate("signedOut('Test logout')");
 assert.equal(await evaluate("document.querySelector('#reminder-mail-list').children.length"),0);
 await evaluate('window.releaseMail()');
 await evaluate('new Promise(resolve=>setTimeout(resolve,50))');
 assert.equal(await evaluate("document.querySelector('#reminder-list').children.length"),0);
 assert.equal(await evaluate("document.querySelector('#reminder-mail-list').children.length"),0);

 assert.equal(await evaluate("document.querySelector('#attention-list').children.length"),0);
 assert.equal(await evaluate("document.querySelector('#attention-counts').textContent"),'');
 assert.equal(await evaluate("document.querySelector('#devices-list').children.length"),0);
 console.log('Browser UI passed: SMTP status and uncertainty, safe text, failure clearing and late mail response after logout; automatic reminder status, stale worker warning, safe text, error clearing and late response after logout; scoped attention filters, safe text, civil horizon, stale-response rejection and logout clearing; versioned device-details prefill, frozen retry and safe before/after history; material CSV mode invalidation, mapping, safe text and exact retry; device CSV file preview, explicit location mapping, safe text, stale-preview rejection and exact import retry; explicit physical device results, frozen basis and exact lost-response retry; device/accessory requirement form and independent blocked assessment; exact attachment upload retry, quarantine download link and explicit instruction binding; inspection proof retry and explicit individual-block release, frozen device version, exact proof retry after lost response, stale-version rejection, scoped device overview, safe text rendering and logout clearing; frozen count/pack/check bases, exact retries, explicit identifier quantity and scan confirmation, packaging conversion without booking, rejection of conflicting/ambiguous codes and URL navigation (simulated API).');

} finally {
 socket?.close();browser.kill('SIGTERM');
 await new Promise(resolve => {if (browser.exitCode !== null) resolve();else browser.once('exit',resolve);});
 server.close();await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100});
}
