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
function originalHistory(state: string, kinds: string[]): Node {
  const table = history(), status = node('column', 'original-status', state, 'original-plan');
  status.Children = [node('label', '', state + ' · provider: OBSERVED'), ...kinds.map(kind => node('button', `${kind}/original-plan`, kind === 'retry' ? 'Broadcast original signed operation' : kind === 'reconcile' ? 'Check status' : kind === 'sendOperation' ? 'Review' : 'Cancel prepared'))];
  table.Children![0].Children![6] = status; return table;
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
  it('offers compact original-operation controls only after an explicit click, preserving the original ID', async () => {
    const emit = vi.fn();
    for (const [state, kinds] of [['PREPARED', ['sendOperation', 'cancelPrepared']], ['SIGNED_NOT_SUBMITTED', ['retry', 'reconcile']], ['UNKNOWN', ['retry', 'reconcile']], ['SUBMITTED', ['reconcile']], ['SUBMITTING', ['reconcile']], ['REORGED', ['reconcile']]] as [string, string[]][]) {
      const table = originalHistory(state, kinds);
      if (!root) await mount(<PublicNode node={table} emit={emit}/>); else await rerender(<PublicNode node={table} emit={emit}/>);
      expect(host.querySelectorAll('tbody td')).toHaveLength(7);
      expect(host.querySelectorAll('.transaction-actions')).toHaveLength(1);
      expect(host.querySelector('.transaction-status > span')!.textContent).toBe(state + ' · provider: OBSERVED');
      const before = emit.mock.calls.length;
      expect(host.querySelectorAll('.transaction-actions button')).toHaveLength(kinds.length);
      for (const kind of kinds) await click(host.querySelector(`[data-action="${kind}/original-plan"]`)!);
      expect(emit.mock.calls.slice(before)).toEqual(kinds.map(kind => [`${kind}/original-plan`]));
      expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
    }
    const before = emit.mock.calls.length;
    await rerender(<PublicNode node={history()} emit={emit}/>);
    expect(host.querySelector('.transaction-actions')).toBeNull();
    expect(host.textContent).toContain('OBSERVED');
    expect(emit.mock.calls).toHaveLength(before);
  });
  it('respects every disabled ancestor and replaced snapshot without treating status text as an action', async () => {
    const emit = vi.fn();
    for (const disabled of ['table', 'row', 'status', 'button']) {
      const table = originalHistory('UNKNOWN', ['retry', 'reconcile']);
      const row = table.Children![0], status = row.Children![6];
      if (disabled === 'table') table.Disabled = true;
      if (disabled === 'row') row.Disabled = true;
      if (disabled === 'status') status.Disabled = true;
      if (disabled === 'button') status.Children![1].Disabled = true;
      if (!root) await mount(<PublicNode node={table} emit={emit}/>); else await rerender(<PublicNode node={table} emit={emit}/>);
      const retry = host.querySelector<HTMLButtonElement>('[data-action="retry/original-plan"]')!;
      expect(retry.disabled).toBe(true); await click(retry);
      await click(host.querySelector('.transaction-status > span')!);
      expect(emit).not.toHaveBeenCalled();
    }
    const observed = history(); observed.Children![0].Children![6].Text = '<button data-action="retry/replacement">UNKNOWN</button>';
    await rerender(<PublicNode node={observed} emit={emit}/>);
    expect(host.querySelector('[data-action]')).toBeNull();
    expect(host.textContent).toContain('<button data-action="retry/replacement">UNKNOWN</button>');
    expect(emit).not.toHaveBeenCalled();
  });
  it('renders every transfer field exactly as inert text with complete selectable addresses', async () => {
    const table = history(), emit = vi.fn();
    await mount(<PublicNode node={table} emit={emit}/>);
    expect(host.querySelector('caption')!.textContent).toBe('Transaction history');
    expect([...host.querySelectorAll('th')].map(cell => cell.textContent)).toEqual(['Date and time', 'Source', 'Destination', 'Amount', 'Explorer', 'Comment', 'Status']);
    const cells = [...host.querySelectorAll('tbody td')];
    expect(cells.map(cell => cell.textContent)).toEqual(table.Children![0].Children!.map(cell => cell.Kind === 'address' ? cell.Value : cell.Kind === 'explorer' ? cell.Text + '↗' : cell.Text));
    expect(host.querySelector('img,script,a')).toBeNull();
    expect(window.runtime!.BrowserOpenURL).not.toHaveBeenCalled();
    const code = cells[2].querySelector('code')!, range = document.createRange(), selection = window.getSelection()!;
    range.selectNodeContents(code); selection.addRange(range);
    expect(selection.toString()).toBe(table.Children![0].Children![2].Value);
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
  it('renders missing, hostile and unreviewed explorer links as plain transaction identity, retaining other history values', async () => {
    for (const value of ['', 'javascript:alert(1)', 'https://attacker.invalid/tx/' + txid, url + '?redirect=evil']) {
      const table = history(); table.Children![0].Children![4].Value = value;
      if (!root) await mount(<PublicNode node={table} emit={() => {}}/>); else await rerender(<PublicNode node={table} emit={() => {}}/>);
      expect(host.querySelector('.explorer-link,a')).toBeNull();
      expect(host.querySelector('.transaction-id')!.textContent).toBe(txid);
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
    expect(host.textContent).toBe(txid);
  });
  it('puts Settings last at the right utility edge while preserving selected identities and exact tab actions', async () => {
    const state = view(); state.Active = 'settings';
    state.Pages!.unshift({ ID: 'settings', Title: 'Settings', Nodes: [] });
    state.Pages!.push({ ID: 'addressbook', Title: 'Address book', Nodes: [] }, { ID: 'wallet/eth', Title: 'ETH', Nodes: [] }, { ID: 'exchange', Title: 'Exchange', Nodes: [] });
    const emit = vi.fn(); await mount(<WalletShell view={state} status="" emit={emit}/>);
    expect([...host.querySelectorAll('[role="tab"]')].map(tab => tab.textContent)).toEqual(['Main account', 'ETH', 'Exchange', 'Address book', 'Settings']);
    expect(host.querySelector('.utility-start [role="tab"]')!.textContent).toBe('Exchange');
    expect(host.querySelector('[role="tab"][aria-selected="true"]')!.textContent).toBe('Settings');
    expect(host.querySelector('main')!.getAttribute('aria-label')).toBe('Settings');
    await click(host.querySelector('[role="tab"][aria-selected="true"]')!);
    expect(emit.mock.calls).toEqual([['tab', 'settings']]);
    state.Pages = state.Pages!.filter(page => page.ID !== 'exchange' && page.ID !== 'addressbook');
    await rerender(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('.utility-start [role="tab"]')!.textContent).toBe('Settings');
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
        expect(getComputedStyle(host.querySelector('.transaction-table td:nth-child(6)')!).overflowWrap).toBe('anywhere');
      }
    } finally { style.remove(); }
  });
});
