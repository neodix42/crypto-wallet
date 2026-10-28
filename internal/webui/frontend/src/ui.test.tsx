// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PublicApp, PublicContent, PublicNode, WalletShell } from './PublicApp';
import { PrivateApp, PromptContents } from './PrivateApp';
import { id, node, phrase, view } from './test-fixtures';
import type { Prompt, Reply, View, WalletEvent } from './types';

let root: Root | undefined;
let host: HTMLDivElement;
let push: (...args: unknown[]) => void;
const unsubscribe = vi.fn();
async function mount(element: React.ReactNode) { await act(async () => { root = createRoot(host); root.render(element); }); }
async function click(element: Element | null) { expect(element).not.toBeNull(); await act(async () => { element!.dispatchEvent(new MouseEvent('click', { bubbles: true })); }); }
async function submit() { await act(async () => { host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); }); }
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host);
  window.runtime = { EventsOn: vi.fn((_name, callback) => { push = callback; return unsubscribe; }) };
  unsubscribe.mockReset(); delete window.go;
});
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; vi.useRealTimers(); });

describe('public React renderer', () => {
  it('renders inert public data and every node kind without injecting provider HTML', () => {
    const attack = '<img src=x onerror=alert(1)>';
    const cases = [node('label', '', attack), node('heading', '', 'Heading'), node('separator'), node('button', 'refresh', 'Refresh'), node('address', '', 'Receive address', '0x123'), node('entry', 'field/amount', 'Amount', '1'), node('check', 'field/showHidden', 'Show hidden', 'true'), node('combo', 'field/region', 'Region')];
    cases[7].Options = [{ ID: 'official', Text: 'Official' }];
    for (const item of cases) expect(renderToStaticMarkup(<PublicNode node={item} emit={() => {}}/>)).not.toContain('onerror=alert(1)>');
    expect(renderToStaticMarkup(<PublicNode node={cases[0]} emit={() => {}}/>)).toContain('&lt;img');
    const column = node('column'); column.Children = cases;
    expect(renderToStaticMarkup(<PublicNode node={column} emit={() => {}}/>)).toContain('Receive address');
    expect(renderToStaticMarkup(<WalletShell view={view()} status="Ready" emit={() => {}}/>)).toContain('status-bar');
  });
  it('keeps full addresses and provider observation only in tooltip, with copy independent of row open', async () => {
    const address = 'a'.repeat(97), row = node('row', 'open/a');
    row.Children = [node('button', 'open/a', 'Ethereum'), node('label', '', '1.25 ETH'), node('address', '', '', address), node('button', 'copyReceive/a', '⧉')];
    row.Children[1].Tooltip = 'Provider observed at 123';
    const emit = vi.fn(); await mount(<PublicContent page={{ ID: 'group/0', Title: 'Main account', Nodes: [row] }} emit={emit}/>);
    expect(host.querySelector('code')!.textContent).toBe(address);
    expect(host.textContent).not.toContain('Provider observed');
    expect(host.querySelector('[title="Provider observed at 123"]')).not.toBeNull();
    await click(host.querySelector('[data-action="copyReceive/a"]'));
    expect(emit.mock.calls).toEqual([['copyReceive/a']]);
    await click(host.querySelector('td:nth-child(2)'));
    expect(emit.mock.calls[1]).toEqual(['open/a']);
  });
  it('renders token balances in three columns and exposes exact contract identity only on hover or focus', async () => {
    const table = node('column', 'walletAssets/ethereum:0', 'Wallet assets');
    const contracts = ['Native currency · ethereum-mainnet', 'ethereum-mainnet\nERC-20 contract: 0xdAC17F958D2ee523a2206206994597C13D831ec7', 'ethereum-mainnet\nERC-20 contract: 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48'];
    table.Children = ['ETH', 'USDT', 'USDC'].map((symbol, index) => {
      const row = node('row', `walletAsset/exact-${index}`);
      const selection = node('button', `asset/owned/exact-${index}`, symbol);
      selection.Disabled = index === 0;
      row.Tooltip = contracts[index];
      row.Children = [selection, node('label', '', index === 1 ? '1.25' : 'Unknown'), node('label', '', index === 0 ? '18' : '6')];
      return row;
    });
    const emit = vi.fn(); await mount(<PublicNode node={table} emit={emit}/>);
    expect([...host.querySelectorAll('th')].map(cell => cell.textContent)).toEqual(['Token', 'Balance', 'Decimals']);
    expect([...host.querySelectorAll('tbody tr')].map(row => [...row.querySelectorAll('td')].map(cell => cell.textContent))).toEqual([['ETH', 'Unknown', '18'], ['USDT', '1.25', '6'], ['USDC', 'Unknown', '6']]);
    expect(host.textContent).not.toContain('0xdAC17');
    const selectors = host.querySelectorAll('.asset-selector');
    expect(selectors[0].getAttribute('tabindex')).toBe('0');
    await act(async () => { selectors[1].dispatchEvent(new MouseEvent('mouseover', { bubbles: true })); });
    expect(host.querySelector('[role="tooltip"]')!.textContent).toBe(contracts[1]);
    expect(selectors[1].querySelector('button')!.hasAttribute('title')).toBe(false);
    await act(async () => { selectors[1].dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })); });
    expect(host.querySelector('[role="tooltip"]')).toBeNull();
    await act(async () => { selectors[0].dispatchEvent(new FocusEvent('focusin', { bubbles: true })); });
    expect(host.querySelector('[role="tooltip"]')!.textContent).toBe(contracts[0]);
    await act(async () => { selectors[0].dispatchEvent(new MouseEvent('mouseover', { bubbles: true })); selectors[0].dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })); });
    expect(host.querySelector('[role="tooltip"]')!.textContent).toBe(contracts[0]);
    await act(async () => { selectors[0].dispatchEvent(new MouseEvent('mouseover', { bubbles: true })); selectors[0].dispatchEvent(new FocusEvent('focusout', { bubbles: true })); });
    expect(host.querySelector('[role="tooltip"]')!.textContent).toBe(contracts[0]);
    await act(async () => { selectors[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); });
    expect(host.querySelector('[role="tooltip"]')).toBeNull();
    await click(selectors[2].querySelector('button'));
    expect(emit.mock.calls).toEqual([['asset/owned/exact-2']]);
  });
  it('keeps receive labels and full addresses together while recipient book icons send only exact Go actions', async () => {
    const address = node('address', '', 'Receive address', '0x' + 'a'.repeat(64));
    const row = node('row', 'recipientField/owned');
    row.Children = [node('entry', 'field/recipient/owned', 'Recipient address', '0x111'), node('button', 'addressBookLoad/owned', 'Load from address book'), node('button', 'addressBookSave/owned', 'Save address to address book')];
    const emit = vi.fn(); await mount(<><PublicNode node={address} emit={emit}/><PublicNode node={row} emit={emit}/></>);
    expect(host.querySelector('.receive-address-line .address-label')!.textContent).toBe('Receive address:');
    expect(host.querySelector('.receive-address-line code')!.textContent).toBe(address.Value);
    expect(host.querySelector('.recipient-field input')!.getAttribute('value')).toBe('0x111');
    const load = host.querySelector('[aria-label="Load from address book"]')!, save = host.querySelector('[aria-label="Save address to address book"]')!;
    expect(load.querySelector('svg')).not.toBeNull(); expect(save.querySelector('svg')).not.toBeNull();
    await click(load); await click(save);
    expect(emit.mock.calls).toEqual([['addressBookLoad/owned'], ['addressBookSave/owned']]);
  });
  it('escapes tooltip metadata and retains a normal rendering for malformed asset groups', async () => {
    const table = node('column', 'walletAssets/test', 'Wallet assets'), row = node('row', 'walletAsset/test');
    row.Tooltip = '<img src=x onerror=alert(1)>';
    row.Children = [node('button', 'asset/exact', 'TOKEN'), node('label', '', 'Unknown'), node('label', '', '6')];
    table.Children = [row]; await mount(<PublicNode node={table} emit={() => {}}/>);
    await act(async () => { host.querySelector('.asset-selector')!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })); });
    expect(host.querySelector('[role="tooltip"]')!.textContent).toBe(row.Tooltip);
    expect(host.querySelector('[role="tooltip"] img')).toBeNull();
    row.Children.pop(); await act(async () => { root!.render(<PublicNode node={table} emit={() => {}}/>); });
    expect(host.querySelector('table')).toBeNull();
    expect(host.textContent).toContain('TOKEN');
  });
  it('handles wallet middle close and keeps Exchange and Settings as top tabs', async () => {
    const state = view(); state.Pages!.push({ ID: 'wallet/a', Title: 'ETH · Ethereum', Nodes: [] }, { ID: 'exchange', Title: 'Exchange', Nodes: [] }, { ID: 'settings', Title: 'Settings', Nodes: [] });
    const emit = vi.fn(); await mount(<WalletShell view={state} status="" emit={emit}/>);
    const wallet = [...host.querySelectorAll('.tab')].find(tab => tab.textContent?.includes('ETH'))!;
    await act(async () => { wallet.dispatchEvent(new MouseEvent('auxclick', { button: 1, bubbles: true, cancelable: true })); });
    expect(emit).toHaveBeenCalledWith('closeTab', 'wallet/a');
    await click([...host.querySelectorAll('[role="tab"]')].find(tab => tab.textContent === 'Settings')!);
    expect(emit).toHaveBeenCalledWith('tab', 'settings');
  });
  it('renders NEAR Wallet/Staking tabs with selection and only typed public control events', async () => {
    const tabs = node('row', 'walletSections/near-mainnet:0', 'NEAR wallet sections', 'Staking');
    tabs.Children = [node('button', 'walletSection/near-mainnet:0/Wallet', 'Wallet'), node('button', 'walletSection/near-mainnet:0/Staking', 'Staking')];
    tabs.Children[1].Disabled = true;
    const emit = vi.fn();
    await mount(<PublicNode node={tabs} emit={emit}/>);
    const list = host.querySelector('[role="tablist"]')!;
    expect(list.getAttribute('aria-label')).toBe('NEAR wallet sections');
    expect([...list.querySelectorAll('[role="tab"]')].map(tab => [tab.textContent, tab.getAttribute('aria-selected')])).toEqual([['Wallet', 'false'], ['Staking', 'true']]);
    await click(list.querySelector('[data-action="walletSection/near-mainnet:0/Wallet"]'));
    expect(emit.mock.calls).toEqual([['walletSection/near-mainnet:0/Wallet']]);
    expect(host.querySelector('form')).toBeNull();
  });
  it('keeps staking contract and exact amount inert, without generic contract or secret bridge methods', async () => {
    const contract = node('address', '', 'Exact contract · near-mainnet', 'astro-stakers.poolv1.near');
    const amount = node('entry', 'field/stakingAmount/near-mainnet:0', 'Amount (NEAR; 1 means 1 NEAR)', '1.25');
    const prepare = node('button', 'stakingPrepare/near-mainnet:0/stake', 'Stake');
    const confirmation = node('button', 'stakingSend/near-mainnet:0', 'Review and confirm staking transaction');
    const emit = vi.fn();
    await mount(<PublicContent page={{ ID: 'wallet/near-mainnet:0', Title: 'NEAR', Nodes: [contract, amount, prepare, confirmation] }} emit={emit}/>);
    expect(host.querySelector('code')!.textContent).toBe('astro-stakers.poolv1.near');
    expect(host.querySelector('input')!.value).toBe('1.25');
    expect(host.textContent).toContain('1 means 1 NEAR');
    await click(host.querySelector('[data-action="stakingPrepare/near-mainnet:0/stake"]'));
    expect(emit.mock.calls).toEqual([['stakingPrepare/near-mainnet:0/stake']]);
    await click(host.querySelector('[data-action="stakingSend/near-mainnet:0"]'));
    expect(emit.mock.calls[1]).toEqual(['stakingSend/near-mainnet:0']);
    expect(host.querySelector('[data-action="stakingSend/near-mainnet:0"]')!.classList.contains('primary')).toBe(true);
  });
  it('does not overwrite a fresh event with a stale snapshot and serializes public actions', async () => {
    let resolve!: (value: View) => void;
    const snapshot = new Promise<View>(done => { resolve = done; });
    const events: WalletEvent[] = [];
    window.go = { webui: { PublicBridge: { Snapshot: () => snapshot, Event: vi.fn(async event => { events.push(event); return true; }) } } };
    await mount(<PublicApp/>);
    const fresh = view(); fresh.Pages![0].Nodes = [node('button', 'refresh', 'Fresh view')];
    await act(async () => { push(fresh); resolve(view()); });
    expect(host.textContent).toContain('Fresh view');
    await click(host.querySelector('[data-action="refresh"]')); expect(events).toEqual([{ ID: 'refresh', Value: '' }]);
  });
  it('uses a fixed status area, hides after seven seconds and resets repeated messages by revision', async () => {
    vi.useFakeTimers();
    const state = view(); state.Status = 'Balance unknown'; state.StatusRevision = 1;
    window.go = { webui: { PublicBridge: { Snapshot: async () => state, Event: async () => true } } };
    await mount(<PublicApp/>);
    expect(host.querySelector('.status-bar')!.textContent).toContain('Balance unknown');
    await act(async () => { vi.advanceTimersByTime(6000); push({ ...state, StatusRevision: 2 }); });
    await act(async () => { vi.advanceTimersByTime(6000); });
    expect(host.querySelector('.status-bar')!.textContent).toContain('Balance unknown');
    await act(async () => { vi.advanceTimersByTime(1000); });
    expect(host.querySelector('.status-bar')!.textContent).toBe('');
    expect(host.textContent).not.toContain('Dismiss');
  });
});

