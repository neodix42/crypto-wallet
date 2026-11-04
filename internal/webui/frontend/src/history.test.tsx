// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { PublicContent, PublicNode, WalletShell } from './PublicApp';
import { node, view } from './test-fixtures';
import type { Node } from './types';

const styles = readFileSync('src/styles.css', 'utf8');

let root: Root | undefined, host: HTMLDivElement;
const txid = '0x' + 'a'.repeat(64), url = 'https://etherscan.io/tx/' + txid;
function history(): Node {
  const row = node('row', 'walletHistory/owned/tx/effect');
  row.Children = [node('label', '', '2026-10-30 10:01:02 UTC'), node('address', '', '', '0x' + '1'.repeat(40)), node('address', '', '', 'recipient-with-a-complete-97-character-address-' + 'x'.repeat(97)), node('label', '', '123456789.000000000000000001 ETH'), node('explorer', '', txid, url), node('label', '', '<img src=x onerror=alert(1)>\nsecond line'), node('label', '', 'OBSERVED')];
  const table = node('transactionTable', 'walletHistory/owned', 'Transaction history'); table.Children = [row]; return table;
}
async function mount(element: React.ReactNode) { await act(async () => { root = createRoot(host); root.render(element); }); }
async function rerender(element: React.ReactNode) { await act(async () => { root!.render(element); }); }
async function click(element: Element) { await act(async () => { element.dispatchEvent(new MouseEvent('click', { bubbles: true })); }); }
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host);
  window.runtime = { EventsOn: vi.fn(() => () => {}), BrowserOpenURL: vi.fn() };
});
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; window.getSelection()?.removeAllRanges(); });

