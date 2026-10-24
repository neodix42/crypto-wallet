import {app,BrowserWindow,ipcMain,protocol,session,powerMonitor,net,dialog,clipboard} from 'electron';
import {ReadStream,WriteStream} from 'node:fs';
import {randomBytes,createHash} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import {validate} from './validation';
import {parseResponse} from './codec';
import {Prompt,PromptReply,validatePrompt,validatePromptReply,clearPrompt} from './prompts';

app.enableSandbox();
// Public wallet views and QR images do not need a hardware rendering pipeline.
// Keep Chromium away from host-specific Mesa/DRI loaders while retaining sandboxing.
app.disableHardwareAcceleration();
app.commandLine.appendSwitch('force-webrtc-ip-handling-policy','disable_non_proxied_udp');
protocol.registerSchemesAsPrivileged([{scheme:'wallet',privileges:{standard:true,secure:true,supportFetchAPI:true}}]);
const origin='wallet://app';const promptOrigin='wallet://prompt'; const sessionID=randomBytes(16).toString('hex');
let core:{stdin:WriteStream,stdout:ReadStream}|undefined;let win:BrowserWindow|undefined;let serial=0;let buffered=Buffer.alloc(0);
let statusTimer:NodeJS.Timeout|undefined;let statusInFlight=false;
const nativeMethods=new Set(['create','import','unlock','confirmRecovery','password','receive','copyReceive','saveQR','scanQR','configure','associate','discovery','discoverAccounts','send','exchangeConnect','exchangeDisconnect','exchangeDeposit','exchangeSwap','exchangeWithdraw']);
const monetaryMethods=new Set(['send','exchangeDeposit','exchangeSwap','exchangeWithdraw']);
// Only reviewed, value-free operation codes may cross the desktop boundary.
// Ordinary failures resolve the IPC handler, avoiding Electron's rejected-handler
// stack logging. Core/session protocol failures still terminate the session.
const operationErrors=new Set(['PASSWORD_POLICY','INPUT_TOO_LONG','PASSWORD_MISMATCH','INVALID_MNEMONIC','REQUEST_CANCELLED','REQUEST_FAILED_STATE_UNKNOWN']);
function operationCode(value:unknown):string{return typeof value==='string'&&operationErrors.has(value)?value:'REQUEST_FAILED_STATE_UNKNOWN';}
class OperationError extends Error{readonly code:string;constructor(value:unknown){const code=operationCode(value);super(code);this.code=code;}}
type Pending={method:string,resolve:(x:unknown)=>void,reject:(e:Error)=>void,timer:NodeJS.Timeout|undefined,deadline:number,activityWindow:number,activityCount:number,promptIDs:Set<string>};
const pending=new Map<string,Pending>();
function renewDeadline(id:string,p:Pending):void{
 clearTimeout(p.timer);const generation=++p.deadline;
 p.timer=setTimeout(()=>{if(pending.get(id)!==p||p.deadline!==generation)return;const prompted=activePrompt?.requestID===id;if(prompted)finishPrompt(true);pending.delete(id);p.reject(new Error('Request timed out; reconcile before retrying'));if(prompted||monetaryMethods.has(p.method))void request('lock',{}).catch(stop)},nativeMethods.has(p.method)?360000:180000);
}
function nativeActivity(id:string,p:Pending,r:ReturnType<typeof parseResponse>):void{
 if(!nativeMethods.has(p.method)||Object.hasOwn(r,'error')||r.body===null||typeof r.body!=='object'||Array.isArray(r.body)||Object.keys(r.body).length!==0)throw new Error('Invalid native activity');
 const now=Date.now();if(now-p.activityWindow>=1000){p.activityWindow=now;p.activityCount=0}if(++p.activityCount>64)throw new Error('Excess native activity');
 renewDeadline(id,p);
}
function notifyLocked():void{finishPrompt(true);if(win&&!win.webContents.isDestroyed()){try{win.webContents.send('wallet:locked')}catch{/* A disposed render frame cannot retain a live view. Locking must continue. */}}}
function stop():void{finishPrompt(false);buffered.fill(0);buffered=Buffer.alloc(0);clearInterval(statusTimer);statusTimer=undefined;const previous=core;core=undefined;if(previous)notifyLocked();previous?.stdin.destroy();previous?.stdout.destroy();for(const p of pending.values()){clearTimeout(p.timer);p.reject(new Error('Wallet session ended'))}pending.clear();}
function lockedSnapshot(value:unknown):boolean{return value!==null&&typeof value==='object'&&(value as {locked?:unknown}).locked===true;}
function pollLock():void{
 if(!core||!win||win.webContents.isDestroyed()||statusInFlight||pending.size!==0)return;
 statusInFlight=true;
 void request('status',{}).then(value=>{if(lockedSnapshot(value))notifyLocked()}).catch(stop).finally(()=>{statusInFlight=false});
}
function request(method:string,body:unknown):Promise<unknown>{
 return new Promise((resolve,reject)=>{if(!core){reject(new Error('Core unavailable'));return}const id=String(++serial);const payload=Buffer.from(JSON.stringify({version:1,id,session:sessionID,method,body}));if(payload.length>1048576){reject(new Error('Oversized request'));return}const h=Buffer.alloc(4);h.writeUInt32BE(payload.length);const p:Pending={method,resolve,reject,timer:undefined,deadline:0,activityWindow:0,activityCount:0,promptIDs:new Set()};pending.set(id,p);renewDeadline(id,p);core.stdin.write(Buffer.concat([h,payload]));});
}
function startCore():void{
 const bin=app.isPackaged?path.join(process.resourcesPath,'bin'):path.resolve(__dirname,'../../bin');const executable=path.join(bin,'wallet-core');
 const manifest=JSON.parse(fs.readFileSync(path.join(bin,'manifest.json'),'utf8')) as Record<string,string>;
 if(createHash('sha256').update(fs.readFileSync(executable)).digest('hex')!==manifest['wallet-core'])throw new Error('Packaged core identity mismatch');
 if(process.env['LOCALWALLET_INHERITED_CORE']!=='1')throw new Error('Start with the packaged network-isolating launcher');
 core={stdin:fs.createWriteStream('',{fd:4,autoClose:true}),stdout:fs.createReadStream('',{fd:3,autoClose:true})};
 core.stdout.on('data',(chunk:string|Buffer)=>{buffered=Buffer.concat([buffered,typeof chunk==='string'?Buffer.from(chunk):chunk]);if(buffered.length>2097152){stop();return}while(buffered.length>=4){const length=buffered.readUInt32BE();if(length<1||length>1048576){stop();return}if(buffered.length<4+length)return;const b=buffered.subarray(4,4+length);buffered=buffered.subarray(4+length);try{const raw=b.toString();b.fill(0);const r=parseResponse(raw);if(r.version!==1||r.session!==sessionID||typeof r.id!=='string'){stop();return}const p=pending.get(r.id);if(!p){stop();return}if(r.method==='prompt'){if(Object.hasOwn(r,'error')||!nativeMethods.has(p.method))throw new Error('Invalid prompt operation');const prompt=validatePrompt(r.body);if(p.promptIDs.has(prompt.id)||p.promptIDs.size>=64||(prompt.kind==='copy'&&p.method!=='copyReceive')||(prompt.kind==='file'&&!['saveQR','scanQR'].includes(p.method))||(prompt.kind==='receive'&&!['receive','copyReceive','saveQR'].includes(p.method))||(prompt.kind==='recovery'&&!['create','confirmRecovery'].includes(p.method)))throw new Error('Invalid prompt binding');p.promptIDs.add(prompt.id);void showPrompt(r.id,prompt).catch(stop);continue}if(r.method==='activity'&&p.method!=='activity'){nativeActivity(r.id,p,r);continue}if(p.method!==r.method){stop();return}if(activePrompt?.requestID===r.id)finishPrompt(false);pending.delete(r.id);clearTimeout(p.timer);if(Object.hasOwn(r,'error'))p.reject(new OperationError(r.error));else p.resolve(r.body)}catch{stop();return}}});
 core.stdin.on('error',stop);core.stdout.on('error',stop);core.stdout.on('end',stop);
}
type ActivePrompt={requestID:string,prompt:Prompt,window:BrowserWindow|undefined,ses:Electron.Session|undefined,delivered:boolean,activityAt:number};
let activePrompt:ActivePrompt|undefined;
function promptReply(active:ActivePrompt,reply:PromptReply):void{
 if(!core||pending.get(active.requestID)===undefined)return;
 const body=Buffer.from(JSON.stringify({version:1,id:active.requestID,session:sessionID,method:'promptReply',body:reply}));const header=Buffer.alloc(4);header.writeUInt32BE(body.length);const frame=Buffer.concat([header,body]);body.fill(0);
 core.stdin.write(frame,()=>frame.fill(0));if(reply.values)reply.values.fill('');delete reply.values;delete reply.path;
}
function finishPrompt(cancel:boolean):void{
 const active=activePrompt;if(!active)return;activePrompt=undefined;
 if(cancel)promptReply(active,{promptId:active.prompt.id,accepted:false});clearPrompt(active.prompt);
 if(active.window&&!active.window.isDestroyed())active.window.destroy();
 void active.ses?.clearStorageData().catch(()=>{});active.window=undefined;active.ses=undefined;
}
function promptEvent(event:Electron.IpcMainInvokeEvent|Electron.IpcMainEvent):ActivePrompt{
 const active=activePrompt,w=active?.window;
 if(!active||!w||w.isDestroyed()||event.sender!==w.webContents||event.senderFrame!==w.webContents.mainFrame||event.senderFrame.url!==promptOrigin+'/'||!pending.has(active.requestID))throw new Error('Prompt unavailable');
 return active;
}
function promptActivity(active:ActivePrompt):void{
 if(activePrompt!==active||Date.now()-active.activityAt<1000)return;
 active.activityAt=Date.now();const p=pending.get(active.requestID);if(!p)return;renewDeadline(active.requestID,p);void request('activity',{}).catch(stop);
}
function secureSession(partition:string,host:string,files:Record<string,string>):Electron.Session{
 const ses=session.fromPartition(partition,{cache:false});ses.setPermissionRequestHandler((_w,_p,cb)=>cb(false));ses.setPermissionCheckHandler(()=>false);
 const local='wallet://'+host+'/';ses.webRequest.onBeforeRequest((details,callback)=>callback({cancel:!details.url.startsWith(local)&&!details.url.startsWith('data:image/png;base64,')}));
 ses.on('will-download',event=>event.preventDefault());
 ses.protocol.handle('wallet',request=>{const u=new URL(request.url);const name=files[u.pathname];if(u.host!==host||!name||u.search||u.hash)return new Response('Denied',{status:403});return net.fetch('file://'+path.join(__dirname,'../ui',name));});
 return ses;
}
async function showPrompt(requestID:string,prompt:Prompt):Promise<void>{
 if(activePrompt||!win||win.webContents.isDestroyed()||!pending.has(requestID))throw new Error('Prompt unavailable');
 const active:ActivePrompt={requestID,prompt,window:undefined,ses:undefined,delivered:false,activityAt:0};activePrompt=active;
 if(prompt.kind==='copy'){clipboard.writeText(prompt.address!);promptReply(active,{promptId:prompt.id,accepted:true});finishPrompt(false);return}
 if(prompt.kind==='file'){
  const title=prompt.title;const result=prompt.save?await dialog.showSaveDialog(win,{title}):await dialog.showOpenDialog(win,{title,properties:['openFile']});
  if(activePrompt!==active||!pending.has(requestID))return;
  const filename='filePath' in result?result.filePath:result.filePaths[0];const reply=validatePromptReply(filename&&!result.canceled?{promptId:prompt.id,accepted:true,path:filename}:{promptId:prompt.id,accepted:false},prompt);
  promptReply(active,reply);finishPrompt(false);return;
 }
 const partition='wallet-prompt-'+randomBytes(16).toString('hex');active.ses=secureSession(partition,'prompt',{'/':'prompt.html','/prompt.js':'prompt.js','/prompt.css':'prompt.css'});
 const fixedTitle='LocalWallet — '+prompt.title.replace(/ — Go signer$/,'');
 const modal=new BrowserWindow({parent:win,modal:true,show:false,width:720,height:720,minWidth:540,minHeight:480,title:fixedTitle,webPreferences:{partition,preload:path.join(__dirname,'prompt-preload.js'),contextIsolation:true,sandbox:true,nodeIntegration:false,webSecurity:true,allowRunningInsecureContent:false,webviewTag:false,devTools:false}});active.window=modal;modal.setMenu(null);
 // The packaged document title must never replace the signer-bound dialog title.
 modal.on('page-title-updated',event=>event.preventDefault());
 modal.webContents.setWindowOpenHandler(()=>({action:'deny'}));modal.webContents.on('will-attach-webview',event=>event.preventDefault());
 const interrupted=()=>{if(activePrompt!==active)return;finishPrompt(true);notifyLocked();void request('lock',{}).catch(stop)};
 modal.webContents.on('will-navigate',event=>{event.preventDefault();interrupted()});modal.webContents.on('render-process-gone',interrupted);
 modal.on('closed',()=>{if(activePrompt===active)finishPrompt(true)});
 modal.webContents.on('before-input-event',(event,input)=>{if(input&&(input.control||input.meta)&&['r','R','F12'].includes(input.key)){event.preventDefault();return}promptActivity(active)});
 await modal.loadURL(promptOrigin+'/');if(activePrompt===active&&!modal.isDestroyed()){modal.setTitle(fixedTitle);modal.show()}
}
void app.whenReady().then(async()=>{
 if(process.getuid?.()===0)throw new Error('Run as an ordinary user');
 startCore();
 const ses=session.fromPartition('wallet-local',{cache:false});ses.setPermissionRequestHandler((_w,_p,cb)=>cb(false));ses.setPermissionCheckHandler(()=>false);
 ses.webRequest.onBeforeRequest((details,callback)=>callback({cancel:!details.url.startsWith(origin+'/')&&!details.url.startsWith('data:image/png;base64,')}));
 ses.on('will-download',event=>event.preventDefault());
 await ses.protocol.handle('wallet',request=>{const u=new URL(request.url);const files:Record<string,string>={'/':'index.html','/renderer.js':'renderer.js','/style.css':'style.css'};const name=files[u.pathname];if(u.host!=='app'||!name||u.search||u.hash)return new Response('Denied',{status:403});return net.fetch('file://'+path.join(__dirname,'../ui',name));});
 win=new BrowserWindow({width:1180,height:850,title:'LocalWallet — Verification pending',webPreferences:{partition:'wallet-local',preload:path.join(__dirname,'preload.js'),contextIsolation:true,sandbox:true,nodeIntegration:false,webSecurity:true,allowRunningInsecureContent:false,webviewTag:false,devTools:false}});
 win.on('closed',()=>{win=undefined;stop()});
 win.webContents.on('will-navigate',e=>e.preventDefault());win.webContents.setWindowOpenHandler(()=>({action:'deny'}));win.webContents.on('will-attach-webview',e=>e.preventDefault());
 const osLock=()=>{finishPrompt(true);notifyLocked();void request('lock',{}).catch(stop)};
 win.webContents.on('render-process-gone',osLock);
 let lastActivity=0;win.webContents.on('before-input-event',()=>{if(Date.now()-lastActivity>1000){lastActivity=Date.now();void request('activity',{}).catch(stop)}});
 ipcMain.on('wallet:activity',event=>{if(win&&event.sender===win.webContents&&event.senderFrame===win.webContents.mainFrame&&event.senderFrame.url===origin+'/')void request('activity',{}).catch(stop)});
 ipcMain.handle('wallet:request',async(event,method:unknown,body:unknown)=>{if(!win||event.sender!==win.webContents||event.senderFrame!==win.webContents.mainFrame||event.senderFrame.url!==origin+'/')throw new Error('Untrusted frame');validate(method,body);if(method==='lock'){finishPrompt(true);notifyLocked()}try{const result=await request(method,body);if(method==='status'&&lockedSnapshot(result))notifyLocked();return {ok:true,body:result}}catch(error){return {ok:false,error:error instanceof OperationError?error.code:'REQUEST_FAILED_STATE_UNKNOWN'}}});
 ipcMain.handle('wallet:prompt:get',event=>{const active=promptEvent(event);if(active.delivered)throw new Error('Prompt unavailable');active.delivered=true;return active.prompt});
 ipcMain.handle('wallet:prompt:reply',(event,body:unknown)=>{const active=promptEvent(event);try{if(!active.delivered)throw new Error('Prompt unavailable');const reply=validatePromptReply(body,active.prompt);promptReply(active,reply);finishPrompt(false)}catch{finishPrompt(true);notifyLocked();void request('lock',{}).catch(stop);throw new Error('Prompt unavailable')}});
 ipcMain.handle('wallet:prompt:copy',event=>{const active=promptEvent(event);if(active.prompt.kind!=='receive'||!active.delivered)throw new Error('Prompt unavailable');clipboard.writeText(active.prompt.address!)});
 ipcMain.on('wallet:prompt:activity',event=>{try{promptActivity(promptEvent(event))}catch{/* Untrusted or expired events carry no activity. */}});
 ipcMain.handle('wallet:version',event=>{if(!win||event.sender!==win.webContents||event.senderFrame!==win.webContents.mainFrame||event.senderFrame.url!==origin+'/')throw new Error('Untrusted frame');return app.getVersion()});
 powerMonitor.on('lock-screen',osLock);powerMonitor.on('suspend',osLock);powerMonitor.on('shutdown',osLock);
 // Status queries never send activity. Avoid queueing behind native dialogs or
 // other work, and never let overlapping timer ticks accumulate requests.
 statusTimer=setInterval(pollLock,5000);
 await win.loadURL(origin+'/');
}).catch(()=>{stop();app.quit()});
app.on('window-all-closed',()=>app.quit());app.on('before-quit',stop);
