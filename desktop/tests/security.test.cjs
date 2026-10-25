const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const {validate,methods}=require('../app/validation.js');
test('TP041 privileged methods have strict public schemas',()=>{for(const method of methods){assert.throws(()=>validate(method,{password:'synthetic forbidden IPC credential'}));assert.throws(()=>validate(method,[]));assert.throws(()=>validate(method,null))}for(const method of ['signBytes','getMnemonic','exportPrivateKey','eval','readFile','backup','restore','revealPhrase','exportPhrase','activity','toString','__proto__'])assert.throws(()=>validate(method,{}));validate('prepare',{accountId:'a',asset:'n:native',recipient:'public',amount:'1.001',memo:''});assert.throws(()=>validate('prepare',{accountId:'a',asset:'n:native',recipient:'p',amount:1,memo:''}));assert.throws(()=>validate('addAccount',{index:0.5}));validate('addAccount',{index:999999});assert.throws(()=>validate('addAccount',{index:1000000}));assert.throws(()=>validate('create',{words:15}));validate('create',{words:12});});
test('TP033 TP041 production surface no secret inputs or remote assets',()=>{const main=fs.readFileSync('src/main.ts','utf8'),preload=fs.readFileSync('src/preload.ts','utf8'),html=fs.readFileSync('ui/index.html','utf8');for(const config of ['contextIsolation:true','sandbox:true','nodeIntegration:false','webSecurity:true','devTools:false','app.enableSandbox()'])assert.ok(main.includes(config));assert.ok(html.includes("connect-src 'none'"));assert.ok(!/type="password"|https?:\/\/|<webview/i.test(html));assert.ok(!/process\.env|readFile|signBytes|secret|password\s*:/i.test(preload));assert.ok(!main.includes('--no-sandbox'));assert.ok(!main.includes('spawn('));assert.ok(main.includes('LOCALWALLET_INHERITED_CORE'));});

// Drive the production main process through real framed Node streams. These
// Electron doubles establish notice routing/authorization boundaries, not an
// installed display-server or Chromium sandbox claim.
const vm=require('node:vm');const path=require('node:path');const crypto=require('node:crypto');
const {EventEmitter}=require('node:events');const {PassThrough,Writable}=require('node:stream');
const nextTurn=()=>new Promise(resolve=>setImmediate(resolve));
function noticeHarness(){
 const stdout=new PassThrough(),outbound=[],windows=[],publicEvents=[],partitions=[],validated=[];let buffered=Buffer.alloc(0),timerID=0;
 const stdin=new Writable({write(chunk,_encoding,done){buffered=Buffer.concat([buffered,chunk]);while(buffered.length>=4){const n=buffered.readUInt32BE();if(buffered.length<n+4)break;outbound.push(JSON.parse(buffered.subarray(4,n+4).toString()));buffered=buffered.subarray(n+4)}done()}});
 const timers=new Map();const app=new EventEmitter();Object.assign(app,{isPackaged:false,enableSandbox(){},disableHardwareAcceleration(){},commandLine:{appendSwitch(){}},whenReady:()=>Promise.resolve(),quit(){},getVersion:()=> '0.1.0-synthetic'});
 const ipcMain=new EventEmitter();ipcMain.handlers=new Map();ipcMain.handle=(name,handler)=>ipcMain.handlers.set(name,handler);const powerMonitor=new EventEmitter();
 class BrowserWindow extends EventEmitter{
  constructor(config){super();this.config=config;this.webContents=new EventEmitter();Object.assign(this.webContents,{mainFrame:{url:'wallet://app/'},isDestroyed:()=>!!this.destroyed,send:(...args)=>publicEvents.push(args),setWindowOpenHandler(){}});windows.push(this)}
  isDestroyed(){return !!this.destroyed}destroy(){this.destroyed=true;this.emit('closed')}setMenu(){}setTitle(){}show(){}async loadURL(url){this.webContents.mainFrame.url=url}
 }
 const session={fromPartition(name){partitions.push(name);const value=new EventEmitter();Object.assign(value,{setPermissionRequestHandler(){},setPermissionCheckHandler(){},webRequest:{onBeforeRequest(){}},protocol:{handle(){}},clearStorageData:()=>Promise.resolve()});return value}};
 const electron={app,BrowserWindow,ipcMain,powerMonitor,session,protocol:{registerSchemesAsPrivileged(){}},net:{fetch:()=>Promise.resolve(new Response('local asset'))},dialog:{},clipboard:{writeText(){throw Error('A notice must never copy data')}}};
 const image=Buffer.from('public synthetic executable');const digest=crypto.createHash('sha256').update(image).digest('hex');
 const fileAPI={readFileSync(filename){return filename.endsWith('manifest.json')?JSON.stringify({'wallet-core':digest}):image},createWriteStream:()=>stdin,createReadStream:()=>stdout};
 const prompts=require('../app/prompts.js');const directory=path.resolve(__dirname,'../app');
 const context=vm.createContext({exports:{},Buffer,URL,Response,process:{env:{LOCALWALLET_INHERITED_CORE:'1'},getuid:()=>1000},__dirname:directory,setTimeout(fn,ms){const id=++timerID;timers.set(id,{fn,ms});return id},clearTimeout:id=>timers.delete(id),setInterval:()=>1,clearInterval(){},require(name){if(name==='electron')return electron;if(name==='node:fs')return fileAPI;if(name==='node:path')return path;if(name==='node:crypto')return crypto;if(name==='./prompts')return {...prompts,validatePrompt(value){const result=prompts.validatePrompt(value);validated.push(result);return result}};if(name==='./codec')return require('../app/codec.js');if(name==='./validation')return require('../app/validation.js');throw Error('Unexpected capability '+name)}});
 new vm.Script(fs.readFileSync(path.join(directory,'main.js'),'utf8'),{filename:path.join(directory,'main.js')}).runInContext(context);
 const event=(window=windows[0])=>({sender:window.webContents,senderFrame:window.webContents.mainFrame});
 function response(request,body={},extra={}){const frame=Buffer.from(JSON.stringify({version:1,id:request.id,session:request.session,method:request.method,body,...extra}));const header=Buffer.alloc(4);header.writeUInt32BE(frame.length);stdout.write(Buffer.concat([header,frame]))}
 return {outbound,windows,publicEvents,partitions,validated,timers,stdin,stdout,powerMonitor,ready:async()=>{await nextTurn();await nextTurn()},call:(method,body={})=>ipcMain.handlers.get('wallet:request')(event(),method,body),response,prompt:(request,prompt,extra={})=>response(request,prompt,{method:'prompt',...extra}),promptCall:(method,body)=>Promise.resolve().then(()=>ipcMain.handlers.get('wallet:prompt:'+method)(event(windows.at(-1)),body)),cleanup(){app.emit('before-quit');stdout.destroy();stdin.destroy()}};
}
const recoveryNotice={kind:'notice',title:'Recovery phrase not confirmed',summary:'The recorded words did not match. Record the complete phrase in order, then try again. Your wallet remains unavailable for payments until this is complete.'};
const noticeID='a'.repeat(32);
function publicResult(value){return JSON.parse(JSON.stringify(value))}