describe('transaction table and expanded main pages', () => {
  it('keeps every original-operation status inert without review, confirmation or broadcast controls', async () => {
    const emit = vi.fn();
    for (const state of ['PREPARED', 'SIGNED_NOT_SUBMITTED', 'UNKNOWN', 'SUBMITTED', 'SUBMITTING', 'REORGED', 'CONFIRMED']) {
      const table = history(); table.Children![0].Children![6].Text = state;
      if (!root) await mount(<PublicNode node={table} emit={emit}/>); else await rerender(<PublicNode node={table} emit={emit}/>);
      expect(host.querySelectorAll('tbody td')).toHaveLength(7);
      expect(host.querySelector('tbody td:last-child')!.textContent).toBe(state);
      expect(host.querySelector('button:not(.explorer-link),input,select,[data-action]')).toBeNull();
      await click(host.querySelector('tbody td:last-child')!);
      expect(emit).not.toHaveBeenCalled();
    }
  });
  it('places Max beside the Amount input in one nonwrapping row with both controls bottom aligned', async () => {
    const style = document.createElement('style'); style.textContent = styles; document.head.append(style);
    try {
      const amount = node('entry', 'field/amount/owned', 'Amount (1 means 1 ETH)', '1.5'), max = node('button', 'max/owned', 'Max');
      const row = node('row', 'sendAmount/owned'); row.Children = [amount, max];
      const emit = vi.fn(); await mount(<PublicNode node={row} emit={emit}/>);
      const container = host.querySelector('.send-amount-field')!, field = container.querySelector('label')!, button = container.querySelector('button')!;
      expect(container.children).toHaveLength(2);
      expect(container.children[0]).toBe(field); expect(container.children[1]).toBe(button);
      expect(field.querySelector('input')!.value).toBe('1.5'); expect(button.textContent).toBe('Max');
      expect(getComputedStyle(container).display).toBe('grid');
      expect(getComputedStyle(container).gridTemplateColumns).toBe('minmax(0,1fr) auto');
      expect(getComputedStyle(container).alignItems).toBe('end');
      expect(getComputedStyle(field).margin).toBe('0px');
      expect(getComputedStyle(button).alignSelf).toBe('end');
      await click(button); expect(emit.mock.calls).toEqual([['max/owned']]);
      max.Disabled = true; await rerender(<PublicNode node={row} emit={emit}/>);
      await click(host.querySelector('button')!); expect(emit).toHaveBeenCalledTimes(1);
    } finally { style.remove(); }
  });
  it('keeps an invalid Amount input aligned with Max and places its single accessible error below both controls', async () => {
    const style = document.createElement('style'); style.textContent = styles; document.head.append(style);
    try {
      const amount = { ...node('entry', 'field/amount/owned', 'Amount', '1e3'), Error: 'Use a positive decimal amount. <img src=x>', InputMode: 'decimal' as const };
      const row = node('row', 'sendAmount/owned'); row.Children = [amount, node('button', 'max/owned', 'Max')];
      const emit = vi.fn(); await mount(<PublicNode node={row} emit={emit}/>);
      const container = host.querySelector('.send-amount-field')!, field = container.querySelector('label')!, input = field.querySelector('input')!, button = container.querySelector('button')!;
      const error = container.querySelector('.send-amount-error')!;
      expect([...container.children]).toEqual([field, button, error]);
      expect(field.querySelector('.field-error')).toBeNull(); expect(container.querySelectorAll('[role="alert"]')).toHaveLength(1);
      expect(input.getAttribute('aria-invalid')).toBe('true'); expect(input.getAttribute('aria-describedby')).toBe(error.id);
      expect(document.getElementById(error.id)).toBe(error); expect(error.textContent).toBe(amount.Error); expect(error.querySelector('img')).toBeNull();
      expect(getComputedStyle(container).alignItems).toBe('end'); expect(getComputedStyle(button).alignSelf).toBe('end');
      expect(getComputedStyle(error).gridColumn).toBe('1 / -1'); expect(input.value).toBe('1e3');
      await click(button); expect(emit.mock.calls).toEqual([['max/owned']]);
      amount.Error = ''; amount.Value = '1.25'; await rerender(<PublicNode node={row} emit={emit}/>);
      expect(container.children).toHaveLength(2); expect(container.querySelector('[role="alert"]')).toBeNull();
      expect(input.hasAttribute('aria-describedby')).toBe(false); expect(input.hasAttribute('aria-invalid')).toBe(false); expect(input.value).toBe('1.25');
    } finally { style.remove(); }
  });
  it('falls back to ordinary fields with their inline errors for malformed or foreign-owner Amount/Max row shapes', async () => {
    const amount = { ...node('entry', 'field/amount/owned', 'Amount', 'bad'), Error: 'Invalid amount' }, maximum = node('button', 'max/owned', 'Max');
    const row = node('row', 'sendAmount/owned');
    for (const children of [[amount], [maximum, amount], [amount, { ...maximum, ID: 'max/another' }], [amount, maximum, node('label', '', 'Extra')]]) {
      row.Children = children; if (!root) await mount(<PublicNode node={row} emit={() => {}}/>); else await rerender(<PublicNode node={row} emit={() => {}}/>);
      expect(host.querySelector('.send-amount-field')).toBeNull(); expect(host.querySelector('.node-row')).not.toBeNull();
      const input = host.querySelector('input')!, error = host.querySelector('.field-error')!;
      expect(error.parentElement!.tagName).toBe('LABEL'); expect(input.getAttribute('aria-describedby')).toBe(error.id);
      expect(host.querySelectorAll('[role="alert"]')).toHaveLength(1); expect(input.value).toBe('bad');
    }
  });
  it('renders inert compact addresses with complete original values in hover and accessible context', async () => {
    const table = history(), emit = vi.fn();
    await mount(<PublicNode node={table} emit={emit}/>);
    expect(host.querySelector('caption')!.textContent).toBe('Transaction history');
    expect([...host.querySelectorAll('th')].map(cell => cell.textContent)).toEqual(['Date and time', 'Source', 'Destination', 'Amount', 'Explorer', 'Comment', 'Status']);
    const cells = [...host.querySelectorAll('tbody td')];
    expect(cells.map(cell => cell.textContent)).toEqual(['2026-10-30 10:01:02 UTC', '0x1111 . . . 111111', 'recipi . . . xxxxxx', '123456789.000000000000000001 ETH', '', '<img src=x onerror=alert(1)>\nsecond line', 'OBSERVED']);
    expect(cells[1].getAttribute('title')).toBe(table.Children![0].Children![1].Value);
    expect(cells[2].getAttribute('title')).toBe(table.Children![0].Children![2].Value);
    expect(cells[2].getAttribute('aria-label')).toBe('Destination address: ' + table.Children![0].Children![2].Value);
    expect(host.querySelector('img,script,a')).toBeNull();
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
    const code = cells[2].querySelector('code')!, range = document.createRange(), selection = window.getSelection()!;
    range.selectNodeContents(code); selection.addRange(range);
    expect(selection.toString()).toBe('recipi . . . xxxxxx');
    for (const key of [new KeyboardEvent('keydown', { key: 'c', ctrlKey: true, bubbles: true, cancelable: true }), new Event('copy', { bubbles: true, cancelable: true })]) {
      await act(async () => { code.dispatchEvent(key); }); expect(key.defaultPrevented).toBe(false);
    }
    expect(emit).not.toHaveBeenCalled();
    const region = host.querySelector('[role="region"]')!;
    expect(region.getAttribute('tabindex')).toBe('0');
  });
  it('opens a reviewed transaction only on explicit click through ordinary browser runtime, without wallet events', async () => {
    const emit = vi.fn(); await mount(<PublicNode node={history()} emit={emit}/>);
    const link = host.querySelector('.explorer-link')!;
    expect(link.getAttribute('title')).toBe(url);
    await click(link); expect(window.runtime!.BrowserOpenURL).toHaveBeenCalledExactlyOnceWith(url);
    expect(emit).not.toHaveBeenCalled();
    delete window.runtime!.BrowserOpenURL;
    await click(link); // Missing runtime cannot navigate the wallet or emit a payment event.
    delete window.runtime;
    await click(link);
    expect(emit).not.toHaveBeenCalled();
  });
  it('renders missing, hostile and unreviewed explorer links as inert unavailable markers, retaining other history values', async () => {
    for (const value of ['', 'javascript:alert(1)', 'https://attacker.invalid/tx/' + txid, url + '?redirect=evil']) {
      const table = history(); table.Children![0].Children![4].Value = value;
      if (!root) await mount(<PublicNode node={table} emit={() => {}}/>); else await rerender(<PublicNode node={table} emit={() => {}}/>);
      expect(host.querySelector('.explorer-link,a')).toBeNull();
      expect(host.querySelector('.transaction-explorer-unavailable')!.textContent).toBe('—');
      expect(host.textContent).not.toContain(txid);
      expect(host.textContent).toContain('OBSERVED');
    }
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
  });
  it('preserves disabled links, empty history and fallback captions without claiming no earlier activity', async () => {
    const table = history(); table.Children![0].Children![4].Disabled = true;
    await mount(<PublicNode node={table} emit={() => {}}/>);
    expect(host.querySelector<HTMLButtonElement>('.explorer-link')!.disabled).toBe(true);
    await click(host.querySelector('.explorer-link')!);
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
    table.Text = ''; table.Children = null;
    await rerender(<PublicNode node={table} emit={() => {}}/>);
    expect(host.querySelector('caption')!.textContent).toBe('Transaction history');
    expect(host.querySelectorAll('tbody tr')).toHaveLength(0);
    expect(host.textContent).not.toMatch(/no transactions|complete history|no earlier/i);
    await rerender(<PublicNode node={node('explorer', '', txid, '')} emit={() => {}}/>);
    expect(host.textContent).toBe('—');
  });
  it('separates icon-only utilities in the header from account/wallet tabs, preserving selection and actions', async () => {
    const state = view(); state.Active = 'settings';
    state.Pages!.unshift({ ID: 'settings', Title: 'Settings', Nodes: [] });
    state.Pages!.push({ ID: 'addressbook', Title: 'Address book', Nodes: [] }, { ID: 'wallet/eth', Title: 'ETH', Nodes: [] }, { ID: 'exchange', Title: 'Exchange', Nodes: [] });
    const emit = vi.fn(); await mount(<WalletShell view={state} status="" emit={emit}/>);
    expect([...host.querySelectorAll('.tabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['Main account']);
    expect([...host.querySelectorAll('.wallet-subtabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['ETH']);
    expect([...host.querySelectorAll('.app-header [role="tab"]')].map(tab => tab.getAttribute('aria-label'))).toEqual(['Exchange', 'Address book', 'Settings']);
    expect(host.querySelector('.utility-navigation')!.textContent).toBe('');
    expect(host.querySelector('[role="tab"][aria-selected="true"]')!.getAttribute('aria-label')).toBe('Settings');
    expect(host.querySelector('.app-identity .build-metadata')!.textContent).toBe('0.1.1 · mainnet');
    expect(host.querySelector('.app-header')!.lastElementChild!.className).toBe('utility-navigation');
    expect([...host.querySelectorAll('.utility-navigation button')].every(button => button.querySelector('svg') && button.getAttribute('title') === button.getAttribute('aria-label'))).toBe(true);
    expect(host.querySelector('main')!.getAttribute('aria-label')).toBe('Settings');
    await click(host.querySelector('[role="tab"][aria-selected="true"]')!);
    expect(emit.mock.calls).toEqual([['tab', 'settings']]);
    state.Pages = state.Pages!.filter(page => page.ID !== 'exchange' && page.ID !== 'addressbook');
    await rerender(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('.utility-navigation [role="tab"]')!.getAttribute('aria-label')).toBe('Settings');
  });
  it('uses the same full-width main card for wallet, Settings, Address book and Exchange, retaining scrollable full values', async () => {
    const style = document.createElement('style'); style.textContent = styles; document.head.append(style);
    try {
      for (const ID of ['wallet/eth', 'settings', 'addressbook', 'exchange']) {
        const content = <PublicContent page={{ ID, Title: ID, Nodes: [history()] }} emit={() => {}}/>;
        if (!root) await mount(content); else await rerender(content);
        const card = host.querySelector('.content-card')!, computed = getComputedStyle(card);
        expect(computed.width).toBe('100%'); expect(parseFloat(computed.minWidth)).toBe(0); expect(computed.maxWidth).not.toBe('1170px');
        expect(getComputedStyle(host.querySelector('.transaction-table-wrap')!).overflowX).toBe('auto');
        expect(getComputedStyle(host.querySelector('.transaction-table .address')!).whiteSpace).toBe('nowrap');
        expect(getComputedStyle(host.querySelector('.transaction-table .transaction-comment')!).whiteSpace).toBe('nowrap');
        expect(getComputedStyle(host.querySelector('.transaction-table .transaction-comment')!).textOverflow).toBe('ellipsis');
      }
    } finally { style.remove(); }
  });
});
