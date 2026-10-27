// This transport is private to the isolated, temporary Electron prompt window.
// Portfolio IPC has a separate public-data-only allowlist.
export type PromptKind='input'|'confirm'|'notice'|'receive'|'recovery'|'recoveryBackup'|'file'|'copy';
export interface Prompt {id:string;kind:PromptKind;title:string;labels?:string[];summary?:string;phrase?:string;png?:string;address?:string;save?:boolean}
export interface PromptReply {promptId:string;accepted:boolean;values?:string[];path?:string}
function object(value:unknown):Record<string,unknown>{if(value===null||typeof value!=='object'||Array.isArray(value))throw new Error('Invalid prompt');return value as Record<string,unknown>}
function text(value:unknown,max:number):value is string{return typeof value==='string'&&Buffer.byteLength(value,'utf8')<=max&&!value.includes('\0')}
function exact(value:Record<string,unknown>,required:string[],optional:string[]=[]):void{if(required.some(key=>!Object.hasOwn(value,key))||Object.keys(value).some(key=>!required.includes(key)&&!optional.includes(key)))throw new Error('Invalid prompt fields')}
export function validatePrompt(value:unknown):Prompt{
 const p=object(value);if(typeof p['id']!=='string'||!/^[0-9a-f]{32}$/.test(p['id'])||!text(p['title'],256)||p['title'].length===0)throw new Error('Invalid prompt identity');
 const required=['id','kind','title'];
 switch(p['kind']){
  case 'input':exact(p,[...required,'labels']);if(!Array.isArray(p['labels'])||p['labels'].length<1||p['labels'].length>8||p['labels'].some(label=>!text(label,256)||label.length===0))throw new Error('Invalid prompt labels');break;
  case 'confirm':case 'notice':exact(p,[...required,'summary']);if(!text(p['summary'],65536))throw new Error('Invalid prompt summary');break;
  case 'recovery':case 'recoveryBackup':exact(p,[...required,'phrase']);if(!text(p['phrase'],512)||!/^([a-z]+ )*(?:[a-z]+)$/.test(p['phrase'])||![12,15,18,21,24].includes(p['phrase'].split(' ').length))throw new Error('Invalid recovery prompt');break;
  case 'receive':exact(p,[...required,'summary','png','address']);if(!text(p['summary'],65536)||!text(p['address'],512)||p['address'].length===0||!text(p['png'],350000)||!/^iVBORw0KGgo[A-Za-z0-9+/]*={0,2}$/.test(p['png']))throw new Error('Invalid receive prompt');break;
  case 'file':exact(p,[...required,'save']);if(typeof p['save']!=='boolean')throw new Error('Invalid file prompt');break;
  case 'copy':exact(p,[...required,'address']);if(!text(p['address'],512)||p['address'].length===0)throw new Error('Invalid copy prompt');break;
  default:throw new Error('Invalid prompt kind');
 }
 return p as unknown as Prompt;
}
export function validatePromptReply(value:unknown,prompt:Prompt):PromptReply{
 const r=object(value);if(r['promptId']!==prompt.id||typeof r['accepted']!=='boolean')throw new Error('Invalid prompt reply');
 if(!r['accepted'])exact(r,['promptId','accepted']);
 else if(prompt.kind==='input'){exact(r,['promptId','accepted','values']);if(!Array.isArray(r['values'])||r['values'].length!==prompt.labels?.length||r['values'].some(v=>!text(v,4096)))throw new Error('Invalid prompt input')}
 else if(prompt.kind==='file'){exact(r,['promptId','accepted','path']);if(!text(r['path'],4096)||r['path'].length===0)throw new Error('Invalid prompt path')}
 else exact(r,['promptId','accepted']);
 return r as unknown as PromptReply;
}
export function clearPrompt(prompt:Prompt):void{delete prompt.phrase;delete prompt.summary;delete prompt.labels;delete prompt.png;delete prompt.address;prompt.title='';prompt.id=''}
