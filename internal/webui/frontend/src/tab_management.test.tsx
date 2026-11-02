// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { PublicNode, WalletShell } from './PublicApp';
import { node, view } from './test-fixtures';
import type { Node, Page, View } from './types';

let root: Root | undefined, host: HTMLDivElement;
async function render(element: React.ReactNode) { await act(async () => { if (!root) root = createRoot(host); root.render(element); }); }
async function dispatch(target: EventTarget, event: Event) { await act(async () => { target.dispatchEvent(event); }); }
async function click(element: Element | null) { expect(element).not.toBeNull(); await dispatch(element!, new MouseEvent('click', { bubbles: true })); }
function pages(): View {
  const group = (index: number): Page => ({ ID: `group/${index}`, Title: index ? 'Savings' : 'Main account', Nodes: [node('label', 'networkCount', '8 networks'), node('button', 'addNextAccount', 'Add account'), node('button', `renameGroup/${index}`, 'Rename'), node('button', `deleteGroup/${index}`, 'Delete')] });
  const wallet = (id: string, title: string): Page => ({ ID: `wallet/${id}`, Title: title, ParentID: 'group/0', Nodes: [node('button', `renameWallet/${id}`, 'Rename'), node('button', `deleteWallet/${id}`, 'Delete')] });
  return { ...view(), Active: 'wallet/eth', AccountGroupID: 'group/0', Pages: [group(0), group(1), wallet('eth', 'Ethereum'), wallet('base', 'Base'), { ID: 'settings', Title: 'Settings', Nodes: [] }] };
}
function tab(text: string): HTMLButtonElement { return [...host.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(button => button.textContent === text)!; }
async function context(button: HTMLButtonElement) { const event = new MouseEvent('contextmenu', { clientX: 100, clientY: 150, bubbles: true, cancelable: true }); await dispatch(button, event); return event; }
beforeEach(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }); document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host); });
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; });

describe('account plus and owned tab context menus', () => {
  it('shows the backend-authorized plus only in the account tab strip and preserves its disabled state', async () => {
    const state = pages(), emit = vi.fn(); state.Active = 'group/0';
    await render(<WalletShell view={state} status="" emit={emit}/>);
    const add = host.querySelector<HTMLButtonElement>('.account-tabs [aria-label="Add deterministic account"]')!;
    expect(add.textContent).toBe(''); expect(add.querySelector('svg')).not.toBeNull(); expect(host.querySelectorAll('[data-action="addNextAccount"]')).toHaveLength(1);
    expect(host.querySelector('main')!.textContent).not.toMatch(/Rename|Delete|Add account/);
    await click(add); expect(emit.mock.calls).toEqual([['addNextAccount']]);
    state.Pages![0].Nodes!.find(item => item.ID === 'addNextAccount')!.Disabled = true;
    await render(<WalletShell view={state} status="" emit={emit}/>); await click(add); expect(emit).toHaveBeenCalledTimes(1);
  });
  it('opens an inactive group or wallet menu without selecting it and emits only its exact Go-provided action', async () => {
    const state = pages(), emit = vi.fn(); await render(<WalletShell view={state} status="" emit={emit}/>);
    expect((await context(tab('Savings'))).defaultPrevented).toBe(true);
    expect(host.querySelector('[role="menu"]')!.getAttribute('aria-label')).toBe('Savings actions');
    expect([...host.querySelectorAll('[role="menuitem"]')].map(item => item.textContent)).toEqual(['Rename', 'Delete']);
    expect(document.activeElement).toBe(host.querySelector('[role="menuitem"]')); expect(emit).not.toHaveBeenCalled();
    await click(host.querySelector('[role="menuitem"]')); expect(emit.mock.calls).toEqual([['renameGroup/1']]); expect(host.querySelector('[role="menu"]')).toBeNull();
    await context(tab('Base')); await click(host.querySelectorAll('[role="menuitem"]')[1]);
    expect(emit.mock.calls).toEqual([['renameGroup/1'], ['deleteWallet/base']]);
    expect(host.querySelector('.wallet-subtabs [aria-selected="true"]')!.textContent).toBe('Ethereum');
  });
  it('supports ContextMenu/Shift-F10, menu arrow navigation and Escape focus restoration', async () => {
    const state = pages(), emit = vi.fn(); await render(<WalletShell view={state} status="" emit={emit}/>);
    const owner = tab('Ethereum'); owner.focus();
    await dispatch(owner, new KeyboardEvent('keydown', { key: 'ContextMenu', bubbles: true, cancelable: true }));
    const menu = host.querySelector('[role="menu"]')!, items = host.querySelectorAll('[role="menuitem"]');
    await dispatch(menu, new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })); expect(document.activeElement).toBe(items[1]);
    await dispatch(menu, new KeyboardEvent('keydown', { key: 'Home', bubbles: true, cancelable: true })); expect(document.activeElement).toBe(items[0]);
    await dispatch(menu, new KeyboardEvent('keydown', { key: 'End', bubbles: true, cancelable: true })); expect(document.activeElement).toBe(items[1]);
    await dispatch(menu, new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(host.querySelector('[role="menu"]')).toBeNull(); expect(document.activeElement).toBe(owner);
    await dispatch(owner, new KeyboardEvent('keydown', { key: 'F10', shiftKey: true, bubbles: true, cancelable: true })); expect(host.querySelector('[role="menu"]')).not.toBeNull();
    await dispatch(document.body, new PointerEvent('pointerdown', { bubbles: true })); expect(host.querySelector('[role="menu"]')).toBeNull();
    await context(owner); await dispatch(window, new Event('resize')); expect(host.querySelector('[role="menu"]')).toBeNull(); expect(emit).not.toHaveBeenCalled();
  });
  it('discards a context menu after navigation or target/action metadata changes and tolerates unrelated balance snapshots', async () => {
    const original = pages(), emit = vi.fn();
    for (const mutate of [
      (state: View) => { state.Active = 'settings'; },
      (state: View) => { state.AccountGroupID = 'group/1'; },
      (state: View) => { state.Pages![3].Title = 'Renamed'; },
      (state: View) => { state.Pages![3].ParentID = 'group/1'; },
      (state: View) => { state.Pages![3].Nodes![0].Disabled = true; },
      (state: View) => { state.Pages![3].Nodes = []; },
      (state: View) => { state.Pages = state.Pages!.filter(page => page.ID !== 'wallet/base'); },
    ]) {
      const state = structuredClone(original); await render(<WalletShell view={state} status="" emit={emit}/>); await context(tab('Base'));
      expect(host.querySelector('[role="menu"]')).not.toBeNull(); mutate(state); await render(<WalletShell view={state} status="" emit={emit}/>);
      expect(host.querySelector('[role="menu"]')).toBeNull(); expect(emit).not.toHaveBeenCalled();
    }
    const state = structuredClone(original); await render(<WalletShell view={state} status="" emit={emit}/>); await context(tab('Base'));
    state.Status = 'Balance refreshed'; state.StatusRevision++;
    await render(<WalletShell view={state} status="" emit={emit}/>); expect(host.querySelector('[role="menu"]')).not.toBeNull();
  });
  it('never synthesizes a missing or foreign action and keeps disabled menu choices inert', async () => {
    const state = pages(), emit = vi.fn(); state.Pages![2].Nodes = [node('button', 'renameWallet/foreign', 'Rename')];
    await render(<WalletShell view={state} status="" emit={emit}/>);
    expect((await context(tab('Ethereum'))).defaultPrevented).toBe(false); expect(host.querySelector('[role="menu"]')).toBeNull();
    for (const action of state.Pages![3].Nodes!) action.Disabled = true;
    await render(<WalletShell view={state} status="" emit={emit}/>); await context(tab('Base'));
    const menu = host.querySelector('[role="menu"]')!; expect(document.activeElement).toBe(menu);
    await click(menu.querySelector('button')); expect(emit).not.toHaveBeenCalled();
    await dispatch(menu, new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })); expect(host.querySelector('[role="menu"]')).toBeNull();
  });
});