test('TP033 ordinary Go notices negatively dismiss only their one-use ID, open no window and retain safe final failure categories',async t=>{
 const h=noticeHarness();t.after(h.cleanup);await h.ready();
 for(const code of ['PASSWORD_POLICY','INPUT_TOO_LONG','PASSWORD_MISMATCH','INVALID_MNEMONIC','REQUEST_CANCELLED','REQUEST_FAILED_STATE_UNKNOWN','PRIVATE_FAILURE_CANARY']){
  const result=h.call('create',{words:12}),request=h.outbound.at(-1),before=h.outbound.length;
  h.prompt(request,{id:noticeID,kind:'notice',title:'Wallet request failed',summary:'<script>PRIVATE_NOTICE_CANARY</script>'});
  assert.equal(h.windows.length,1);assert.equal(h.partitions.length,1);assert.equal(h.outbound.length,before+1);
  assert.deepEqual(h.outbound.at(-1),{version:1,id:request.id,session:request.session,method:'promptReply',body:{promptId:noticeID,accepted:false}});
  assert.equal(h.validated.at(-1).title,'');assert.equal(h.validated.at(-1).id,'');assert.equal(h.validated.at(-1).summary,undefined);
  h.response(request,null,{error:code});assert.deepEqual(publicResult(await result),{ok:false,error:code==='PRIVATE_FAILURE_CANARY'?'REQUEST_FAILED_STATE_UNKNOWN':code});
 }
 assert.equal(h.outbound.filter(frame=>frame.method==='promptReply'&&frame.body.accepted===true).length,0);assert.deepEqual(h.publicEvents,[]);assert.ok(!JSON.stringify(h.outbound).includes('PRIVATE_'));assert.ok(!JSON.stringify(h.outbound).includes('<script>'));
});

