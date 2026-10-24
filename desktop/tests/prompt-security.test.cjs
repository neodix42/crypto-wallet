'use strict';
const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const path=require('node:path');const vm=require('node:vm');
const {validatePrompt,validatePromptReply,clearPrompt}=require('../app/prompts.js');
const base={id:'a'.repeat(32),title:'Synthetic prompt'};
const examples=[{...base,kind:'input',labels:['Password']},{...base,kind:'confirm',summary:'Network / recipient / amount / fees'},{...base,kind:'notice',summary:''},{...base,kind:'recovery',phrase:Array(12).fill('abandon').join(' ')},{...base,kind:'receive',summary:'Ethereum',png:'iVBORw0KGgoAAAA=',address:'PUBLIC'},{...base,kind:'file',save:false},{...base,kind:'copy',address:'PUBLIC'}];
test('TP033 prompt schemas accept only the expected bounded fields and never interpret hostile text as authority',()=>{
 for(const prompt of examples){assert.equal(validatePrompt(prompt),prompt);assert.throws(()=>validatePrompt({...prompt,extra:'forbidden'}));for(const key of Object.keys(prompt)){const missing={...prompt};delete missing[key];assert.throws(()=>validatePrompt(missing),prompt.kind+':'+key)}}
 for(const value of [null,[],1,'{}',false])assert.throws(()=>validatePrompt(value));
 for(const mutation of [{id:'not-an-id'},{id:0},{title:''},{title:2},{title:'é'.repeat(129)},{title:'bad\0title'},{kind:'eval'}])assert.throws(()=>validatePrompt({...examples[0],...mutation}));
 for(const labels of [null,[],Array(9).fill('label'),[0],[''],['é'.repeat(129)]])assert.throws(()=>validatePrompt({...examples[0],labels}));
 for(const summary of [null,1,'x'.repeat(65537)])for(const kind of ['confirm','notice'])assert.throws(()=>validatePrompt({...base,kind,summary}));
 for(const phrase of [null,Array(11).fill('abandon').join(' '),' abandon '.repeat(12),'é '.repeat(12),'a'.repeat(513)])assert.throws(()=>validatePrompt({...examples[3],phrase}));
 for(const words of [12,15,18,21,24])validatePrompt({...examples[3],phrase:Array(words).fill('abandon').join(' ')});
 for(const mutation of [{summary:null},{address:0},{address:''},{address:'a'.repeat(513)},{png:null},{png:'iVBORw0KGgo'+ 'A'.repeat(350001)},{png:'data:image/png;base64,iVBORw0KGgo='},{png:'SGVsbG8='}])assert.throws(()=>validatePrompt({...examples[4],...mutation}));
 for(const save of [null,'false',0])assert.throws(()=>validatePrompt({...examples[5],save}));
 for(const address of [null,'','x'.repeat(513)])assert.throws(()=>validatePrompt({...examples[6],address}));
 validatePrompt({...base,kind:'confirm',summary:'<script>throw "public text only"</script>'});
});
test('TP042 prompt replies bind acceptance to one ID and exactly the current prompt kind',()=>{
 for(const prompt of examples){const canceled={promptId:prompt.id,accepted:false};assert.equal(validatePromptReply(canceled,prompt),canceled);assert.throws(()=>validatePromptReply({...canceled,values:[]},prompt));assert.throws(()=>validatePromptReply({...canceled,path:'/tmp/x'},prompt));const accepted={promptId:prompt.id,accepted:true,...(prompt.kind==='input'?{values:['synthetic password']}:prompt.kind==='file'?{path:'/tmp/public.png'}:{})};assert.equal(validatePromptReply(accepted,prompt),accepted);for(const key of Object.keys(accepted)){const missing={...accepted};delete missing[key];assert.throws(()=>validatePromptReply(missing,prompt))}assert.throws(()=>validatePromptReply({...accepted,extra:true},prompt));assert.throws(()=>validatePromptReply({...accepted,promptId:'b'.repeat(32)},prompt));assert.throws(()=>validatePromptReply({...accepted,accepted:'yes'},prompt))}
 for(const values of [null,[],['x','y'],[false],['x'.repeat(4097)],['a\0b']])assert.throws(()=>validatePromptReply({promptId:base.id,accepted:true,values},examples[0]));
 validatePromptReply({promptId:base.id,accepted:true,values:['é'.repeat(2048)]},examples[0]);assert.throws(()=>validatePromptReply({promptId:base.id,accepted:true,values:['é'.repeat(2049)]},examples[0]));
 for(const filename of [null,'','x'.repeat(4097),'a\0b'])assert.throws(()=>validatePromptReply({promptId:base.id,accepted:true,path:filename},examples[5]));
 for(const value of [null,[],1])assert.throws(()=>validatePromptReply(value,examples[0]));
 const payload={...examples[3],labels:['secret'],summary:'secret',png:'public',address:'public'};clearPrompt(payload);assert.deepEqual(payload,{id:'',title:'',kind:'recovery'});
});
test('TP033 prompt preload exposes only three scoped operations and sanitizes all transport errors',async()=>{
 let exposed;let fail=false,clock=10000;const calls=[],sent=[],listeners=[];
 const electron={contextBridge:{exposeInMainWorld(name,value){assert.equal(name,'walletPrompt');exposed=value}},ipcRenderer:{invoke:async(...args)=>{calls.push(args);if(fail)throw Error('SYNTHETIC-SECRET-ERROR');return 'safe'},send:(...args)=>sent.push(args)}};
 class Clock extends Date {static now(){return clock}}
 const context=vm.createContext({exports:{},Date:Clock,window:{addEventListener:(...args)=>listeners.push(args)},require(name){assert.equal(name,'electron');return electron}});const filename=path.resolve(__dirname,'../app/prompt-preload.js');new vm.Script(fs.readFileSync(filename,'utf8'),{filename}).runInContext(context);
 assert.ok(Object.isFrozen(exposed));assert.deepEqual(Object.keys(exposed).sort(),['copyAddress','getPrompt','respond']);assert.equal(await exposed.getPrompt(),'safe');assert.deepEqual(calls.at(-1),['wallet:prompt:get',undefined]);await exposed.respond({promptId:base.id,accepted:false});assert.equal(calls.at(-1)[0],'wallet:prompt:reply');await exposed.copyAddress();assert.deepEqual(calls.at(-1),['wallet:prompt:copy',undefined]);
 for(const forbidden of ['wallet','create','unlock','invoke','on','readFile','getMnemonic','activity','require'])assert.equal(exposed[forbidden],undefined);
 fail=true;for(const invoke of [()=>exposed.getPrompt(),()=>exposed.respond({}),()=>exposed.copyAddress()])await assert.rejects(invoke(),error=>error.message==='Prompt unavailable');
 assert.equal(listeners.length,1);assert.equal(listeners[0][0],'pointerdown');assert.equal(listeners[0][2].capture,true);const pointer=listeners[0][1];pointer({isTrusted:false});assert.equal(sent.length,0);pointer({isTrusted:true});pointer({isTrusted:true});assert.equal(sent.length,1);clock=11001;pointer({isTrusted:true});assert.deepEqual(sent,[['wallet:prompt:activity'],['wallet:prompt:activity']]);
});
