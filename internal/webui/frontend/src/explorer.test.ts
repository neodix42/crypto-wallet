import { describe, expect, it } from 'vitest';
import { safeExplorerURL } from './explorer';

const hash = 'a1'.repeat(32), evm = '0x' + hash;
describe('reviewed transaction explorer routes', () => {
  it('accepts the exact routes constructed for supported public networks', () => {
    const urls = [
      ...['etherscan.io', 'sepolia.etherscan.io', 'basescan.org', 'sepolia.basescan.org', 'arbiscan.io', 'sepolia.arbiscan.io', 'hyperevmscan.io'].map(host => `https://${host}/tx/${evm}`),
      ...['app.hyperliquid.xyz', 'app.hyperliquid-testnet.xyz'].map(host => `https://${host}/explorer/tx/${evm}`),
      ...['nearblocks.io', 'testnet.nearblocks.io'].map(host => `https://${host}/txns/${'1'.repeat(32)}`),
      'https://nearblocks.io/txns/11111111111111111111111111111112',
      'https://nearblocks.io/txns/4vJ9JU1bJJE96FWSJKvHsmmFADCg4gpZQff4P3bkLKi',
      ...['mainnet', 'testnet'].map(network => `https://explorer.aptoslabs.com/txn/${evm}?network=${network}`),
      ...['dcrdata.decred.org', 'testnet.dcrdata.org'].map(host => `https://${host}/tx/${hash}`),
      ...['tonviewer.com', 'testnet.tonviewer.com'].map(host => `https://${host}/transaction/${hash}`),
    ];
    for (const url of urls) expect(safeExplorerURL(url), url).toBe(true);
  });
  it('rejects arbitrary protocols, domains, credentials, encodings and transaction substitutions', () => {
    const known = `https://etherscan.io/tx/${evm}`;
    const urls = ['', 'not a URL', `http://etherscan.io/tx/${evm}`, `javascript:alert(1)`, 'file:///tmp/wallet', `data:text/html,<script>alert(1)</script>`, `//etherscan.io/tx/${evm}`,
      `https://etherscan.io.attacker.invalid/tx/${evm}`, `https://etherscan.io@attacker.invalid/tx/${evm}`, `https://user:pass@etherscan.io/tx/${evm}`, `https://etherscan.io:443/tx/${evm}`, `https://etherscan.io:444/tx/${evm}`, `https://ETHERSCAN.IO/tx/${evm}`, ` ${known}`, `${known}\n`, `${known}#fragment`, `${known}?redirect=https://attacker.invalid`, `${known}/`, `https://etherscan.io/address/${evm}`, `https://etherscan.io/tx/${evm.toUpperCase()}`, `https://etherscan.io/tx/${evm.slice(0, -1)}`, `https://etherscan.io/tx/${evm}1`, `https://etherscan.io/tx/%30x${hash}`, `https://etherscan.io/tx/../tx/${evm}`, `https:\\etherscan.io\\tx\\${evm}`,
      `https://app.hyperliquid.xyz/tx/${evm}`, `https://nearblocks.io/tx/${'1'.repeat(32)}`, `https://nearblocks.io/txns/${'1'.repeat(31)}`, `https://nearblocks.io/txns/${'1'.repeat(33)}`, `https://nearblocks.io/txns/${'1'.repeat(45)}`, `https://nearblocks.io/txns/${'O'.repeat(32)}`, `https://nearblocks.io/txns/${'z'.repeat(44)}`,
      `https://explorer.aptoslabs.com/txn/${evm}`, `https://explorer.aptoslabs.com/txn/${evm}?network=local`, `https://explorer.aptoslabs.com/txn/${evm}?network=mainnet&redirect=evil`, `https://explorer.aptoslabs.com/tx/${evm}?network=mainnet`, `https://tonviewer.com/transaction/0x${hash}`, `https://testnet.dcrdata.org/tx/${hash.toUpperCase()}`];
    for (const url of urls) expect(safeExplorerURL(url), url).toBe(false);
  });
});
