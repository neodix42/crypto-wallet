// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ExplorerLink, TransactionTable } from './TransactionTable';
import { node } from './test-fixtures';
import type { Node } from './types';

const hash = '0x' + 'ab'.repeat(32), url = 'https://etherscan.io/tx/' + hash;
let host: HTMLDivElement, root: Root, style: HTMLStyleElement;
function transaction(source = '0x' + '1'.repeat(40), destination = '0x' + '2'.repeat(40), comment = ''): Node {
  const row = node('row', 'public-transaction');
  row.Children = [node('label', '', '2026-11-04 10:11:12 UTC'), node('address', '', '', source), node('address', '', '', destination), node('label', '', '0.1 USDT'), node('explorer', '', hash, url), node('label', '', comment), node('label', '', 'OBSERVED')];
  const table = node('transactionTable', 'public-history', 'Transaction history'); table.Children = [row]; return table;
}
async function render(table: Node) { await act(async () => { root.render(<TransactionTable node={table}/>); }); }
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  style = document.createElement('style'); style.textContent = readFileSync('src/styles.css', 'utf8'); document.head.append(style);
  window.runtime = { EventsOn: vi.fn(() => () => {}), BrowserOpenURL: vi.fn() };
});
afterEach(async () => { await act(async () => { root.unmount(); }); style.remove(); });