describe('entry error rows preserve neighboring action alignment', () => {
  it('places the recipient error below its address input and both address-book controls', async () => {
    const style = document.createElement('style'); style.textContent = readFileSync('src/styles.css', 'utf8'); document.head.append(style);
    try {
      const entry = { ...node('entry', 'field/recipient/owned', 'Recipient address', 'wrong-chain'), Error: 'Address is not compatible with this network.' };
      const row = node('row', 'recipientField/owned'); row.Children = [entry, node('button', 'addressBookLoad/owned', 'Load from address book'), node('button', 'addressBookSave/owned', 'Save address to address book')];
      await render(<PublicNode node={row} emit={() => {}}/>);
      const box = host.querySelector('.recipient-field')!, input = box.querySelector('input')!, error = box.querySelector('.recipient-error')!;
      expect(box.children).toHaveLength(4); expect(box.children[3]).toBe(error); expect(box.querySelector('label .field-error')).toBeNull();
      expect(input.getAttribute('aria-describedby')).toBe(error.id); expect(box.querySelectorAll('[role="alert"]')).toHaveLength(1);
      expect(getComputedStyle(error).gridColumn).toBe('1 / -1'); expect(getComputedStyle(box).alignItems).toBe('end');
    } finally { style.remove(); }
  });
  it('keeps exchange/direct entry errors on separate full-width rows after all input/action controls', async () => {
    const style = document.createElement('style'); style.textContent = readFileSync('src/styles.css', 'utf8'); document.head.append(style);
    try {
      const spend = { ...node('entry', 'field/spend', 'Amount', '1e2'), Error: 'Enter a decimal amount', InputMode: 'decimal' as const };
      const destination = { ...node('entry', 'field/withdrawAddress', 'Destination', 'bad'), Error: 'Choose a compatible address' };
      const row = node('row'); row.Children = [spend, destination, node('button', 'exchangeQuote', 'Get price')];
      await render(<PublicNode node={row} emit={() => {}}/>);
      const box = host.querySelector('.node-row')!, errors = box.querySelectorAll('.node-row-error'), inputs = box.querySelectorAll('input');
      expect([...box.children].slice(0, 3).map(child => child.tagName)).toEqual(['LABEL', 'LABEL', 'BUTTON']); expect(errors).toHaveLength(2);
      for (const [index, error] of [...errors].entries()) { expect(box.children[3 + index]).toBe(error); expect(inputs[index].getAttribute('aria-describedby')).toBe(error.id); expect(getComputedStyle(error).flexBasis).toBe('100%'); }
      expect(box.querySelectorAll('label .field-error')).toHaveLength(0); expect(box.querySelectorAll('[role="alert"]')).toHaveLength(2);
      expect([...box.querySelectorAll('label')].every(label => getComputedStyle(label).margin === '0px')).toBe(true);
      spend.Error = ''; destination.Error = ''; await render(<PublicNode node={row} emit={() => {}}/>);
      expect(host.querySelectorAll('[role="alert"]')).toHaveLength(0); expect(host.querySelector('.entry-action-row')).not.toBeNull();
      expect([...box.querySelectorAll('label')].every(label => getComputedStyle(label).margin === '0px')).toBe(true);
    } finally { style.remove(); }
  });
});
