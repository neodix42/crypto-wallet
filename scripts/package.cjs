// Optional complete desktop runtime. No publisher credentials or publishing.
const fs=require('node:fs');const path=require('node:path');const cp=require('node:child_process');const crypto=require('node:crypto');
const root=path.resolve(__dirname,'..');
(async()=>{
 const version=cp.execFileSync('python3',['scripts/version.py'],{cwd:root,encoding:'utf8'}).trim();
 const {packager}=await import('../desktop/node_modules/@electron/packager/dist/index.js');
 const output=await packager({dir:path.join(root,'desktop'),out:path.join(root,'dist'),name:'CryptoWallet',appVersion:version,buildVersion:version,beforeAsar:[async({buildPath})=>{const filename=path.join(buildPath,'package.json');const pkg=JSON.parse(fs.readFileSync(filename,'utf8'));pkg.version=version;fs.writeFileSync(filename,JSON.stringify(pkg,null,2)+'\n');}],electronVersion:'45.0.0',platform:'linux',arch:'x64',asar:true,overwrite:true,prune:true,ignore:[/^\/src($|\/)/,/^\/tests($|\/)/,/^\/node_modules($|\/)/,/tsconfig\.json/],extraResource:[path.join(root,'bin')]});
 const appdir=output[0];const fuse=await import('../desktop/node_modules/@electron/fuses/dist/index.js');
 await fuse.flipFuses(path.join(appdir,'CryptoWallet'),{version:fuse.FuseVersion.V1,[fuse.FuseV1Options.RunAsNode]:false,[fuse.FuseV1Options.EnableNodeOptionsEnvironmentVariable]:false,[fuse.FuseV1Options.EnableNodeCliInspectArguments]:false,[fuse.FuseV1Options.OnlyLoadAppFromAsar]:true,[fuse.FuseV1Options.EnableCookieEncryption]:true});
 // Keep the sandboxed Go launcher as the public entry point. Electron stays internal.
 fs.renameSync(path.join(appdir,'CryptoWallet'),path.join(appdir,'cryptowallet-ui'));
 fs.copyFileSync(path.join(root,'bin/wallet-launcher'),path.join(appdir,'cryptowallet'));fs.chmodSync(path.join(appdir,'cryptowallet'),0o755);
 cp.execFileSync('python3',['scripts/license-notices.py','--desktop-runtime',appdir],{cwd:root,stdio:'inherit'});
 const portable=path.join(root,'dist','cryptowallet-'+version+'-linux-amd64.tar.gz');
 cp.execFileSync('tar',['-C',path.dirname(appdir),'-czf',portable,path.basename(appdir)]);
 const manifest={format:'portable-desktop-v1',status:'DEVELOPMENT_UNSIGNED',version,builtAt:new Date().toISOString(),source:cp.execFileSync('git',['rev-parse','HEAD'],{cwd:root,encoding:'utf8'}).trim(),dirty:cp.execFileSync('git',['status','--porcelain'],{cwd:root,encoding:'utf8'}).trim()!=='',platform:'linux-amd64',os:fs.readFileSync('/etc/os-release','utf8'),go:cp.execFileSync('go',['version'],{encoding:'utf8'}).trim(),node:process.version,electron:'45.0.0',application:path.basename(appdir),portable:{file:path.basename(portable),sha256:crypto.createHash('sha256').update(fs.readFileSync(portable)).digest('hex')},binaryManifest:JSON.parse(fs.readFileSync(path.join(root,'bin/manifest.json'),'utf8')),fuses:await fuse.getCurrentFuseWire(path.join(appdir,'cryptowallet-ui')),limitations:['No publisher signature','OS/display-server matrix incomplete','Production release gates incomplete','Linux Electron ASAR integrity is not publisher authentication']};
 fs.writeFileSync(path.join(root,'dist/build-manifest.json'),JSON.stringify(manifest,null,2)+'\n');
 fs.writeFileSync(path.join(root,'dist/SHA256SUMS'),manifest.portable.sha256+'  '+manifest.portable.file+'\n');
 fs.writeFileSync(path.join(root,'dist/go-modules.json'),cp.execFileSync('go',['list','-m','-json','all'],{cwd:root,encoding:'utf8'}));fs.copyFileSync(path.join(root,'desktop/package-lock.json'),path.join(root,'dist/npm-package-lock.json'));console.log(portable);
})().catch(e=>{console.error(e.message);process.exit(1)});
