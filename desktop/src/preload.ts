import {contextBridge,ipcRenderer} from 'electron';
// No generic IPC, native objects, files, credentials or signing methods escape.
const methods=['networks','accounts','status','create','import','unlock','lock','balances','receive','history','prepare','send','retry','reconcile','confirmRecovery','password','addAccount','hideAccount','discoverAccounts','cancelPrepared','copyReceive','saveQR','scanQR','configure','associate','chainHistory','max','discovery'] as const;
const operationErrors=new Set(['PASSWORD_POLICY','INPUT_TOO_LONG','PASSWORD_MISMATCH','INVALID_MNEMONIC','REQUEST_CANCELLED','REQUEST_FAILED_STATE_UNKNOWN']);
function unwrap(value:unknown):unknown{
 if(value!==null&&typeof value==='object'&&!Array.isArray(value)){
  const result=value as Record<string,unknown>;
  if(Object.hasOwn(result,'ok')&&result['ok']===true&&Object.keys(result).length===2&&Object.hasOwn(result,'body'))return result['body'];
  if(Object.hasOwn(result,'ok')&&result['ok']===false&&Object.keys(result).length===2&&Object.hasOwn(result,'error')&&typeof result['error']==='string'&&operationErrors.has(result['error']))throw new Error(result['error']);
 }
 throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
}
const api:Record<string,(body?:unknown)=>Promise<unknown>>={};
for(const method of methods)api[method]=async(body:unknown={})=>{
 let value:unknown;try{value=await ipcRenderer.invoke('wallet:request',method,body)}catch{throw new Error('REQUEST_FAILED_STATE_UNKNOWN')}
 // Error.message carries only a safe code: contextBridge drops custom properties.
 return unwrap(value);
};
contextBridge.exposeInMainWorld('wallet',Object.freeze({...api,getVersion():Promise<string>{return ipcRenderer.invoke('wallet:version')},onLocked(callback:unknown):()=>void{
 if(typeof callback!=='function')throw new TypeError('Lock callback must be a function');
 const listener=()=>{callback()};ipcRenderer.on('wallet:locked',listener);
 return ()=>{ipcRenderer.removeListener('wallet:locked',listener)};
}}));

let lastPointer=0;window.addEventListener('pointerdown',event=>{if(event.isTrusted&&Date.now()-lastPointer>1000){lastPointer=Date.now();ipcRenderer.send('wallet:activity')}},{capture:true});