function bridge(prompt: Prompt, reply: (reply: Reply) => Promise<boolean> = async () => true) {
  const copy = vi.fn(async () => true), activity = vi.fn(async () => true);
  window.go = { webui: { PrivateBridge: { Prompt: vi.fn(async () => prompt), Reply: vi.fn(reply), Copy: copy, Activity: activity } } };
  return { copy, activity };
}

describe('separate private React prompt', () => {
  it('masks every input and clears DOM before the private reply crosses its bridge', async () => {
    let received: Reply | undefined;
    bridge({ id, kind: 'input', title: 'Unlock local wallet', labels: ['App password'] }, async value => {
      expect(host.querySelector('input')?.value || '').toBe(''); received = structuredClone(value); return true;
    });
    await mount(<PrivateApp/>);
    const input = host.querySelector('input')!; expect(input.type).toBe('password'); expect(input.autocomplete).toBe('off');
    input.value = 'disposable-test-password'; await submit();
    expect(received).toEqual({ promptId: id, accepted: true, values: ['disposable-test-password'] });
    expect(host.querySelector('input')).toBeNull();
  });
  it('rejects inputs exceeding the byte limit without passing them to Go', async () => {
    bridge({ id, kind: 'input', title: 'Import', labels: ['Recovery phrase'] });
    await mount(<PrivateApp/>); host.querySelector('input')!.value = 'é'.repeat(2049); await submit();
    expect(window.go!.webui!.PrivateBridge!.Reply).not.toHaveBeenCalled(); expect(host.textContent).toContain('4096 bytes');
  });
  it('shows numbered initial words without any clipboard operation', async () => {
    const { copy } = bridge({ id, kind: 'recovery', title: 'Record recovery phrase', phrase });
    await mount(<PrivateApp/>);
    expect(host.querySelectorAll('[data-recovery-word]')).toHaveLength(12);
    expect(host.textContent).toContain('12about'); expect(host.textContent).not.toContain('Copy phrase');
    const event = new KeyboardEvent('keydown', { key: 'c', ctrlKey: true, bubbles: true, cancelable: true });
    await act(async () => { window.dispatchEvent(event); }); expect(event.defaultPrevented).toBe(true); expect(copy).not.toHaveBeenCalled();
  });
  it('copies only an explicit authenticated backup and sends no phrase argument', async () => {
    const { copy } = bridge({ id, kind: 'recoveryBackup', title: 'Back up recovery phrase', phrase });
    await mount(<PrivateApp/>);
    await click([...host.querySelectorAll('button')].find(button => button.textContent === 'Copy phrase')!);
    expect(copy.mock.calls).toEqual([[]]); expect(host.textContent).toContain('clipboard history');
    expect(host.querySelectorAll('[data-recovery-word]')).toHaveLength(12);
    await submit(); expect(host.textContent).not.toContain('abandon');
  });
  it('uses the Go-provided local QR and argument-free public address copy', async () => {
    const { copy } = bridge({ id, kind: 'receive', title: 'Receive ETH', address: '0x123', png: 'iVBORw0KGgo=', summary: 'Ethereum mainnet' });
    await mount(<PrivateApp/>);
    expect(host.querySelector('img')!.src).toBe('data:image/png;base64,iVBORw0KGgo=');
    await click([...host.querySelectorAll('button')].find(button => button.textContent === 'Copy address')!);
    expect(copy.mock.calls).toEqual([[]]);
  });
  it('revokes and clears a live phrase, rejects late delivery and never renders summary HTML', async () => {
    bridge({ id, kind: 'recoveryBackup', title: 'Backup', phrase }); await mount(<PrivateApp/>);
    await act(async () => { push(); });
    expect(host.textContent).not.toContain('abandon'); expect(window.go!.webui!.PrivateBridge!.Reply).toHaveBeenCalledWith({ promptId: id, accepted: false });
    const markup = renderToStaticMarkup(<PromptContents prompt={{ id, kind: 'confirm', title: 'Review', summary: '<iframe src="https://bad.invalid">' }} error="" copyStatus="" copying={false} copy={() => {}}/>);
    expect(markup).toContain('&lt;iframe'); expect(markup).not.toContain('<iframe');
  });
  it('does not display a prompt that arrives after its private frame is revoked', async () => {
    let resolve!: (value: Prompt) => void;
    window.go = { webui: { PrivateBridge: { Prompt: () => new Promise(done => { resolve = done; }), Reply: vi.fn(async () => true), Copy: vi.fn(async () => true), Activity: vi.fn(async () => true) } } };
    await mount(<PrivateApp/>);
    await act(async () => { push(); resolve({ id, kind: 'recovery', title: 'Record', phrase }); });
    expect(host.textContent).not.toContain('abandon');
  });
});
