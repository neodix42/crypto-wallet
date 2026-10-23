import {contextBridge,ipcRenderer} from 'electron';
// No generic IPC, native objects, files, credentials or signing methods escape.
const methods=['networks','accounts','status','create','import','unlock','lock','balances','receive','history','prepare','send','retry','reconcile','backup','restore','password','addAccount','hideAccount','discoverAccounts','cancelPrepared','copyReceive','saveQR','scanQR','configure','associate','chainHistory','max','discovery'] as const;
const api:Record<string,(body?:unknown)=>Promise<unknown>>={};
for(const method of methods)api[method]=(body:unknown={})=>ipcRenderer.invoke('wallet:request',method,body);
contextBridge.exposeInMainWorld('wallet',Object.freeze({...api,getVersion():Promise<string>{return ipcRenderer.invoke('wallet:version')},onLocked(callback:unknown):()=>void{
 if(typeof callback!=='function')throw new TypeError('Lock callback must be a function');
 const listener=()=>{callback()};ipcRenderer.on('wallet:locked',listener);
 return ()=>{ipcRenderer.removeListener('wallet:locked',listener)};
}}));

let lastPointer=0;window.addEventListener('pointerdown',event=>{if(event.isTrusted&&Date.now()-lastPointer>1000){lastPointer=Date.now();ipcRenderer.send('wallet:activity')}},{capture:true});
