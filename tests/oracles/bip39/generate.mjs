// Independent public synthetic oracle. Never import any user wallet here.
import {Mnemonic} from 'ethers';import {writeFileSync} from 'node:fs';
const vectors=[];
for(const bytes of [16,20,24,28,32])for(const [p,passphrase] of ['', 'TREZOR','\u00e9','e\u0301','public synthetic \ud83d\ude42'].entries()){
 const entropy=Uint8Array.from({length:bytes},(_,i)=>(i*7)%256);const m=Mnemonic.fromEntropy(entropy,passphrase);vectors.push({id:`BIP39-English-${bytes*8}-${p}`,entropy:Buffer.from(entropy).toString('hex'),phrase:m.phrase,passphrase,seed:m.computeSeed().slice(2)});
}
writeFileSync(new URL('./fixtures.json',import.meta.url),JSON.stringify({classification:'PUBLIC SYNTHETIC TEST SECRETS ONLY',oracle:'ethers 6.17.0, independent JS BIP39/NFKD implementation, exact lockfile integrity',vectors},null,2)+'\n');