test('TP033 the exact reviewed recovery-word notice reports a mismatch on generic denial or a successful pending-root result',async t=>{
 for(const method of ['create','confirmRecovery'])for(const ending of ['REQUEST_DENIED','REQUEST_FAILED_STATE_UNKNOWN','REQUEST_CANCELLED','PASSWORD_POLICY','success']){
  const h=noticeHarness();t.after(h.cleanup);await h.ready();const operation=h.call(method,method==='create'?{words:12}:{}),request=h.outbound[0];h.prompt(request,{id:noticeID,...recoveryNotice});assert.equal(h.windows.length,1);assert.deepEqual(h.outbound.at(-1).body,{promptId:noticeID,accepted:false});
  if(ending==='success')h.response(request,{recoveryConfirmed:false});else h.response(request,null,{error:ending});
  assert.deepEqual(publicResult(await operation),{ok:false,error:['REQUEST_DENIED','REQUEST_FAILED_STATE_UNKNOWN','success'].includes(ending)?'RECOVERY_WORDS_MISMATCH':ending});assert.deepEqual(h.publicEvents,[]);
 }
 for(const mutation of [{method:'unlock'},{title:recoveryNotice.title+' '},{title:'Wallet request failed'},{summary:recoveryNotice.summary+' '},{summary:'PRIVATE_RECOVERY_WORD_CANARY'},{summary:''}]){
  const h=noticeHarness();t.after(h.cleanup);await h.ready();const method=mutation.method||'confirmRecovery',operation=h.call(method),request=h.outbound[0];const {method:ignored,...change}=mutation;h.prompt(request,{id:noticeID,...recoveryNotice,...change});h.response(request,null,{error:'REQUEST_DENIED'});assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_FAILED_STATE_UNKNOWN'});assert.equal(h.windows.length,1);assert.deepEqual(h.publicEvents,[]);
 }
 for(const variant of ['no-notice','unknown-notice','wrong-method','changed-title','changed-summary']){
  const h=noticeHarness();t.after(h.cleanup);await h.ready();const method=variant==='wrong-method'?'unlock':'create',operation=h.call(method,method==='create'?{words:12}:{}),request=h.outbound[0];
  if(variant!=='no-notice')h.prompt(request,{id:noticeID,...recoveryNotice,...(variant==='unknown-notice'?{title:'Wallet request failed',summary:'PRIVATE_UNREVIEWED_CANARY'}:variant==='changed-title'?{title:recoveryNotice.title+' '}:variant==='changed-summary'?{summary:recoveryNotice.summary+' '}: {})});
  h.response(request,{recoveryConfirmed:false,publicAccount:'PUBLIC_ACCOUNT'});assert.deepEqual(publicResult(await operation),{ok:true,body:{recoveryConfirmed:false,publicAccount:'PUBLIC_ACCOUNT'}});assert.equal(h.windows.length,1);assert.deepEqual(h.publicEvents,[]);
 }
 const h=noticeHarness();t.after(h.cleanup);await h.ready();const operation=h.call('confirmRecovery'),request=h.outbound[0];h.response(request,null,{error:'RECOVERY_WORDS_MISMATCH'});assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_FAILED_STATE_UNKNOWN'});
});

test('TP042 notice dismissal never answers password, phrase, receive or spend approval prompts',async t=>{
 for(const [method,body,prompt] of [
  ['unlock',{}, {kind:'input',title:'Unlock wallet',labels:['App password']}],
  ['send',{id:'original-payment'}, {kind:'confirm',title:recoveryNotice.title,summary:recoveryNotice.summary}],
  ['create',{words:12}, {kind:'recovery',title:'Record recovery phrase',phrase:Array(12).fill('abandon').join(' ')}],
  ['receive',{accountId:'public-fixture'}, {kind:'receive',title:'Receive wallet asset',summary:'Public network',address:'PUBLIC_ADDRESS',png:'iVBORw0KGgoAAAA='}]
 ]){
  const h=noticeHarness();t.after(h.cleanup);await h.ready();const operation=h.call(method,body),request=h.outbound[0];h.prompt(request,{id:noticeID,...prompt});await nextTurn();
  assert.equal(h.windows.length,2);assert.equal(h.windows[1].config.modal,true);assert.equal(h.outbound.length,1);assert.equal(h.partitions.length,2);
  await h.promptCall('get');await h.promptCall('reply',{promptId:noticeID,accepted:false});assert.deepEqual(h.outbound[1].body,{promptId:noticeID,accepted:false});h.response(request,null,{error:'REQUEST_CANCELLED'});assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_CANCELLED'});assert.equal(h.windows[1].destroyed,true);assert.deepEqual(h.publicEvents,[]);
 }
});

