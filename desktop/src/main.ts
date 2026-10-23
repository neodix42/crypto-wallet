import {app,BrowserWindow,ipcMain,protocol,session,powerMonitor,net} from 'electron';
import {ReadStream,WriteStream} from 'node:fs';
import {randomBytes,createHash} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import {validate} from './validation';
import {parseResponse} from './codec';

app.enableSandbox();
app.commandLine.appendSwitch('force-webrtc-ip-handling-policy','disable_non_proxied_udp');
protocol.registerSchemesAsPrivileged([{scheme:'wallet',privileges:{standard:true,secure:true,supportFetchAPI:true}}]);
const origin='wallet://app'; const sessionID=randomBytes(16).toString('hex');
let core:{stdin:WriteStream,stdout:ReadStream}|undefined;let win:BrowserWindow|undefined;let serial=0;let buffered=Buffer.alloc(0);
let statusTimer:NodeJS.Timeout|undefined;let statusInFlight=false;
const pending=new Map<string,{method:string,resolve:(x:unknown)=>void,reject:(e:Error)=>void,timer:NodeJS.Timeout}>();
function notifyLocked():void{if(win&&!win.webContents.isDestroyed())win.webContents.send('wallet:locked');}
function stop():void{clearInterval(statusTimer);statusTimer=undefined;const previous=core;core=undefined;if(previous)notifyLocked();previous?.stdin.destroy();previous?.stdout.destroy();for(const p of pending.values()){clearTimeout(p.timer);p.reject(new Error('Wallet session ended'))}pending.clear();}
function lockedSnapshot(value:unknown):boolean{return value!==null&&typeof value==='object'&&(value as {locked?:unknown}).locked===true;}
function pollLock():void{
 if(!core||!win||win.webContents.isDestroyed()||statusInFlight||pending.size!==0)return;
 statusInFlight=true;
 void request('status',{}).then(value=>{if(lockedSnapshot(value))notifyLocked()}).catch(stop).finally(()=>{statusInFlight=false});
}
function request(method:string,body:unknown):Promise<unknown>{
 return new Promise((resolve,reject)=>{if(!core){reject(new Error('Core unavailable'));return}const id=String(++serial);const payload=Buffer.from(JSON.stringify({version:1,id,session:sessionID,method,body}));if(payload.length>1048576){reject(new Error('Oversized request'));return}const h=Buffer.alloc(4);h.writeUInt32BE(payload.length);const timer=setTimeout(()=>{pending.delete(id);reject(new Error('Request timed out; reconcile before retrying'));if(method==='send')void request('lock',{}).catch(stop)},180000);pending.set(id,{method,resolve,reject,timer});core.stdin.write(Buffer.concat([h,payload]));});
}
function startCore():void{
 const bin=app.isPackaged?path.join(process.resourcesPath,'bin'):path.resolve(__dirname,'../../bin');const executable=path.join(bin,'wallet-core');
 const manifest=JSON.parse(fs.readFileSync(path.join(bin,'manifest.json'),'utf8')) as Record<string,string>;
 if(createHash('sha256').update(fs.readFileSync(executable)).digest('hex')!==manifest['wallet-core'])throw new Error('Packaged core identity mismatch');
 if(process.env['LOCALWALLET_INHERITED_CORE']!=='1')throw new Error('Start with the packaged network-isolating launcher');
 core={stdin:fs.createWriteStream('',{fd:4,autoClose:true}),stdout:fs.createReadStream('',{fd:3,autoClose:true})};
 core.stdout.on('data',(chunk:string|Buffer)=>{buffered=Buffer.concat([buffered,typeof chunk==='string'?Buffer.from(chunk):chunk]);if(buffered.length>2097152){stop();return}while(buffered.length>=4){const length=buffered.readUInt32BE();if(length<1||length>1048576){stop();return}if(buffered.length<4+length)return;const b=buffered.subarray(4,4+length);buffered=buffered.subarray(4+length);try{const r=parseResponse(b.toString());if(r.version!==1||r.session!==sessionID||typeof r.id!=='string'){stop();return}const p=pending.get(r.id);if(!p||p.method!==r.method){stop();return}pending.delete(r.id);clearTimeout(p.timer);if(r.error)p.reject(new Error('Operation failed or unavailable. Check wallet state before retrying.'));else p.resolve(r.body)}catch{stop();return}}});
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
 ipcMain.handle('wallet:request',async(event,method:unknown,body:unknown)=>{if(!win||event.sender!==win.webContents||event.senderFrame!==win.webContents.mainFrame||!event.senderFrame.url.startsWith(origin+'/'))throw new Error('Untrusted frame');validate(method,body);if(method==='lock')notifyLocked();const result=await request(method,body);if(method==='status'&&lockedSnapshot(result))notifyLocked();return result});
 powerMonitor.on('lock-screen',osLock);powerMonitor.on('suspend',osLock);powerMonitor.on('shutdown',osLock);
 // Status queries never send activity. Avoid queueing behind native dialogs or
 // other work, and never let overlapping timer ticks accumulate requests.
 statusTimer=setInterval(pollLock,5000);
 await win.loadURL(origin+'/');
}).catch(()=>{stop();app.quit()});
app.on('window-all-closed',()=>app.quit());app.on('before-quit',stop);
