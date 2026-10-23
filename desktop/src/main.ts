import {app,BrowserWindow,ipcMain,protocol,session,powerMonitor,net} from 'electron';
import {ReadStream,WriteStream} from 'node:fs';
import {randomBytes,createHash} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import {validate} from './validation';
import {parseResponse} from './codec';

app.enableSandbox();
// Public wallet views and QR images do not need a hardware rendering pipeline.
// Keep Chromium away from host-specific Mesa/DRI loaders while retaining sandboxing.
app.disableHardwareAcceleration();
app.commandLine.appendSwitch('force-webrtc-ip-handling-policy','disable_non_proxied_udp');
protocol.registerSchemesAsPrivileged([{scheme:'wallet',privileges:{standard:true,secure:true,supportFetchAPI:true}}]);
const origin='wallet://app'; const sessionID=randomBytes(16).toString('hex');
let core:{stdin:WriteStream,stdout:ReadStream}|undefined;let win:BrowserWindow|undefined;let serial=0;let buffered=Buffer.alloc(0);
let statusTimer:NodeJS.Timeout|undefined;let statusInFlight=false;
const nativeMethods=new Set(['create','import','unlock','backup','restore','password','receive','copyReceive','saveQR','scanQR','configure','associate','discovery','discoverAccounts','send']);
// Only reviewed, value-free operation codes may cross the desktop boundary.
// Ordinary failures resolve the IPC handler, avoiding Electron's rejected-handler
// stack logging. Core/session protocol failures still terminate the session.
const operationErrors=new Set(['PASSWORD_POLICY','INPUT_TOO_LONG','PASSWORD_MISMATCH','INVALID_MNEMONIC','BACKUP_DESTINATION_UNSAFE','BACKUP_EXISTS','BACKUP_WRITE_FAILED','BACKUP_VERIFY_FAILED','RESTORE_SOURCE_UNSAFE','REQUEST_CANCELLED','REQUEST_FAILED_STATE_UNKNOWN']);
function operationCode(value:unknown):string{return typeof value==='string'&&operationErrors.has(value)?value:'REQUEST_FAILED_STATE_UNKNOWN';}
class OperationError extends Error{readonly code:string;constructor(value:unknown){const code=operationCode(value);super(code);this.code=code;}}
type Pending={method:string,resolve:(x:unknown)=>void,reject:(e:Error)=>void,timer:NodeJS.Timeout|undefined,deadline:number,activityWindow:number,activityCount:number};
const pending=new Map<string,Pending>();
function renewDeadline(id:string,p:Pending):void{
 clearTimeout(p.timer);const generation=++p.deadline;
 p.timer=setTimeout(()=>{if(pending.get(id)!==p||p.deadline!==generation)return;pending.delete(id);p.reject(new Error('Request timed out; reconcile before retrying'));if(p.method==='send')void request('lock',{}).catch(stop)},nativeMethods.has(p.method)?360000:180000);
}
function nativeActivity(id:string,p:Pending,r:ReturnType<typeof parseResponse>):void{
 if(!nativeMethods.has(p.method)||Object.hasOwn(r,'error')||r.body===null||typeof r.body!=='object'||Array.isArray(r.body)||Object.keys(r.body).length!==0)throw new Error('Invalid native activity');
 const now=Date.now();if(now-p.activityWindow>=1000){p.activityWindow=now;p.activityCount=0}if(++p.activityCount>64)throw new Error('Excess native activity');
 renewDeadline(id,p);
}
function notifyLocked():void{if(win&&!win.webContents.isDestroyed()){try{win.webContents.send('wallet:locked')}catch{/* A disposed render frame cannot retain a live view. Locking must continue. */}}}
function stop():void{clearInterval(statusTimer);statusTimer=undefined;const previous=core;core=undefined;if(previous)notifyLocked();previous?.stdin.destroy();previous?.stdout.destroy();for(const p of pending.values()){clearTimeout(p.timer);p.reject(new Error('Wallet session ended'))}pending.clear();}
function lockedSnapshot(value:unknown):boolean{return value!==null&&typeof value==='object'&&(value as {locked?:unknown}).locked===true;}
function pollLock():void{
 if(!core||!win||win.webContents.isDestroyed()||statusInFlight||pending.size!==0)return;
 statusInFlight=true;
 void request('status',{}).then(value=>{if(lockedSnapshot(value))notifyLocked()}).catch(stop).finally(()=>{statusInFlight=false});
}
function request(method:string,body:unknown):Promise<unknown>{
 return new Promise((resolve,reject)=>{if(!core){reject(new Error('Core unavailable'));return}const id=String(++serial);const payload=Buffer.from(JSON.stringify({version:1,id,session:sessionID,method,body}));if(payload.length>1048576){reject(new Error('Oversized request'));return}const h=Buffer.alloc(4);h.writeUInt32BE(payload.length);const p:Pending={method,resolve,reject,timer:undefined,deadline:0,activityWindow:0,activityCount:0};pending.set(id,p);renewDeadline(id,p);core.stdin.write(Buffer.concat([h,payload]));});
}
function startCore():void{
 const bin=app.isPackaged?path.join(process.resourcesPath,'bin'):path.resolve(__dirname,'../../bin');const executable=path.join(bin,'wallet-core');
 const manifest=JSON.parse(fs.readFileSync(path.join(bin,'manifest.json'),'utf8')) as Record<string,string>;
 if(createHash('sha256').update(fs.readFileSync(executable)).digest('hex')!==manifest['wallet-core'])throw new Error('Packaged core identity mismatch');
 if(process.env['LOCALWALLET_INHERITED_CORE']!=='1')throw new Error('Start with the packaged network-isolating launcher');
 core={stdin:fs.createWriteStream('',{fd:4,autoClose:true}),stdout:fs.createReadStream('',{fd:3,autoClose:true})};
 core.stdout.on('data',(chunk:string|Buffer)=>{buffered=Buffer.concat([buffered,typeof chunk==='string'?Buffer.from(chunk):chunk]);if(buffered.length>2097152){stop();return}while(buffered.length>=4){const length=buffered.readUInt32BE();if(length<1||length>1048576){stop();return}if(buffered.length<4+length)return;const b=buffered.subarray(4,4+length);buffered=buffered.subarray(4+length);try{const r=parseResponse(b.toString());if(r.version!==1||r.session!==sessionID||typeof r.id!=='string'){stop();return}const p=pending.get(r.id);if(!p){stop();return}if(r.method==='activity'&&p.method!=='activity'){nativeActivity(r.id,p,r);continue}if(p.method!==r.method){stop();return}pending.delete(r.id);clearTimeout(p.timer);if(Object.hasOwn(r,'error'))p.reject(new OperationError(r.error));else p.resolve(r.body)}catch{stop();return}}});
 core.stdin.on('error',stop);core.stdout.on('error',stop);core.stdout.on('end',stop);
}
void app.whenReady().then(async()=>{
 if(process.getuid?.()===0)throw new Error('Run as an ordinary user');
 startCore();
 const ses=session.fromPartition('wallet-local',{cache:false});ses.setPermissionRequestHandler((_w,_p,cb)=>cb(false));ses.setPermissionCheckHandler(()=>false);
 ses.webRequest.onBeforeRequest((details,callback)=>callback({cancel:!details.url.startsWith(origin+'/')&&!details.url.startsWith('data:image/png;base64,')}));
 ses.on('will-download',event=>event.preventDefault());
 await ses.protocol.handle('wallet',request=>{const u=new URL(request.url);const files:Record<string,string>={'/':'index.html','/renderer.js':'renderer.js','/style.css':'style.css'};const name=files[u.pathname];if(u.host!=='app'||!name||u.search||u.hash)return new Response('Denied',{status:403});return net.fetch('file://'+path.join(__dirname,'../ui',name));});
 win=new BrowserWindow({width:1180,height:850,title:'LocalWallet — Development / Testnet',webPreferences:{partition:'wallet-local',preload:path.join(__dirname,'preload.js'),contextIsolation:true,sandbox:true,nodeIntegration:false,webSecurity:true,allowRunningInsecureContent:false,webviewTag:false,devTools:false}});
 win.on('closed',()=>{win=undefined;stop()});
 win.webContents.on('will-navigate',e=>e.preventDefault());win.webContents.setWindowOpenHandler(()=>({action:'deny'}));win.webContents.on('will-attach-webview',e=>e.preventDefault());
 const osLock=()=>{notifyLocked();void request('lock',{}).catch(stop)};
 win.webContents.on('render-process-gone',osLock);
 let lastActivity=0;win.webContents.on('before-input-event',()=>{if(Date.now()-lastActivity>1000){lastActivity=Date.now();void request('activity',{}).catch(stop)}});
 ipcMain.on('wallet:activity',event=>{if(win&&event.sender===win.webContents&&event.senderFrame===win.webContents.mainFrame&&event.senderFrame.url.startsWith(origin+'/'))void request('activity',{}).catch(stop)});
 ipcMain.handle('wallet:request',async(event,method:unknown,body:unknown)=>{if(!win||event.sender!==win.webContents||event.senderFrame!==win.webContents.mainFrame||!event.senderFrame.url.startsWith(origin+'/'))throw new Error('Untrusted frame');validate(method,body);if(method==='lock')notifyLocked();try{const result=await request(method,body);if(method==='status'&&lockedSnapshot(result))notifyLocked();return {ok:true,body:result}}catch(error){return {ok:false,error:error instanceof OperationError?error.code:'REQUEST_FAILED_STATE_UNKNOWN'}}});
 ipcMain.handle('wallet:version',event=>{if(!win||event.sender!==win.webContents||event.senderFrame!==win.webContents.mainFrame||!event.senderFrame.url.startsWith(origin+'/'))throw new Error('Untrusted frame');return app.getVersion()});
 powerMonitor.on('lock-screen',osLock);powerMonitor.on('suspend',osLock);powerMonitor.on('shutdown',osLock);
 // Status queries never send activity. Avoid queueing behind native dialogs or
 // other work, and never let overlapping timer ticks accumulate requests.
 statusTimer=setInterval(pollLock,5000);
 await win.loadURL(origin+'/');
}).catch(()=>{stop();app.quit()});
app.on('window-all-closed',()=>app.quit());app.on('before-quit',stop);
