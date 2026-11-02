// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Field, PublicContent, PublicNode, WalletShell } from './PublicApp';
import { validView } from './protocol';
import { node, view } from './test-fixtures';
import type { Node, Page, View } from './types';
import { readFileSync } from 'node:fs';
const styles = readFileSync('src/styles.css', 'utf8');

let root: Root | undefined, host: HTMLDivElement;
async function render(element: React.ReactNode) { await act(async () => { if (!root) root = createRoot(host); root.render(element); }); }
async function click(element: Element | null) { expect(element).not.toBeNull(); await act(async () => { element!.dispatchEvent(new MouseEvent('click', { bubbles: true })); }); }
function row(id: string): Node {
  const item = node('row', 'open/' + id);
  item.Children = [node('button', 'open/' + id, id), node('label', '', 'Unknown'), node('address', '', '', '0x' + '1'.repeat(40)), node('button', 'copyReceive/' + id, 'Copy', '0x' + '1'.repeat(40))];
  return item;
}
function account(index: number, count: number, rows: Node[]): Page {
  const selector = node('combo', 'field/group', 'Account group'); selector.Options = [{ ID: String(index), Text: 'Account ' + index }];
  const actions = node('row'); actions.Children = [node('button', 'refresh', 'Refresh wallet state'), node('button', 'lock', 'Lock wallet')];
  return { ID: 'group/' + index, Title: index ? 'Account ' + index : 'Main account', Nodes: [selector, node('label', 'networkCount', count + ' networks'), node('label', 'portfolioValuation', 'Balance: 25$'), actions, ...rows] };
}
function accounts(): View {
  return { ...view(), AccountGroupID: 'group/1', Active: 'wallet/near-1', Pages: [account(0, 8, [row('eth-0')]), account(1, 7, [row('near-1')]), { ID: 'wallet/eth-0', Title: 'Ethereum', ParentID: 'group/0', Nodes: [] }, { ID: 'wallet/near-1', Title: 'NEAR', ParentID: 'group/1', Nodes: [] }, { ID: 'wallet/base-1', Title: 'Base', ParentID: 'group/1', Nodes: [] }, ...['exchange', 'addressbook', 'settings'].map(ID => ({ ID, Title: ID === 'addressbook' ? 'Address book' : ID[0].toUpperCase() + ID.slice(1), Nodes: [] }))] };
}
beforeEach(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }); document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host); });
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; });