test('TP041 negative notice dismissal preserves strict schema, pending-operation, session and replay checks',async t=>{
 for(const mode of ['wrong-session','wrong-request','wrong-operation','extra-field','replay','expired','finished']){
  const h=noticeHarness();t.after(h.cleanup);await h.ready();const operation=h.call(mode==='wrong-operation'?'status':'confirmRecovery'),request=h.outbound[0];
  if(mode==='replay')h.prompt(request,{id:noticeID,...recoveryNotice});
  if(mode==='expired'){const timer=h.timers.values().next().value;timer.fn();assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_FAILED_STATE_UNKNOWN'})}
  if(mode==='finished'){h.response(request,{});assert.deepEqual(publicResult(await operation),{ok:true,body:{}})}
  h.prompt(request,{id:noticeID,...recoveryNotice,...(mode==='extra-field'?{values:['PRIVATE_CANARY']}:{} )},mode==='wrong-session'?{session:'b'.repeat(32)}:mode==='wrong-request'?{id:'unrequested'}:{});
  await nextTurn();if(!['expired','finished'].includes(mode))assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_FAILED_STATE_UNKNOWN'});
  assert.equal(h.outbound.filter(frame=>frame.method==='promptReply').length,mode==='replay'?1:0);assert.equal(h.outbound.filter(frame=>frame.method==='promptReply'&&frame.body.accepted===true).length,0);assert.equal(h.windows.length,1);assert.equal(h.stdin.destroyed,true);assert.ok(!JSON.stringify(h.publicEvents).includes('PRIVATE_'));assert.ok(!JSON.stringify(h.outbound).includes('PRIVATE_'));
 }
});

test('TP043 locked or concurrent dedicated prompts cannot be converted into an automatic notice approval',async t=>{
 for(const mode of ['concurrent','locked']){
  const h=noticeHarness();t.after(h.cleanup);await h.ready();const operation=h.call('confirmRecovery'),request=h.outbound[0];h.prompt(request,{id:noticeID,kind:'input',title:'Confirm recovery phrase',labels:['Recorded word 2']});await nextTurn();
  if(mode==='locked'){
   h.powerMonitor.emit('lock-screen');assert.equal(h.windows[1].destroyed,true);assert.deepEqual(h.outbound[1].body,{promptId:noticeID,accepted:false});assert.equal(h.outbound[2].method,'lock');h.response(h.outbound[2],{});h.response(request,null,{error:'REQUEST_CANCELLED'});assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_CANCELLED'});
  }
  h.prompt(request,{id:'b'.repeat(32),...recoveryNotice});await nextTurn();if(mode==='concurrent')assert.deepEqual(publicResult(await operation),{ok:false,error:'REQUEST_FAILED_STATE_UNKNOWN'});
  assert.equal(h.outbound.filter(frame=>frame.method==='promptReply'&&frame.body.accepted===true).length,0);assert.equal(h.windows.length,2);assert.equal(h.stdin.destroyed,true);
 }
});

test('TP033 public preload accepts the fixed recovery guidance code and exposes no prompt text channel',async()=>{
 let api,envelope;const invocations=[];const electron={contextBridge:{exposeInMainWorld(name,value){assert.equal(name,'wallet');api=value}},ipcRenderer:{invoke:async(...args)=>{invocations.push(args);return envelope},on(){},removeListener(){},send(){}}};
 const context=vm.createContext({exports:{},window:{addEventListener(){}},require(name){assert.equal(name,'electron');return electron}});const filename=path.resolve(__dirname,'../app/preload.js');new vm.Script(fs.readFileSync(filename,'utf8'),{filename}).runInContext(context);
 for(const code of ['RECOVERY_WORDS_MISMATCH','PASSWORD_POLICY','REQUEST_CANCELLED']){envelope={ok:false,error:code};await assert.rejects(api.confirmRecovery(),error=>error.message===code)}
 for(const value of [{ok:false,error:'PRIVATE_CANARY'},{ok:false,error:'RECOVERY_WORDS_MISMATCH',summary:'PRIVATE_CANARY'},{ok:false,error:{code:'RECOVERY_WORDS_MISMATCH'}},null]){envelope=value;await assert.rejects(api.confirmRecovery(),error=>error.message==='REQUEST_FAILED_STATE_UNKNOWN')}
 assert.ok(Object.isFrozen(api));for(const key of ['getPrompt','promptReply','onNotice','onPrompt','invoke'])assert.equal(api[key],undefined);assert.ok(invocations.every(call=>call[0]==='wallet:request'&&call[1]==='confirmRecovery'));
});