describe('compact transaction details', () => {
  it('keeps Explorer icon-only with its full safe URL tooltip and full transaction accessible name', async () => {
    const table = transaction(); await render(table);
    const cell = host.querySelector('tbody td:nth-child(5)')!, link = cell.querySelector<HTMLButtonElement>('button')!;
    expect(link.type).toBe('button'); expect(link.title).toBe(url);
    expect(link.getAttribute('aria-label')).toBe(`View transaction ${hash} in external explorer`);
    expect(link.textContent).toBe(''); expect(cell.textContent).toBe('');
    expect(link.querySelector('svg')!.getAttribute('aria-hidden')).toBe('true');
    expect(host.textContent).not.toContain(hash); expect(host.textContent).not.toContain(url);
    expect(host.querySelector('[href],script,img')).toBeNull();
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
    await act(async () => { link.click(); });
    expect(window.runtime!.BrowserOpenURL).toHaveBeenCalledExactlyOnceWith(url);
  });
  it('keeps the native button keyboard focus and Enter/Space behavior without intercepting keys', async () => {
    await render(transaction());
    const link = host.querySelector<HTMLButtonElement>('.explorer-link')!;
    link.focus(); expect(document.activeElement).toBe(link); expect(link.tabIndex).toBe(0);
    for (const key of ['Enter', ' ']) {
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
      link.dispatchEvent(event); expect(event.defaultPrevented).toBe(false);
    }
    // happy-dom does not synthesize keyboard default actions. The native
    // activation event below exercises the button's shared click path.
    await act(async () => { link.click(); });
    expect(window.runtime!.BrowserOpenURL).toHaveBeenCalledExactlyOnceWith(url);
    const table = transaction(); table.Children![0].Children![4].Disabled = true; await render(table);
    const disabled = host.querySelector<HTMLButtonElement>('.explorer-link')!;
    expect(disabled.disabled).toBe(true); await act(async () => { disabled.click(); });
    expect(window.runtime!.BrowserOpenURL).toHaveBeenCalledTimes(1);
  });
  it('never exposes hostile or missing explorer URLs as links, transaction text or runtime calls', async () => {
    for (const value of ['', 'javascript:alert(1)', 'file:///tmp/wallet', 'http://etherscan.io/tx/' + hash, 'https://attacker.invalid/tx/' + hash, url + '?redirect=evil', 'https://user:pass@etherscan.io/tx/' + hash]) {
      const table = transaction(); table.Children![0].Children![4].Value = value; await render(table);
      const cell = host.querySelector('tbody td:nth-child(5)')!;
      expect(cell.querySelector('button,a,[href]')).toBeNull(); expect(cell.textContent).toBe('—');
      expect(cell.querySelector('[aria-label]')!.getAttribute('aria-label')).toBe('Transaction explorer unavailable');
      expect(cell.textContent).not.toContain(hash); expect(cell.textContent).not.toContain(value || hash);
      cell.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    }
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
  });
  it('keeps the Explorer and Comment column contracts even when a future payload uses another node kind', async () => {
    const table = transaction(), cells = table.Children![0].Children!;
    for (const kind of ['label', 'address', 'button'] as const) {
      cells[4] = node(kind, '', url + '\n' + hash, '');
      cells[5] = node('address', '', 'Exact blockchain memo\nSecond line', 'unrelated address payload');
      await render(table);
      const explorer = host.querySelector('.transaction-explorer-cell')!, comment = host.querySelector<HTMLTableCellElement>('.transaction-comment-cell')!;
      expect(explorer.textContent).toBe('—'); expect(explorer.querySelector('button,a,code,[href]')).toBeNull();
      expect(comment.textContent).toBe('Exact blockchain memo\nSecond line'); expect(comment.title).toBe('Exact blockchain memo\nSecond line');
      expect(comment.querySelector('code,button,a')).toBeNull(); expect(comment.textContent).not.toContain('unrelated address payload');
    }
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
  });
  it('checks the current URL and disabled flag again when an already rendered control is activated', async () => {
    const explorer = node('explorer', '', hash, url);
    await act(async () => { root.render(<ExplorerLink node={explorer}/>); });
    const link = host.querySelector<HTMLButtonElement>('button')!;
    explorer.Value = 'https://attacker.invalid/tx/' + hash;
    await act(async () => { link.click(); }); expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
    explorer.Value = url; explorer.Disabled = true;
    await act(async () => { link.click(); }); expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
  });
  it('shows first/last six address characters while retaining exact full hover and accessible values', async () => {
    const addresses = ['0x73410EA1FcbE1b8bCA883762dC56beF46403A469', 'UQAomnBshnaKi0Uu645Qg-hXScddgZwXa2U__ba7kf8UvZ05', '6066e22782257c3edaca6220990bfeda37eb2926ceb034b4d75c2bc1e3f8958d', 'validator.pool.near'];
    const expected = ['0x7341 . . . 03A469', 'UQAomn . . . 8UvZ05', '6066e2 . . . f8958d', 'valida . . . l.near'];
    for (let i = 0; i < addresses.length; i++) {
      const table = transaction(addresses[i], addresses[i]), original = JSON.stringify(table); await render(table);
      for (const [index, context] of [[1, 'Source'], [2, 'Destination']] as const) {
        const cell = host.querySelectorAll('tbody td')[index], code = cell.querySelector('code')!;
        expect(code.textContent).toBe(expected[i]); expect(code.title).toBe(addresses[i]); expect(cell.getAttribute('title')).toBe(addresses[i]);
        expect(cell.getAttribute('aria-label')).toBe(`${context} address: ${addresses[i]}`);
        expect(code.getAttribute('aria-hidden')).toBe('true'); expect(getComputedStyle(code).whiteSpace).toBe('nowrap');
      }
      expect(JSON.stringify(table)).toBe(original);
    }
  });
  it('leaves short addresses and unknown provider states unchanged instead of inventing address fragments', async () => {
    for (const value of ['', '—', 'Unknown', 'alice.near', 'unavailable-public-address-not-observed', 'Unknown source address from this provider']) {
      await render(transaction(value, value));
      for (const code of host.querySelectorAll('.transaction-address')) expect(code.textContent).toBe(value);
    }
  });
  it('clips long multiline comments to one visual line with the complete exact escaped comment on hover', async () => {
    const comment = '<img src=x onerror=alert(1)>\r\nactual on-chain memo  ' + 'long checked comment '.repeat(100);
    const table = transaction(undefined, undefined, comment), original = JSON.stringify(table); await render(table);
    const cell = host.querySelector<HTMLTableCellElement>('.transaction-comment-cell')!, text = cell.querySelector<HTMLSpanElement>('.transaction-comment')!;
    expect(cell.textContent).toBe(comment); expect(cell.title).toBe(comment); expect(text.title).toBe(comment); expect(text.getAttribute('aria-label')).toBe(comment);
    expect(host.querySelector('img,script')).toBeNull();
    const computed = getComputedStyle(text);
    expect(computed.whiteSpace).toBe('nowrap'); expect(computed.overflow).toBe('hidden'); expect(computed.textOverflow).toBe('ellipsis'); expect(computed.maxWidth).toBe('28ch');
    expect(getComputedStyle(cell).whiteSpace).toBe('nowrap'); expect(JSON.stringify(table)).toBe(original);
  });
  it('does not substitute UI descriptions for an empty comment or remove actual blockchain text resembling UI labels', async () => {
    const table = transaction(); table.Children![0].Children![5].Tooltip = 'Local UI planned effect: send funds'; await render(table);
    let cell = host.querySelector<HTMLTableCellElement>('.transaction-comment-cell')!;
    expect(cell.textContent).toBe(''); expect(cell.title).toBe(''); expect(host.textContent).not.toContain('Local UI planned effect');
    const actual = 'Planned effect: user wrote this exact blockchain memo'; table.Children![0].Children![5].Text = actual; await render(table);
    cell = host.querySelector<HTMLTableCellElement>('.transaction-comment-cell')!;
    expect(cell.textContent).toBe(actual); expect(cell.title).toBe(actual);
  });
});
