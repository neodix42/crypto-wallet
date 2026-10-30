// Keep this list aligned with Go's chainutil.TransactionExplorer. Provider data
// never chooses an arbitrary browser URL or changes the wallet's navigation.
const evmHosts = new Set(['etherscan.io', 'sepolia.etherscan.io', 'basescan.org', 'sepolia.basescan.org', 'hyperevmscan.io']);
const coreHosts = new Set(['app.hyperliquid.xyz', 'app.hyperliquid-testnet.xyz']);
const nearHosts = new Set(['nearblocks.io', 'testnet.nearblocks.io']);
const decredHosts = new Set(['dcrdata.decred.org', 'testnet.dcrdata.org']);
const tonHosts = new Set(['tonviewer.com', 'testnet.tonviewer.com']);
function nearHash(value: string): boolean {
  const alphabet = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
  if (!/^[1-9A-HJ-NP-Za-km-z]{32,44}$/.test(value)) return false;
  let decoded = 0n;
  for (const char of value) decoded = decoded * 58n + BigInt(alphabet.indexOf(char));
  let bytes = 0;
  for (; decoded > 0n; decoded >>= 8n) bytes++;
  return bytes + value.length - value.replace(/^1+/, '').length === 32;
}
export function safeExplorerURL(value: string): boolean {
  let url: URL;
  try { url = new URL(value); } catch { return false; }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || url.hash || url.href !== value) return false;
  const host = url.hostname, path = url.pathname;
  if (host === 'explorer.aptoslabs.com') return /^\/txn\/0x[0-9a-f]{64}$/.test(path) && /^\?network=(mainnet|testnet)$/.test(url.search);
  if (url.search) return false;
  if (evmHosts.has(host)) return /^\/tx\/0x[0-9a-f]{64}$/.test(path);
  if (coreHosts.has(host)) return /^\/explorer\/tx\/0x[0-9a-f]{64}$/.test(path);
  if (nearHosts.has(host)) return path.startsWith('/txns/') && nearHash(path.slice(6));
  if (decredHosts.has(host)) return /^\/tx\/[0-9a-f]{64}$/.test(path);
  return tonHosts.has(host) && /^\/transaction\/[0-9a-f]{64}$/.test(path);
}
