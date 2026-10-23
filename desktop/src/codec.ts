export interface ResponseFrame {version:1;id:string;session:string;method:string;body:unknown;error?:string}
// JSON.parse validates syntax; this bounded structural pass additionally rejects
// duplicate semantic keys (including escaped spellings) at every object depth.
export function parseResponse(text:string):ResponseFrame {
 if(text.length===0||text.length>1048576)throw new Error('Invalid response size');
 const value:unknown=JSON.parse(text);
 const stack:{kind:string;key:boolean;seen:Set<string>}[]=[];
 for(let i=0;i<text.length;i++){
  const c=text[i];
  if(c==='"'){
   const start=i++;for(;i<text.length;i++){if(text[i]==='\\'){i++;continue}if(text[i]==='"')break}
   const top=stack[stack.length-1];
   if(top?.kind==='{'&&top.key){const key=JSON.parse(text.slice(start,i+1)) as string;if(top.seen.has(key))throw new Error('Duplicate response field');top.seen.add(key);top.key=false}
  }else if(c==='{'||c==='['){if(stack.length>=32)throw new Error('Response nesting rejected');stack.push({kind:c,key:c==='{',seen:new Set()})}
  else if(c==='}'||c===']')stack.pop();
  else if(c===','){const top=stack[stack.length-1];if(top?.kind==='{')top.key=true}
 }
 if(value===null||typeof value!=='object'||Array.isArray(value))throw new Error('Invalid response object');
 const r=value as Record<string,unknown>,allowed=['version','id','session','method','body','error'];
 if(Object.keys(r).some(k=>!allowed.includes(k))||!['version','id','session','method','body'].every(k=>Object.hasOwn(r,k)))throw new Error('Invalid response fields');
 if(r['version']!==1||typeof r['id']!=='string'||r['id'].length===0||r['id'].length>64||typeof r['session']!=='string'||!/^[0-9a-f]{32}$/.test(r['session'])||typeof r['method']!=='string'||r['method'].length===0||r['method'].length>64||(Object.hasOwn(r,'error')&&(typeof r['error']!=='string'||r['error'].length>128)))throw new Error('Invalid response schema');
 return value as ResponseFrame;
}
