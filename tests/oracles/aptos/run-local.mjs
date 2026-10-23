// PUBLIC SYNTHETIC LOCALNET ONLY. Starts a private validator and always stops it.
import {spawn} from 'node:child_process';
import {existsSync,createWriteStream,readFileSync,mkdirSync,mkdtempSync,rmSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {resolve} from 'node:path';
import {once} from 'node:events';
const cli=resolve('.tools/aptos/aptos');
const expected='7da14e8cceced2b10834856fae2f6b26095275a4da488ca2c5215691daa37e90';
if(!existsSync(cli)||createHash('sha256').update(readFileSync(cli)).digest('hex')!==expected)throw new Error('Install pinned Aptos CLI 7.11.0 using tests/oracles/aptos/install-cli.sh');
let active=false;try{await fetch('http://127.0.0.1:8080/v1');active=true;}catch{}
if(active)throw new Error('Local port 8080 is occupied; refusing to reuse an unknown validator');
mkdirSync(resolve('.tools/tmp'),{recursive:true});
const dir=mkdtempSync(resolve('.tools/tmp/aptos-local-'));
const log=createWriteStream(resolve(dir,'node.log'));
let node,task;
const exitSignal=()=>{if(task)task.kill('SIGINT');if(node)node.kill('SIGINT');};
process.once('SIGINT',exitSignal);process.once('SIGTERM',exitSignal);
async function run(command,args,env=process.env){task=spawn(command,args,{stdio:'inherit',env});const [code]=await once(task,'exit');task=undefined;if(code!==0)throw new Error('Local Aptos test command failed');}
try{
 node=spawn(cli,['node','run-localnet','--test-dir',dir,'--no-txn-stream','--assume-yes'],{stdio:['ignore','pipe','pipe']});node.stdout.pipe(log);node.stderr.pipe(log);
 let ready=false;for(let i=0;i<120;i++){if(node.exitCode!==null)break;try{const r=await fetch('http://127.0.0.1:8070/');if(r.ok){ready=true;break}}catch{}await new Promise(r=>setTimeout(r,250));}
 if(!ready)throw new Error('Pinned local validator did not become ready');
 await run(process.execPath,['tests/oracles/aptos/setup-rotation.mjs']);
 await run('go',['test','./internal/chains/aptos','-run','TestLocalNode','-v','-count=1'],{...process.env,WALLET_APTOS_LOCAL:'1',WALLET_APTOS_ROTATION:'1'});
}finally{
 if(task&&task.exitCode===null)task.kill('SIGKILL');
 if(node&&node.exitCode===null){const ended=once(node,'exit');node.kill('SIGINT');const timer=setTimeout(()=>node.kill('SIGKILL'),10000);await ended;clearTimeout(timer);}
 log.end();rmSync(dir,{recursive:true,force:true});
 process.removeListener('SIGINT',exitSignal);process.removeListener('SIGTERM',exitSignal);
}
