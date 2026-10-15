// Browser regression check for module scope. The API is simulated;
// real SQLite/HTTP behavior is covered by Go tests and the Compose smoke check.
import {spawn} from 'node:child_process';
import {createServer} from 'node:http';
import {mkdtemp,readFile,rm,writeFile} from 'node:fs/promises';
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
 await call('Emulation.setEmulatedMedia',{features:[{name:'prefers-reduced-motion',value:'reduce'}]});
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});

 await call('Page.addScriptToEvaluateOnNewDocument',{source:`
 const dependencies={inventory:[],devices:[],documents:['devices'],compliance:['devices','documents'],reprocessing:['compliance'],packs:[],release:['packs','devices','documents','compliance','reprocessing','recalls'],recalls:[],reminders:['devices']};
 window.moduleConfig=(enabled)=>({revision:1,enabled_modules:enabled,modules:Object.entries(dependencies).map(([id,depends_on])=>({id,name:id==='inventory'?'Inventarisierung':'<img src=x> '+id,enabled:enabled.includes(id),required:id==='inventory',depends_on}))});
 window.config=window.moduleConfig(['inventory']);window.requests=[];window.deviceName='Equipment';window.detailPosts=[];window.replies=new Map();window.userName='test';window.packs=[];
 window.visible=(selector)=>{const node=document.querySelector(selector);return Boolean(node&&!node.closest('[hidden],[data-module-disabled=\"true\"]')&&!node.closest('#inventory.modules-unavailable'));};
 window.fetch=async(input,options={})=>{
  const actualPath=typeof input==='string'?input:input.url;const path=actualPath.startsWith('/api/v1/inventory/devices?')?'/api/v1/devices':actualPath;window.requests.push({path,method:options.method||'GET'});let data;
  if(path==='/api/v1/setup/status')data={pending:false};
  else if(path==='/api/v1/me')data={id:'synthetic-user',username:window.userName,administrator:false,csrf_token:'synthetic',rights:[{site_id:'site',role:'material_manager'},{site_id:'site',role:'device_manager'}]};
  else if(path==='/api/v1/modules'){if(window.failModules)return new Response(JSON.stringify({message:'Configuration unavailable'}),{status:503});data=structuredClone(window.config);if(window.holdModules){window.holdModules=false;await new Promise(resolve=>window.releaseModules=resolve);}}
  else if(path.startsWith('/api/v1/auth/'))data={csrf_token:'synthetic'};
  else if(path==='/api/v1/locations')data=[{id:'warehouse',name:'Lager',site_id:'site',site:'Demo',operator:'Demo'}];
  else if(path==='/api/v1/stock/catalog')data=[{id:'batch',product_id:'product',product:'Material',lot:'DEMO',unit:'Stück'}];
  else if(path==='/api/v1/stock/balances')data=[];
  else if(path.startsWith('/api/v1/stock/movements'))data=[];
  else if(path==='/api/v1/product-identifiers')data=[];
  else if(path==='/api/v1/devices')data=[{id:'device',inventory_number:'DRK-TEST',name:window.deviceName,location_id:'warehouse',manufacturer:'Demo',model:'M1',serial_or_lot:'SERIAL',version:1,active_blocks:[],instructions:[],incidents:[]}];
  else if(path==='/api/v1/devices/device/book')data={id:'device',inventory_number:'DRK-TEST',location_id:'warehouse',name:window.deviceName,manufacturer:'Demo',model:'M1',serial_or_lot:'SERIAL',version:1,incidents:[],active_blocks:[],instructions:[]};
  else if(path==='/api/v1/devices/device/details'){
   const key=options.headers['Idempotency-Key'];window.detailPosts.push({key,body:options.body});
   if(window.replies.has(key))data=window.replies.get(key);else{data={version:2};window.replies.set(key,data);window.deviceName=JSON.parse(options.body).details.name;if(window.loseDetailReply){window.loseDetailReply=false;throw Error('Lost response');}}
  }
  else if(path==='/api/v1/devices/device/events')data=[];
  else if(path==='/api/v1/devices/device/documents')data=[];
  else if(path==='/api/v1/devices/device/manual-access?user_id=synthetic-user')data={manager:true,assessment:{user_id:'synthetic-user',version:0,device_version:1,status:'unknown',reasons:[],events:[]}};
  else if(path.startsWith('/api/v1/devices/attention'))data={devices:[],counts:{},scanned_devices:1,horizon_days:60,as_of:new Date().toISOString(),day:'2026-10-14'};
  else if(path==='/api/v1/devices/reminders')data={reminders:[],thresholds:[30,7,1],as_of:new Date().toISOString()};
  else if(path==='/api/v1/devices/reminder-mail')data=[];
  else if(path.startsWith('/api/v1/packlists?'))data=window.packs;
  else if(path==='/api/v1/packlists'&&options.method==='POST'){const body=JSON.parse(options.body);data={...body,id:'material-pack',version:1};window.packs=[data];window.packPublished=body;}
  else if(path==='/api/v1/packlists/material-pack/readiness')data={status:'ready',data_as_of:new Date().toISOString(),current_version:true,basis_hash:'synthetic',packlist:window.packs[0],reasons:[],items:[]};
  else if(path==='/api/v1/packlists/material-pack/checks')data=[];
  else if(path==='/api/v1/recalls')data=[];
  else throw Error('Unexpected request '+path);
  if(actualPath.startsWith('/api/v1/inventory/devices?')){const query=new URL(actualPath,location.origin).searchParams;const q=(query.get('q')||'').toLowerCase();const items=data.filter(item=>[item.inventory_number,item.name,item.manufacturer,item.serial_or_lot].join(' ').toLowerCase().includes(q));data={items,total:items.length,equipment:0,limit:50,offset:0};}
  return new Response(JSON.stringify(data),{headers:{'Content-Type':'application/json','X-Data-As-Of':new Date().toISOString()}});
 };
 `});
 await call('Page.navigate',{url});
 await until("visible('#booking') && document.querySelector('#devices-list').children.length===1");
 assert.deepEqual(await evaluate("[...document.querySelector('#device-action').options].map(o=>o.value)"),['details','master_data']);
 assert.equal(await evaluate("document.querySelector('#devices').getClientRects().length>0 && document.querySelector('#material').getClientRects().length===0"),true);
 assert.equal(await evaluate("document.querySelector('#workspace-title').textContent"),'Geräte und Equipment');
 assert.equal(await evaluate("document.querySelector('#metric-devices').textContent"),'1');
 assert.equal(await evaluate("document.querySelector('#device-import').parentElement.id"),'inventory');
 await evaluate("document.querySelector('#inventory-search').value='not-present';document.querySelector('#inventory-search').dispatchEvent(new Event('input'))");
 await until("document.querySelector('#devices-list').children.length===0 && !document.querySelector('#inventory-empty').hidden");
 await evaluate("document.querySelector('#inventory-search').value='Equipment';document.querySelector('#inventory-search').dispatchEvent(new Event('input'))");
 await until("document.querySelector('#devices-list').children.length===1 && document.querySelector('#inventory-empty').hidden");
 await evaluate("document.querySelector('#inventory-search').value='';document.querySelector('#inventory-search').dispatchEvent(new Event('input'))");
 assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true);
 if(process.env.DRK_UI_SCREENSHOTS){const shot=await call('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.DRK_UI_SCREENSHOTS,'drk-desktop.png'),Buffer.from(shot.data,'base64'));}
 for(const width of [320,390,768]){
  await call('Emulation.setDeviceMetricsOverride',{width,height:900,deviceScaleFactor:1,mobile:true});
  assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true);
  await evaluate("document.querySelector('#navigation-toggle').click()");
  assert.equal(await evaluate("document.body.classList.contains('navigation-open') && document.querySelector('#navigation-toggle').getAttribute('aria-expanded')==='true'"),true);
  if(process.env.DRK_UI_SCREENSHOTS&&width===390){const shot=await call('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.DRK_UI_SCREENSHOTS,'drk-mobile-menu.png'),Buffer.from(shot.data,'base64'));}
  await evaluate("document.querySelector('#navigation-backdrop').click()");
  assert.equal(await evaluate("!document.body.classList.contains('navigation-open')"),true);
  if(process.env.DRK_UI_SCREENSHOTS&&width===390){const shot=await call('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.DRK_UI_SCREENSHOTS,'drk-mobile.png'),Buffer.from(shot.data,'base64'));}
 }
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
 await call('Emulation.setEmulatedMedia',{media:'print'});
 assert.equal(await evaluate("document.querySelector('#workspace-header').getClientRects().length===0 && document.querySelector('#workspace-navigation').getClientRects().length===0 && document.querySelector('#label-panel').getClientRects().length>0"),true);
 await call('Emulation.setEmulatedMedia',{media:'screen',features:[{name:'prefers-reduced-motion',value:'reduce'}]});
 await evaluate("document.querySelector('#inventory-import-open').click()");
 assert.equal(await evaluate("document.querySelector('#workspace-title').textContent"),'Geräte importieren');
 await evaluate("document.querySelector('[aria-controls=devices]').click()");

 await evaluate("document.querySelector('[aria-controls=material]').click()");
 assert.equal(await evaluate("document.querySelector('#material').getClientRects().length>0 && document.querySelector('#devices').getClientRects().length===0 && document.querySelector('[aria-controls=material]').getAttribute('aria-current')==='page'"),true);
 assert.equal(await evaluate("document.querySelector('[aria-controls=administration]')===null && document.querySelector('[aria-controls=packlists]')===null"),true);
 await evaluate("document.querySelector('[aria-controls=devices]').click()");

 assert.ok(await evaluate("visible('#device-import') && !visible('#device-attention') && !visible('#document-upload-form') && !visible('#packlists') && !visible('#recalls') && !visible('#usage') && !visible('#pack-release')"));
 assert.equal(await evaluate("document.querySelector('#module-list img')===null"),true);
 assert.equal(await evaluate("requests.some(r=>/documents|attention|reminders|recalls|packlists|usage|product-duties|reprocessing|release/.test(r.path))"),false);
 // Device/equipment ground data remains editable in the core inventory.
 await evaluate("document.querySelector('#device-begin').click()");
 await until("document.querySelector('#device-detail-name').value==='Equipment'");
 await evaluate("document.querySelector('#device-detail-name').value='Synthetic equipment updated';document.querySelector('#device-evidence').value='Synthetic reference';document.querySelector('#device-note').value='Synthetic correction';window.loseDetailReply=true;document.querySelector('#device-control-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("window.detailPosts.length===1 && document.querySelector('#device-basis').textContent.includes('nicht bestätigt')");
 const frozen=await evaluate('window.detailPosts[0]');
 // A module refresh retains the exact unconfirmed write and key.
 await evaluate("window.config=moduleConfig(['inventory','devices']);document.querySelector('#module-reload').click()");
 await until("visible('#devices') && document.querySelector('#devices-status').textContent.includes('Geräte angezeigt') && !document.querySelector('#inventory').classList.contains('modules-unavailable')");
 await evaluate("document.querySelector('#device-control-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("window.detailPosts.length===2");
 assert.deepEqual(await evaluate('window.detailPosts[1]'),frozen);
 // Optional document/board endpoints stay unused while basic device lifecycle is active.
 assert.equal(await evaluate("requests.some(r=>/documents|attention|reminders/.test(r.path))"),false);
 await evaluate("window.config=moduleConfig(['inventory','packs','recalls']);window.requests=[];document.querySelector('#module-reload').click()");
 await until("visible('#packlists') && visible('#recalls') && document.querySelector('#pack-status').textContent.includes('Sollversionen geladen')");
 await until("requests.some(r=>r.path==='/api/v1/recalls')");
 assert.equal(await evaluate("visible('#pack-add-device') || visible('#physical-device-check')"),false);
 await evaluate("document.querySelector('#pack-edit-start').click()");
 await until("document.querySelectorAll('#pack-items fieldset').length===1");
 await evaluate("document.querySelector('#pack-note').value='Synthetic standalone material pack';document.querySelector('#pack-editor').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("Boolean(window.packPublished)");
 assert.deepEqual(await evaluate('window.packPublished.devices'),[]);
 assert.equal(await evaluate("requests.some(r=>/assessment|device-checks|documents|usage|product-duties|reprocessing|release/.test(r.path))"),false);
 // Existing device requirements cannot be silently discarded while device records are off.
 await evaluate("window.packs[0].devices=[{device_id:'device',accessories:[]}];document.querySelector('#pack-edit-start').click()");
 await until("document.querySelector('#pack-status').textContent.includes('enthält Geräteanforderungen')");
 assert.equal(await evaluate("document.querySelector('#pack-editor button[type=submit]').disabled"),true);
 // Documents can expose own-account manual access independently of professional compliance.
 await evaluate("window.config=moduleConfig(['inventory','devices','documents']);window.requests=[];document.querySelector('#module-reload').click()");
 await until("visible('#manual-context-load') && document.querySelector('#manual-context-device').options.length===1");
 assert.equal(await evaluate("document.querySelector('#manual-context-person').textContent"),'Angemeldetes Konto: test');
 await evaluate("document.querySelector('#manual-context-load').click()");
 await until("visible('#manual-access-form') && document.querySelector('#manual-access-status').textContent==='Zugangsnachweise geladen.'");
 assert.equal(await evaluate("requests.some(r=>r.path.includes('/manual-access?user_id=synthetic-user'))"),true);
 assert.equal(await evaluate("requests.some(r=>/usage|product-duties|end-user-acceptance|duty-fulfilment/.test(r.path))"),false);
 // A missing or contradictory module response closes the interface and stops inventory loads.
 await evaluate("window.failModules=true;window.requests=[];document.querySelector('#module-reload').click()");
 await until("document.querySelector('#module-status').textContent.includes('nicht verfügbar')");
 assert.equal(await evaluate("visible('#booking') || visible('#devices') || visible('#packlists')"),false);
 assert.deepEqual(await evaluate('requests.map(r=>r.path)'),['/api/v1/modules']);
 await evaluate("window.failModules=false;window.config=moduleConfig(['inventory','documents']);document.querySelector('#module-reload').click()");
 await until("document.querySelector('#module-status').textContent.includes('Abhängigkeiten')||document.querySelector('#module-status').textContent.includes('abhängigkeiten')");
 assert.equal(await evaluate("visible('#booking')"),false);
 // An older refresh cannot reinstate modules after logout and a new login with the same account.
 await evaluate("window.config=moduleConfig(Object.keys(moduleDependencies));window.holdModules=true;document.querySelector('#module-reload').click()");
 await until('Boolean(window.releaseModules)');
 await evaluate("document.querySelector('#logout').click()");
 await until("!document.querySelector('#login').hidden");
 await evaluate("window.config=moduleConfig(['inventory']);window.userName='same-account-new-session';restore()");
 await until("visible('#booking') && document.querySelector('#account').textContent.includes('same-account-new-session')");
 await evaluate('window.releaseModules()');
 await new Promise(resolve=>setTimeout(resolve,100));
 assert.equal(await evaluate("visible('#packlists') || visible('#pack-release') || visible('#recalls') || visible('#usage')"),false);
 assert.equal(await evaluate("moduleConfiguration.enabled_modules.join(',')"),'inventory');
 // Role-specific reloads cannot expose a module hidden by configuration.
 await evaluate("document.querySelector('#packlists').hidden=false;document.querySelector('#usage').hidden=false;document.querySelector('#document-upload-form').hidden=false;document.querySelector('#pack-release').hidden=false");
 assert.equal(await evaluate("visible('#packlists') || visible('#usage') || visible('#document-upload-form') || visible('#pack-release')"),false);
 const before=await evaluate('window.requests.length');
 await evaluate("document.querySelector('#release-load').click();document.querySelector('#usage-load').click();document.querySelector('#pack-refresh').click()");
 assert.equal(await evaluate('window.requests.length'),before);

 // New object workspace: minimal creation and exact retries over the same APIs.
 await evaluate(`window.workspaceBaseFetch=window.fetch;window.creationPosts=[];window.creationReplies=new Map();window.photoPosts=[];window.photos=[];window.photoReplies=new Map();
 window.fetch=async(input,options={})=>{const path=String(input);let data;
  if(path==='/api/v1/devices'&&options.method==='POST'){const posted={body:options.body,key:options.headers['Idempotency-Key']};creationPosts.push(posted);if(!creationReplies.has(posted.key))creationReplies.set(posted.key,{id:'created-device',...JSON.parse(posted.body),version:0,active_blocks:[],instructions:[],incidents:[]});data=creationReplies.get(posted.key);if(window.loseCreation){window.loseCreation=false;throw Error('Lost create reply');}}
  else if(path==='/api/v1/devices/created-device/book')data=[...creationReplies.values()][0];
  else if(path==='/api/v1/devices/created-device/events'||path==='/api/v1/devices/created-device/documents')data=[];
  else if(path==='/api/v1/me'&&window.objectViewer)data={id:'synthetic-user',username:'readonly-user',csrf_token:'synthetic',administrator:false,rights:[{site_id:'site',role:'viewer'}]};
  else if(path==='/api/v1/devices/created-device/photo'&&options.method==='POST'){const posted={body:options.body,key:options.headers['Idempotency-Key']};photoPosts.push(posted);if(!photoReplies.has(posted.key)){const body=JSON.parse(posted.body);data={id:'photo-event',version:photos.length+1,action:body.action,note:body.note,created_at:'2026-10-16T00:00:00Z'};photoReplies.set(posted.key,data);photos.unshift(data);}else data=photoReplies.get(posted.key);if(window.losePhoto){window.losePhoto=false;throw Error('Lost photo reply');}}
  else if(path==='/api/v1/devices/created-device/photo')data=photos;
  else return workspaceBaseFetch(input,options);
  return new Response(JSON.stringify(data),{status:200,headers:{'Content-Type':'application/json'}});
 };
 document.querySelector('#inventory-new-open').click();document.querySelector('#new-device-number').value='invalid#number';window.invalidInventoryNumberRejected=!document.querySelector('#new-device-number').checkValidity();document.querySelector('#new-device-number').value='SINGLE-1';document.querySelector('#new-device-name').value='<img src=x> Tragetasche';document.querySelector('#new-device-kind').value='equipment';window.loseCreation=true;document.querySelector('#device-new-form').requestSubmit();`);
 assert.equal(await evaluate("window.invalidInventoryNumberRejected"),true);
 await until("document.querySelector('#device-new-status').textContent.includes('nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#new-device-name').disabled"),true);
 await evaluate("document.querySelector('#device-new-submit').click()");
 await until("selectedInventoryRecord?.id==='created-device' && document.querySelector('#object-name').textContent.includes('Tragetasche')");
 assert.deepEqual(await evaluate('creationPosts[0]'),await evaluate('creationPosts[1]'));
 assert.equal(await evaluate("document.querySelector('#object-name img')===null"),true);
 assert.equal(await evaluate("document.querySelector('#object-tab-controls')===null && document.querySelector('#object-tab-documents')===null"),true);
 if(process.env.DRK_UI_SCREENSHOTS){const shot=await call('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.DRK_UI_SCREENSHOTS,'drk-object.png'),Buffer.from(shot.data,'base64'));}
 await call('Emulation.setDeviceMetricsOverride',{width:390,height:900,deviceScaleFactor:1,mobile:true});
 assert.equal(await evaluate('document.documentElement.scrollWidth<=innerWidth'),true);
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
 await evaluate("document.querySelector('#object-tab-basics').click()");
 await until("deviceDraft?.id==='created-device' && !document.querySelector('#device-detail-name').disabled");
 assert.equal(await evaluate("document.querySelector('#device-detail-name').value"),'<img src=x> Tragetasche');
 await evaluate("document.querySelector('#object-tab-overview').click();document.querySelector('#object-photo-note').value='Synthetic photo';const data=new DataTransfer();data.items.add(new File(['synthetic png'], 'photo.png',{type:'image/png'}));document.querySelector('#object-photo-file').files=data.files;window.losePhoto=true;document.querySelector('#object-photo-form').requestSubmit()");
 await until("document.querySelector('#object-photo-status').textContent.includes('nicht bestätigt')");
 assert.equal(await evaluate("document.querySelector('#object-photo-note').disabled"),true);
 await evaluate("document.querySelector('#logout').click()");await until("!document.querySelector('#login').hidden");
 await evaluate("restore()");await until("user?.id==='synthetic-user' && moduleConfiguration?.enabled_modules.includes('inventory') && !document.querySelector('#inventory').hidden");
 await evaluate("openInventoryRecord('created-device')");await until("selectedInventoryRecord?.id==='created-device' && !document.querySelector('#object-photo-save').disabled");

 await evaluate("document.querySelector('#object-photo-save').click()");
 await until("document.querySelector('#object-photo-status').textContent.includes('gespeichert')");
 assert.deepEqual(await evaluate('photoPosts[0]'),await evaluate('photoPosts[1]'));
 assert.equal(await evaluate("document.querySelector('#object-photo-history').children.length"),1);
 await evaluate("document.querySelector('#object-photo-note').value='Synthetic removal';document.querySelector('#object-photo-remove').click()");
 await until("currentPhotoVersion===2 && document.querySelector('#object-photo').hidden");

 await evaluate("window.config=moduleConfig(['inventory','devices','documents']);document.querySelector('#module-reload').click()");
 await until("moduleConfiguration?.enabled_modules.includes('documents') && document.querySelector('#object-tab-documents')");
 await evaluate("document.querySelector('#object-tab-documents').click()");
 await until("document.querySelector('#document-upload-form').parentElement.id==='object-panel-documents' && document.querySelector('#device-choice').value==='created-device'");
 assert.equal(await evaluate("document.querySelector('#object-tab-users')===null"),true);
 await evaluate("window.config=moduleConfig(['inventory','devices','documents','compliance','reprocessing']);document.querySelector('#module-reload').click()");
 await until("document.querySelector('#object-tab-users') && document.querySelector('#object-tab-cycles')");
 await until("['usage-device','duty-device','end-user-device','cycle-device','fulfil-device'].every(id=>document.getElementById(id).value==='created-device')");

 await evaluate("document.querySelector('#logout').click()");await until("!document.querySelector('#login').hidden");
 await evaluate("window.objectViewer=true;restore()");await until("user?.username==='readonly-user' && moduleConfiguration?.enabled_modules.includes('documents')");
 await evaluate("openInventoryRecord('created-device')");await until("selectedInventoryRecord?.id==='created-device' && document.querySelector('#object-photo-history').children.length===2");
 assert.equal(await evaluate("document.querySelector('#inventory-new-open').hidden && document.querySelector('#object-photo-form').hidden && document.querySelector('#device-control-form').hidden"),true);
 await evaluate("document.querySelector('#object-back').click()");
 assert.equal(await evaluate("activeSection"),'devices');
 console.log('Module browser UI passed: desktop/mobile layout without horizontal overflow, server inventory search, minimal single creation/replay, object tabs and photo retry/removal/history; core material/equipment inventory and edits, retained exact write retry, independent material packs and recalls, own-account document access without compliance, no discarded device requirements, no inactive endpoint loads, safe module text, missing/invalid config closed, same-account stale config rejected and role loaders cannot reveal inactive functions (simulated API).');
} finally {
 socket?.close();browser.kill('SIGTERM');await new Promise(resolve=>{if(browser.exitCode!==null)resolve();else browser.once('exit',resolve);});await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100});
}
