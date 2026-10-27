import {contextBridge,ipcRenderer} from 'electron';
// Each invocation is additionally bound to the live modal's main frame and
// one signer-created prompt ID in main. Never expose generic IPC or portfolio API.
async function invoke(channel:string,body?:unknown):Promise<unknown>{try{return await ipcRenderer.invoke(channel,body)}catch{throw new Error('Prompt unavailable')}}
async function copyRecoveryPhrase():Promise<void>{
 const result=await invoke('wallet:prompt:copy-recovery');
 if(result===null||typeof result!=='object'||Array.isArray(result)||Object.keys(result).length!==1||(result as {ok?:unknown}).ok!==true)throw new Error('Prompt unavailable');
}
contextBridge.exposeInMainWorld('walletPrompt',Object.freeze({
 getPrompt:()=>invoke('wallet:prompt:get'),
 respond:(body:unknown)=>invoke('wallet:prompt:reply',body),
 copyAddress:()=>invoke('wallet:prompt:copy'),
 copyRecoveryPhrase
}));

let lastPointer=0;window.addEventListener('pointerdown',event=>{if(event.isTrusted&&Date.now()-lastPointer>1000){lastPointer=Date.now();ipcRenderer.send('wallet:prompt:activity')}},{capture:true});
