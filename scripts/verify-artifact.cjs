// Inspect and bind actual packaged bytes; this is not publisher authentication.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const root=path.resolve(__dirname,'..');
const production=['app/codec.js','app/main.js','app/preload.js','app/prompt-preload.js','app/prompts.js','app/validation.js','ui/index.html','ui/prompt.css','ui/prompt.html','ui/prompt.js','ui/renderer.js','ui/style.css'];
const components=['wallet-core','wallet-signer','wallet-launcher'];
const hash=value=>crypto.createHash('sha256').update(value).digest('hex');
const read=relative=>{const file=path.join(root,relative);if(fs.lstatSync(file).isSymbolicLink()||!fs.statSync(file).isFile())throw new Error('unsafe-artifact-file');return fs.readFileSync(file);};
const json=relative=>JSON.parse(read(relative).toString());
const reportPath=path.join(root,'reports/artifact-security.json');
const write=report=>{fs.mkdirSync(path.dirname(reportPath),{recursive:true});fs.writeFileSync(reportPath,JSON.stringify(report,null,2)+'\n');};
(async()=>{
 const fuse=await import('../desktop/node_modules/@electron/fuses/dist/index.js');
 const asar=await import('../desktop/node_modules/@electron/asar/lib/asar.js');
 const appdir=path.join(root,'dist/LocalWallet-linux-x64');
 const wire=await fuse.getCurrentFuseWire(path.join(appdir,'LocalWallet'));
 const expected=new Map([[fuse.FuseV1Options.RunAsNode,false],[fuse.FuseV1Options.EnableNodeOptionsEnvironmentVariable,false],[fuse.FuseV1Options.EnableNodeCliInspectArguments,false],[fuse.FuseV1Options.OnlyLoadAppFromAsar,true],[fuse.FuseV1Options.EnableCookieEncryption,true]]);
 if(wire.version!=='1')throw new Error('unexpected-fuse-schema');
 for(const [key,value]of expected)if(wire[key]!==String(Number(value)).charCodeAt(0))throw new Error('unsafe-electron-fuse');
 const archive=path.join(appdir,'resources/app.asar');
 const files=asar.listPackage(archive);
 const allowed=new Set([...production,'package.json'].map(file=>'/'+file));
 for(const file of files)if(!['/app','/ui'].includes(file)&&!allowed.has(file))throw new Error('unexpected-asar-file');
 const payloadSHA256={},productionSourceSHA256={};
 for(const file of production){
  if(!files.includes('/'+file))throw new Error('missing-production-file');
  const packed=asar.extractFile(archive,file),source=read('desktop/'+file);
  if(!packed.equals(source))throw new Error('packaged-production-source-mismatch');
  payloadSHA256[file]=hash(packed);productionSourceSHA256['desktop/'+file]=hash(source);
 }
 const manifest=json('dist/build-manifest.json');
 const binaryManifest=json('dist/LocalWallet-linux-x64/resources/bin/manifest.json');
 if(JSON.stringify(Object.keys(binaryManifest).sort())!==JSON.stringify([...components].sort()))throw new Error('unexpected-binary-manifest');
 if(!manifest.binaryManifest||components.some(name=>manifest.binaryManifest[name]!==binaryManifest[name]))throw new Error('candidate-binary-manifest-mismatch');
 for(const name of components){
  if(typeof binaryManifest[name]!=='string'||!/^[0-9a-f]{64}$/.test(binaryManifest[name]))throw new Error('invalid-binary-hash');
  if(hash(read('bin/'+name))!==binaryManifest[name]||hash(read('dist/LocalWallet-linux-x64/resources/bin/'+name))!==binaryManifest[name])throw new Error('candidate-binary-hash-mismatch');
 }
 if(hash(read('dist/LocalWallet-linux-x64/wallet-launcher'))!==binaryManifest['wallet-launcher'])throw new Error('candidate-launcher-hash-mismatch');
 const metadata=JSON.parse(asar.extractFile(archive,'package.json').toString());
 if(metadata.version!==manifest.version)throw new Error('candidate-version-mismatch');
 const artifact={version:manifest.version,binaryManifest,asarSHA256:hash(read('dist/LocalWallet-linux-x64/resources/app.asar')),electronSHA256:hash(read('dist/LocalWallet-linux-x64/LocalWallet'))};
 write({status:'PASS',scope:'Actual packaged Electron fuses, exact 12 production files and Go binary identities; no publisher authentication or OS sandbox acceptance claim',artifact,fuses:wire,files,payloadSHA256,productionSourceSHA256,productionPayloadMatchesSource:true,productionFilesCompared:production.length});
 console.log('PASS packaged fuses and 12 production payloads bound to current ASAR and Go binaries');
})().catch(()=>{write({status:'FAIL',reason:'packaged-artifact-verification-failed'});console.error('FAIL packaged artifact verification failed');process.exitCode=1;});
