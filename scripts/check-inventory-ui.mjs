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
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.text);
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
 window.posted = [];
 window.fetch = async (input,options={}) => {
  const path = typeof input === 'string' ? input : input.url;
  let data;
  if (path === '/api/v1/me') data = {id:'synthetic-user',username:'test',csrf_token:'synthetic-csrf',rights:[{site_id:'site',role:'material_manager'}]};
  else if (path === '/api/v1/locations') data = [{id:'warehouse',name:'Lager',site:'Demo',operator:'Demo',site_id:'site'}];
  else if (path === '/api/v1/stock/catalog') data = [{id:'batch',product:'Demo',unit:'Stück',lot:'DEMO'}];
  else if (path === '/api/v1/stock/balances') data = window.testBalances;
  else if (path.startsWith('/api/v1/stock/movements?')) data = [];
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
 console.log('Browser UI passed: explicit count start, frozen basis across reload, mandatory recount on conflict and valid zero count (simulated API).');
} finally {
 socket?.close();browser.kill('SIGTERM');
 await new Promise(resolve => {if (browser.exitCode !== null) resolve();else browser.once('exit',resolve);});
 server.close();await rm(profile,{recursive:true,force:true});
}
