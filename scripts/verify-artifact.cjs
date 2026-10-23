// Inspect actual packaged bytes; this is not publisher authentication.
const fs=require('node:fs'),path=require('node:path');
const root=path.resolve(__dirname,'..');
(async()=>{
 const fuse=await import('../desktop/node_modules/@electron/fuses/dist/index.js');
 const asar=await import('../desktop/node_modules/@electron/asar/lib/asar.js');
 const appdir=path.join(root,'dist/LocalWallet-linux-x64');
 const wire=await fuse.getCurrentFuseWire(path.join(appdir,'LocalWallet'));
 const expected=new Map([[fuse.FuseV1Options.RunAsNode,false],[fuse.FuseV1Options.EnableNodeOptionsEnvironmentVariable,false],[fuse.FuseV1Options.EnableNodeCliInspectArguments,false],[fuse.FuseV1Options.OnlyLoadAppFromAsar,true],[fuse.FuseV1Options.EnableCookieEncryption,true]]);
 if(wire.version!=='1')throw new Error('Unexpected Electron fuse schema');
 for(const [key,value]of expected)if(wire[key]!==String(Number(value)).charCodeAt(0))throw new Error('Unsafe Electron fuse '+key);
 const files=asar.listPackage(path.join(appdir,'resources/app.asar'));
 if(files.some(p=>/^\/(src|tests|node_modules)(\/|$)/.test(p)||p.endsWith('.test.cjs')))throw new Error('Development/test sources shipped in ASAR');
 for(const file of ['/app/main.js','/app/preload.js','/app/validation.js','/app/codec.js','/ui/renderer.js'])if(!files.includes(file))throw new Error('Missing application file '+file);
 const report={status:'PASS',scope:'Actual packaged Electron fuses and ASAR inventory; no publisher authentication or OS sandbox acceptance claim',fuses:wire,files};
 fs.mkdirSync(path.join(root,'reports'),{recursive:true});fs.writeFileSync(path.join(root,'reports/artifact-security.json'),JSON.stringify(report,null,2)+'\n');
 console.log('PASS packaged fuses and production ASAR inventory');
})().catch(e=>{console.error(e.message);process.exit(1)});
