// Browser regression check for module scope. The API is simulated;
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
 const deps={inventory:[],devices:[],documents:['devices'],compliance:['devices','documents'],reprocessing:['compliance'],packs:[],release:['packs','devices','documents','compliance','reprocessing','recalls'],recalls:[],reminders:['devices']};
 const names={inventory:'Inventarisierung',devices:'Geräteakten',documents:'Dokumente',compliance:'Produktpflichten',reprocessing:'Aufbereitung',packs:'Packlisten',release:'Einsatzfreigabe',recalls:'Rückrufe',reminders:'Erinnerungen'};
 window.config=(enabled,revision=1)=>({revision,editable:true,enabled_modules:enabled,modules:Object.entries(deps).map(([id,depends_on])=>({id,name:names[id],required:id==='inventory',enabled:enabled.includes(id),depends_on}))});
 window.moduleData=config(['inventory']);window.setupPending=true;window.noSetupCSRF=false;window.failStatus=true;window.failedChecks=false;window.requests=[];window.completePosts=[];window.modulePosts=[];window.moduleReplies=new Map();window.logged=false;window.admin=true;window.currentUsername='setup-admin';window.events=[];
 window.visible=selector=>{const node=document.querySelector(selector);return Boolean(node&&!node.hidden&&node.getClientRects().length);};
 window.fetch=async(input,options={})=>{
 const path=typeof input==='string'?input:input.url;requests.push({path,method:options.method||'GET'});let data;
 const reply=(data,status=200)=>new Response(JSON.stringify(data),{status,headers:{'Content-Type':'application/json'}});
 if(path==='/api/v1/setup/status'){
  if(failStatus)return reply({message:'Synthetic startup failure'},503);
  if(!setupPending)return reply({pending:false});
  return reply({pending:true,csrf_token:noSetupCSRF?'':'setup-nonce',revision:moduleData.revision,modules:moduleData.modules});
 }
 if(path==='/api/v1/setup/checks'){
  const body=JSON.parse(options.body);if(options.headers['X-CSRF-Token']!=='setup-nonce'||body.setup_code!=='synthetic-setup-code')return reply({message:'Einrichtungscode ungültig.'},403);
  return reply({revision:moduleData.revision,can_complete:!failedChecks,checks:[{id:'db',label:'Datenbank',status:failedChecks?'error':'ok',required:true,message:failedChecks?'Schreibzugriff fehlt.':'Integrität und Schreibzugriff geprüft.'},{id:'smtp',label:'Mailserver',status:'warning',required:false,message:'Nicht konfiguriert; Versand nicht getestet.'}]});
 }
 if(path==='/api/v1/setup/complete'){
  completePosts.push({body:options.body,csrf:options.headers['X-CSRF-Token']});if(window.completeConflict){window.completeConflict=false;moduleData=config(moduleData.enabled_modules,moduleData.revision+1);return reply({message:'Einrichtungsstand geändert'},409);}setupPending=false;const body=JSON.parse(options.body);moduleData=config(body.enabled_modules,moduleData.revision+1);currentUsername=body.username;if(window.loseComplete){window.loseComplete=false;throw Error('Lost completion response');}return reply({completed:true},201);
 }
 if(path==='/api/v1/me'){if(!logged)return reply({message:'Anmeldung erforderlich'},401);return reply({id:'same-user',username:currentUsername,administrator:admin,csrf_token:'session-nonce',rights:[{site_id:'site',role:'material_manager'},{site_id:'site',role:'device_manager'}]});}
 if(path==='/api/v1/auth/login'){logged=true;return reply({csrf_token:'session-nonce'});}
 if(path==='/api/v1/auth/logout'){logged=false;return reply({});}
 if(path==='/api/v1/modules')return reply(structuredClone(moduleData));
 if(path==='/api/v1/admin/modules'){
  if(options.method==='POST'){
   const request={key:options.headers['Idempotency-Key'],csrf:options.headers['X-CSRF-Token'],body:options.body};modulePosts.push(request);
   if(window.holdModulePost){window.holdModulePost=false;await new Promise(resolve=>window.releaseModulePost=resolve);}
   if(window.moduleConflict){window.moduleConflict=false;return reply({message:'Funktionsstand geändert.'},409);}
   if(moduleReplies.has(request.key))return reply(moduleReplies.get(request.key));
   const body=JSON.parse(request.body);moduleData=config(body.enabled_modules,moduleData.revision+1);const result={revision:moduleData.revision,enabled_modules:moduleData.enabled_modules};moduleReplies.set(request.key,result);events.push({revision:moduleData.revision,note:body.note,created_at:new Date().toISOString()});if(window.loseModuleReply){window.loseModuleReply=false;throw Error('Lost module reply');}return reply(result);
  }
  if(window.holdAdminGet){window.holdAdminGet=false;const result={...structuredClone(moduleData),history:structuredClone(events)};await new Promise(resolve=>window.releaseAdminGet=resolve);return reply(result);}
  return reply({...structuredClone(moduleData),history:structuredClone(events)});
 }
 if(path==='/api/v1/admin')return reply({objects:[{id:'operator',kind:'operator',name:'Organisation',version:1,rights:[]},{id:'site',kind:'site',name:'Betrieb',parent_id:'operator',version:1,rights:[]},{id:'location',kind:'location',name:'Lager',parent_id:'site',version:1,rights:[]},{id:'same-user',kind:'user',name:'Administrator',active:true,administrator:true,version:1,rights:[]}],events:[],more_events:false});
 if(path==='/api/v1/locations')return reply([{id:'location',name:'Lager',site_id:'site',site:'Betrieb',operator:'Organisation'}]);
 if(path==='/api/v1/stock/catalog'||path==='/api/v1/stock/balances'||path.startsWith('/api/v1/stock/movements')||path==='/api/v1/product-identifiers'||path==='/api/v1/devices')return reply([]);
 if(path.startsWith('/api/v1/packlists?'))return reply([]);
 throw Error('Unexpected synthetic API '+path);
 };
 `});
 await call('Page.navigate',{url});
 await until("visible('#startup-retry')");
 assert.equal(await evaluate("visible('#login') || visible('#inventory') || requests.some(r=>r.path==='/api/v1/me')"),false);
 await evaluate("window.failStatus=false;window.noSetupCSRF=true;document.querySelector('#startup-retry').click()");
 await until("document.querySelector('#startup-status').textContent.includes('Einrichtungsschutz')");
 assert.equal(await evaluate("visible('#setup') || visible('#login')"),false);
 await evaluate("window.noSetupCSRF=false;document.querySelector('#startup-retry').click()");
 await until("visible('#setup') && !document.querySelector('#setup-next').disabled");
 assert.equal(await evaluate("selectedModules(document.querySelector('#setup-modules')).join(',')"),'inventory');
 assert.equal(await evaluate("document.querySelector('#setup-modules input[data-module=inventory]').disabled"),true);
 await evaluate("document.querySelector('#setup-code').value='wrong';document.querySelector('#setup-next').click()");
 await until("document.querySelector('#setup-status').textContent.includes('ungültig')");
 await evaluate("document.querySelector('#setup-code').value='synthetic-setup-code';window.failedChecks=true;document.querySelector('#setup-next').click()");
 await until("visible('#setup-check-results') && document.querySelector('#setup-status').textContent.includes('fehlgeschlagen')");
 assert.equal(await evaluate("setupStep"),0);
 assert.equal(await evaluate("document.querySelector('#setup-checks').textContent.includes('Versand nicht getestet')"),true);
 await evaluate("window.failedChecks=false;document.querySelector('#setup-next').click()");
 await until("setupStep===1 && !setupBusy");
 await evaluate("document.querySelector('#setup-username').value='setup-admin';document.querySelector('#setup-password').value='synthetic-password-2026';document.querySelector('#setup-password-confirm').value='different';document.querySelector('#setup-operator').value='<svg onload=alert(1)>';document.querySelector('#setup-site').value='Betrieb';document.querySelector('#setup-location').value='Lager';document.querySelector('#setup-next').click()");
 await until("document.querySelector('#setup-status').textContent.includes('nicht überein')");
 await evaluate("document.querySelector('#setup-password-confirm').value='synthetic-password-2026';document.querySelector('#setup-next').click()");
 await until("setupStep===2 && !setupBusy");
 await evaluate("document.querySelector('#setup-modules input[data-module=documents]').click()");
 assert.deepEqual(await evaluate("selectedModules(document.querySelector('#setup-modules'))"),['inventory','devices','documents']);
 await evaluate("document.querySelector('#setup-modules input[data-module=devices]').click()");
 assert.equal(await evaluate("document.querySelector('#setup-modules input[data-module=devices]').checked"),true);
 assert.equal(await evaluate("document.querySelector('#setup-status').textContent.includes('Zuerst Dokumente')"),true);
 await evaluate("document.querySelector('#setup-modules input[data-module=documents]').click();document.querySelector('#setup-next').click()");
 await until("setupStep===3 && !setupBusy");
 assert.equal(await evaluate("document.querySelector('#setup-summary svg')===null && document.querySelector('#setup-summary').textContent.includes('<svg')"),true);
 await evaluate("document.querySelector('#setup-next').click()");
 await until("document.querySelector('#setup-status').textContent.includes('ausdrücklich bestätigen')");
 assert.equal(await evaluate('completePosts.length'),0);
 await evaluate("document.querySelector('#setup-confirm').checked=true;window.completeConflict=true;document.querySelector('#setup-next').click()");
 await until("setupStep===2 && !setupBusy && document.querySelector('#setup-status').textContent.includes('Einrichtungsstand geändert')");
 assert.equal(await evaluate('setupCompletion'),null);
 await evaluate("document.querySelector('#setup-next').click()");
 await until("setupStep===3 && !setupBusy");
 assert.equal(await evaluate("document.querySelector('#setup-confirm').checked"),false);
 await evaluate("document.querySelector('#setup-confirm').checked=true;window.loseComplete=true;document.querySelector('#setup-next').click()");
 await until("visible('#login') && !visible('#setup')");
 assert.equal(await evaluate('completePosts.length'),2);
 assert.equal(await evaluate('JSON.parse(completePosts[1].body).expected_revision'),2);
 assert.equal(await evaluate("completePosts[0].csrf"),'setup-nonce');
 assert.equal(await evaluate("document.querySelector('#setup-code').value+document.querySelector('#setup-password').value+document.querySelector('#setup-password-confirm').value"),'');
 assert.equal(await evaluate("requests.some(r=>r.path==='/api/v1/auth/login'||r.path==='/api/v1/me')"),false);
 assert.equal(await evaluate("document.querySelector('#login [name=username]').value"),'setup-admin');
 assert.equal(await evaluate('localStorage.length+sessionStorage.length'),0);
 // The account is not logged in implicitly after setup.
 await evaluate("document.querySelector('#login [name=password]').value='synthetic-password-2026';document.querySelector('#login').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("visible('#admin-modules') && document.querySelector('#admin-modules-status').textContent.includes('geladen')");
 assert.equal(await evaluate("document.querySelector('#admin-modules-choices input[data-module=inventory]').disabled"),true);
 await evaluate("document.querySelector('#admin-modules-choices input[data-module=packs]').click();document.querySelector('#admin-modules-note').value='Synthetic lost reply';window.loseModuleReply=true;document.querySelector('#admin-modules-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("document.querySelector('#admin-modules-status').textContent.includes('nicht bestätigt')");
 const frozen=await evaluate('modulePosts[0]');
 assert.equal(frozen.csrf,'session-nonce');assert.deepEqual(JSON.parse(frozen.body).enabled_modules,['inventory','devices','packs']);
 assert.equal(await evaluate("document.querySelector('#admin-modules-note').disabled && document.querySelector('#admin-modules-choices input[data-module=devices]').disabled"),true);
 await evaluate("document.querySelector('#admin-modules-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("document.querySelector('#admin-modules-status').textContent.includes('Änderung bestätigt')");
 assert.deepEqual(await evaluate('modulePosts[1]'),frozen);
 assert.equal(await evaluate("moduleConfiguration.enabled_modules.includes('packs') && visible('#packlists')"),true);
 assert.equal(await evaluate("document.querySelector('#admin-modules-history').textContent.includes('Synthetic lost reply')"),true);
 // A conflict requires a new snapshot before another mutation.
 await evaluate("window.moduleConflict=true;document.querySelector('#admin-modules-note').value='Synthetic conflict';document.querySelector('#admin-modules-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("document.querySelector('#admin-modules-status').textContent.includes('Funktionsstand geändert')");
 assert.equal(await evaluate("document.querySelector('#admin-modules-submit').disabled"),true);
 await evaluate("document.querySelector('#admin-modules-reload').click()");
 await until("document.querySelector('#admin-modules-status').textContent.includes('geladen') && !document.querySelector('#admin-modules-submit').disabled");
 // A late reply is owned by its original login session even when the same account signs in again.
 await evaluate("window.holdAdminGet=true;document.querySelector('#admin-modules-reload').click()");
 await until('Boolean(window.releaseAdminGet)');
 await evaluate("document.querySelector('#logout').click()");
 await until("visible('#login')");
 await evaluate("window.admin=false;window.logged=true;restore()");
 await until("visible('#booking')");
 await evaluate('window.releaseAdminGet()');
 await new Promise(resolve=>setTimeout(resolve,100));
 assert.equal(await evaluate("visible('#administration') || visible('#admin-modules')"),false);
 assert.equal(await evaluate("document.querySelector('#admin-modules-choices').children.length"),0);
 // A previously issued mutation cannot repaint a newly authenticated session.
 await evaluate("signedOut('Synthetic relogin');window.admin=true;window.logged=true;restore()");
 await until("visible('#admin-modules') && document.querySelector('#admin-modules-status').textContent.includes('geladen')");
 await evaluate("window.holdModulePost=true;document.querySelector('#admin-modules-note').value='Synthetic held post';document.querySelector('#admin-modules-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until('Boolean(window.releaseModulePost)');
 await evaluate("document.querySelector('#logout').click()");
 await until("visible('#login')");
 await evaluate("window.logged=true;restore()");
 await until("visible('#admin-modules') && document.querySelector('#admin-modules-status').textContent.includes('Unbestätigte Änderung')");
 await evaluate('window.releaseModulePost()');
 await new Promise(resolve=>setTimeout(resolve,100));
 assert.equal(await evaluate("document.querySelector('#admin-modules-status').textContent.includes('Unbestätigte Änderung')"),true);
 await evaluate("document.querySelector('#admin-modules-form').dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}))");
 await until("document.querySelector('#admin-modules-status').textContent.includes('Änderung bestätigt')");
 console.log('Web setup/module administration UI passed: startup availability and nonce gate, ownership code, failed environment checks, explicit account/password confirmation, module dependency selection, safe summary, lost-completion recovery without implicit login, credential clearing, administrator-only settings, exact frozen mutation retry, revision conflict, refreshed workspace and stale login replies (simulated API).');
} finally {
 socket?.close();browser.kill('SIGTERM');await new Promise(resolve=>{if(browser.exitCode!==null)resolve();else browser.once('exit',resolve);});await new Promise(resolve=>server.close(resolve));await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100});
}