describe('account-owned wallet navigation and controls', () => {
  it('opens one Exchange tab beside account names and keeps it while navigating accounts', async () => {
    const state = accounts(), emit = vi.fn(); await render(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('.account-tabs .exchange-tab')).toBeNull();
    await click(host.querySelector('.utility-navigation [aria-label="Exchange"]'));
    expect(emit.mock.calls).toEqual([['tab', 'exchange']]);
    state.Active = 'exchange'; await render(<WalletShell view={state} status="" emit={emit}/>);
    expect([...host.querySelectorAll('.account-tabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['Main account', 'Account 1', 'Exchange']);
    expect(host.querySelectorAll('.account-tabs [aria-selected="true"]')).toHaveLength(1);
    expect(host.querySelector('.account-tabs [aria-selected="true"]')!.textContent).toBe('Exchange');
    expect(host.querySelector('main')!.getAttribute('aria-label')).toBe('Exchange');
    expect(host.querySelector('.wallet-subtabs')).toBeNull();
    await click(host.querySelector('.utility-navigation [aria-label="Exchange"]'));
    expect(host.querySelectorAll('.exchange-tab')).toHaveLength(1);
    state.Active = 'group/0'; state.AccountGroupID = 'group/0'; await render(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('.exchange-tab')).not.toBeNull();
    expect(host.querySelector('.account-tabs [aria-selected="true"]')!.textContent).toBe('Main account');
    await click(host.querySelector('.exchange-tab [role="tab"]'));
    expect(emit.mock.calls.at(-1)).toEqual(['tab', 'exchange']);
  });
  it('closes the active Exchange tab to its remembered account and allows reopening', async () => {
    const state = accounts(); state.Active = 'exchange'; const emit = vi.fn();
    await render(<WalletShell view={state} status="" emit={emit}/>);
    await click(host.querySelector('[aria-label="Close Exchange"]'));
    expect(emit.mock.calls).toEqual([['tab', 'group/1']]);
    expect(host.querySelector('.exchange-tab')).toBeNull();
    state.Active = 'group/1'; await render(<WalletShell view={state} status="" emit={emit}/>);
    await click(host.querySelector('.utility-navigation [aria-label="Exchange"]'));
    expect(host.querySelectorAll('.exchange-tab')).toHaveLength(1);
  });
  it('middle-closes an inactive Exchange tab without changing wallet selection and clears it on lock', async () => {
    const state = accounts(); state.Active = 'exchange'; const emit = vi.fn();
    await render(<WalletShell view={state} status="" emit={emit}/>);
    state.Active = 'wallet/near-1'; await render(<WalletShell view={state} status="" emit={emit}/>);
    for (const button of [0, 2]) await act(async () => { host.querySelector('.exchange-tab')!.dispatchEvent(new MouseEvent('auxclick', { button, bubbles: true, cancelable: true })); });
    expect(host.querySelector('.exchange-tab')).not.toBeNull();
    await act(async () => { host.querySelector('.exchange-tab')!.dispatchEvent(new MouseEvent('auxclick', { button: 1, bubbles: true, cancelable: true })); });
    expect(host.querySelector('.exchange-tab')).toBeNull(); expect(emit).not.toHaveBeenCalled();
    await click(host.querySelector('.utility-navigation [aria-label="Exchange"]'));
    expect(host.querySelector('.exchange-tab')).not.toBeNull();
    const locked = accounts(); locked.Active = 'group/0'; locked.Pages = locked.Pages!.filter(page => page.ID !== 'exchange');
    await render(<WalletShell view={locked} status="" emit={emit}/>);
    await render(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('.exchange-tab')).toBeNull();
  });
  it('moves authoritative network count and lock beside utility icons while keeping refresh inside the table header', async () => {
    const state = accounts(); state.Active = 'group/1'; const emit = vi.fn();
    await render(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('.utility-navigation')!.firstElementChild!.textContent).toBe('7 networks');
    expect([...host.querySelectorAll('.utility-navigation button')].map(button => button.getAttribute('aria-label'))).toEqual(['Exchange', 'Address book', 'Lock wallet', 'Settings']);
    expect(host.querySelector('.utility-navigation [aria-label="Lock wallet"] svg')).not.toBeNull();
    expect(host.querySelectorAll('[aria-label="Lock wallet"]')).toHaveLength(1);
    expect(host.querySelector('main select')).toBeNull(); expect(host.querySelector('main')!.textContent).not.toContain('Account group');
    expect(host.querySelector('main')!.textContent).toContain('Balance: 25$'); expect(host.querySelector('main')!.textContent).not.toContain('7 networks');
    const refresh = host.querySelector('thead th:last-child [aria-label="Refresh wallet state"]');
    expect(refresh!.textContent).toBe(''); expect(host.querySelectorAll('[data-action="refresh"]')).toHaveLength(1);
    await click(refresh); await click(host.querySelector('[aria-label="Lock wallet"]'));
    expect(emit.mock.calls).toEqual([['refresh'], ['lock']]);
  });
  it('shows only the selected account wallets on a separate row and preserves exact child close and select actions', async () => {
    const state = accounts(), emit = vi.fn(); await render(<WalletShell view={state} status="" emit={emit}/>);
    expect([...host.querySelectorAll('.account-tabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['Main account', 'Account 1']);
    expect(host.querySelector('.account-tabs [aria-selected="true"]')!.textContent).toBe('Account 1');
    expect([...host.querySelectorAll('.wallet-subtabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['NEAR', 'Base']);
    expect(host.querySelector('.wallet-subtabs [aria-selected="true"]')!.textContent).toBe('NEAR');
    expect(host.querySelector('.wallet-subtabs')!.previousElementSibling).toBe(host.querySelector('.account-tabs'));
    await click(host.querySelector('.wallet-subtabs [role="tab"]'));
    await act(async () => { host.querySelector('.wallet-subtabs .tab')!.dispatchEvent(new MouseEvent('auxclick', { button: 1, bubbles: true, cancelable: true })); });
    await click(host.querySelector('[aria-label="Close Base"]'));
    expect(emit.mock.calls).toEqual([['tab', 'wallet/near-1'], ['closeTab', 'wallet/near-1'], ['closeTab', 'wallet/base-1']]);
    state.Active = 'group/0'; state.AccountGroupID = 'group/0'; await render(<WalletShell view={state} status="" emit={emit}/>);
    expect([...host.querySelectorAll('.wallet-subtabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['Ethereum']);
    expect(host.querySelector('.utility-navigation .network-count')!.textContent).toBe('8 networks');
  });
  it('retains the owning account while viewing settings and returns the brand to that account overview', async () => {
    const state = accounts(); state.Active = 'settings'; const emit = vi.fn(); await render(<WalletShell view={state} status="" emit={emit}/>);
    expect(host.querySelector('main')!.getAttribute('aria-label')).toBe('Settings');
    expect(host.querySelector('.account-tabs [aria-selected="true"]')!.textContent).toBe('Account 1');
    expect([...host.querySelectorAll('.wallet-subtabs [role="tab"]')].map(tab => tab.textContent)).toEqual(['NEAR', 'Base']);
    expect(host.querySelector('.wallet-subtabs [aria-selected="true"]')).toBeNull();
    await click(host.querySelector('.brand')); expect(emit.mock.calls).toEqual([['tab', 'group/1']]);
  });
  it('renders an account whose wallets are all hidden as an empty portfolio, and omits lock when the session has no lock action', async () => {
    const state = { ...view(), AccountGroupID: 'group/0', Pages: [account(0, 0, []), { ID: 'settings', Title: 'Settings', Nodes: [] }] };
    state.Pages[0].Nodes = [node('label', 'networkCount', '0 networks')];
    await render(<WalletShell view={state} status="" emit={() => {}}/>);
    expect(host.querySelector('.portfolio-workspace')).not.toBeNull(); expect(host.querySelector('.wallet-table')).not.toBeNull();
    expect(host.querySelector('.welcome-card')).toBeNull(); expect(host.querySelector('[aria-label="Lock wallet"]')).toBeNull();
    expect(host.querySelector('.network-count')!.textContent).toBe('0 networks');
  });
  it('keeps every wallet action on one scrollable row at all supported font sizes', async () => {
    const style = document.createElement('style'); style.textContent = styles; document.head.append(style);
    try {
      const actions = node('row', 'walletActions/ton'); actions.Children = ['Send', 'Receive', 'Track new token', 'Refresh token discovery', 'Refresh contract status', 'Hide wallet'].map(Text => node('button', Text, Text));
      for (const size of ['14', '18', '24']) {
        document.documentElement.dataset.fontSize = size;
        await render(<PublicNode node={actions} emit={() => {}}/>);
        const toolbar = host.querySelector('.wallet-actions')!, computed = getComputedStyle(toolbar);
        expect(computed.display).toBe('flex'); expect(computed.flexWrap).toBe('nowrap'); expect(computed.overflowX).toBe('auto');
        expect([...toolbar.children].every(button => getComputedStyle(button).whiteSpace === 'nowrap')).toBe(true);
        expect(toolbar.children).toHaveLength(6);
      }
    } finally { style.remove(); delete document.documentElement.dataset.fontSize; }
  });
});

describe('Go-authored entry validation', () => {
  it('displays escaped network validation errors, exposes decimal keyboard/pattern and preserves invalid raw drafts', async () => {
    const field = { ...node('entry', 'field/amount/owned', 'Amount', '1e3'), InputMode: 'decimal' as const, Error: 'Enter an exact decimal amount. <img src=x>' };
    const emit = vi.fn(); await render(<Field node={field} emit={emit}/>);
    const input = host.querySelector('input')!;
    expect(input.type).toBe('text'); expect(input.value).toBe('1e3'); expect(input.inputMode).toBe('decimal');
    expect(input.getAttribute('pattern')).toBe('[0-9]+(\\.[0-9]+)?'); expect(input.getAttribute('aria-invalid')).toBe('true');
    expect(document.getElementById(input.getAttribute('aria-describedby')!)!.textContent).toBe(field.Error);
    expect(host.querySelector('.field-error img')).toBeNull();
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '1,25'); input.dispatchEvent(new Event('input', { bubbles: true })); });
    expect(input.value).toBe('1,25'); expect(emit.mock.calls).toEqual([['field/amount/owned', '1,25']]);
    await render(<Field node={{ ...field, Value: '1.25', Error: '' }} emit={emit}/>);
    expect(input.value).toBe('1.25'); expect(input.hasAttribute('aria-invalid')).toBe(false); expect(host.querySelector('[role="alert"]')).toBeNull();
    const recipient = { ...node('entry', 'field/recipient/owned', 'Recipient address', 'wrong-chain-address'), Error: 'Enter an address supported by NEAR.' };
    await render(<Field node={recipient} emit={emit}/>);
    expect(host.querySelector('input')!.getAttribute('aria-invalid')).toBe('true'); expect(host.querySelector('input')!.hasAttribute('pattern')).toBe(false);
  });
  it('accepts and renders the exact serialized Go fixture for empty fields and populated validation metadata', async () => {
    const snapshots: unknown[] = JSON.parse(readFileSync('src/testdata/go-account-layout.json', 'utf8'));
    expect(snapshots).toHaveLength(2);
    for (const snapshot of snapshots) {
      expect(validView(snapshot)).toBe(true);
      if (!validView(snapshot)) throw new Error('Invalid Go fixture');
      await render(<PublicContent page={snapshot.Pages![1]} emit={() => {}}/>);
      expect(host.querySelector('input')!.value).toBe('1e3');
      expect(host.querySelector('input')!.getAttribute('aria-invalid')).toBe(snapshot.AccountGroupID ? 'true' : null);
    }
    expect(host.querySelector('.field-error')!.textContent).toBe('Enter a positive decimal amount.');
  });
  it('accepts actual Go serialized empty optional fields on every page/node kind', () => {
    const state = accounts(); state.AccountGroupID = '';
    function serialized(nodes: Node[] | null) {
      for (const node of nodes || []) { node.Error = ''; node.InputMode = ''; serialized(node.Children); }
    }
    for (const page of state.Pages!) { if (!page.ParentID) page.ParentID = ''; serialized(page.Nodes); }
    expect(validView(state)).toBe(true);
    const field = node('entry', 'field/amount/owned', 'Amount'); field.Error = 'é'.repeat(512);
    state.Pages![0].Nodes!.push(field); expect(validView(state)).toBe(true);
    field.Error += 'é'; expect(validView(state)).toBe(false);
  });
  it('accepts only bounded entry validation fields and canonical owned-account context metadata', () => {
    const state = accounts(); expect(validView(state)).toBe(true);
    for (const AccountGroupID of ['group/01', 'group/-1', 'group/4294967296', 'group/2', 'wallet/a', null]) expect(validView({ ...state, AccountGroupID })).toBe(false);
    for (const ParentID of ['group/01', 'group/-1', 'group/4294967296', 'settings', null]) {
      const invalid = structuredClone(state); invalid.Pages![2].ParentID = ParentID as string; expect(validView(invalid)).toBe(false);
    }
    const outside = structuredClone(state); outside.Pages![2].ParentID = 'group/4294967295'; expect(validView(outside)).toBe(true);
    outside.Pages![0].ParentID = 'group/0'; expect(validView(outside)).toBe(false);
    const field = node('entry', 'field/recipient/owned', 'Recipient'); const entries = view(); entries.Pages![0].Nodes = [field];
    for (const Error of ['', 'Wrong network']) { field.Error = Error; expect(validView(entries)).toBe(true); }
    for (const Error of ['x'.repeat(1025), 'bad\0', 1, null]) { field.Error = Error as string; expect(validView(entries)).toBe(false); }
    delete field.Error;
    for (const InputMode of ['', 'decimal']) { field.InputMode = InputMode as Node['InputMode']; expect(validView(entries)).toBe(true); }
    field.InputMode = 'number' as Node['InputMode']; expect(validView(entries)).toBe(false);
    field.InputMode = 'decimal'; field.Kind = 'label'; expect(validView(entries)).toBe(false);
  });
});
