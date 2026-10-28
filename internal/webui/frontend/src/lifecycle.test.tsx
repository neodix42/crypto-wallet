// @vitest-environment happy-dom
// Synthetic public fixtures only. These exercise the shipped React components;
// they do not provide wallet secrets, a signer or blockchain acceptance evidence.
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Field, PublicApp, PublicContent, WalletShell } from './PublicApp';
import { PrivateApp } from './PrivateApp';
import { id, node, phrase, view } from './test-fixtures';
import type { Prompt, Reply, View } from './types';

let root: Root | undefined;
let host: HTMLDivElement;
let pushed: (...args: unknown[]) => void;
const unsubscribe = vi.fn();
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (reason: Error) => void;
  const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail; });
  return { promise, resolve, reject };
}
async function mount(value: ReactNode) {
  await act(async () => { root = createRoot(host); root.render(value); });
}
async function rerender(value: ReactNode) { await act(async () => { root!.render(value); }); }
async function dispatch(target: EventTarget, event: Event) { await act(async () => { target.dispatchEvent(event); }); }
async function click(text: string) {
  const button = [...host.querySelectorAll('button')].find(item => item.textContent === text);
  expect(button, text).toBeDefined();
  await dispatch(button!, new MouseEvent('click', { bubbles: true }));
}
async function submit() { await dispatch(host.querySelector('form')!, new Event('submit', { bubbles: true, cancelable: true })); }
function privateBridge(prompt: unknown, reply: (value: Reply) => Promise<boolean> = async () => true) {
  const bridge = { Prompt: vi.fn(async () => prompt), Reply: vi.fn(reply), Copy: vi.fn(async () => true), Activity: vi.fn(async () => true) };
  window.go = { webui: { PrivateBridge: bridge } };
  return bridge;
}
function publicBridge(state: unknown = view()) {
  const bridge = { Snapshot: vi.fn(async () => state), Event: vi.fn(async () => true) };
  window.go = { webui: { PublicBridge: bridge } };
  return bridge;
}
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host);
  window.runtime = { EventsOn: vi.fn((_name, callback) => { pushed = callback; return unsubscribe; }) };
  unsubscribe.mockReset(); delete window.go;
});
afterEach(async () => {
  if (root) await act(async () => { root!.unmount(); });
  root = undefined; vi.restoreAllMocks(); vi.useRealTimers();
});

describe('public renderer interaction and asynchronous lifetime', () => {
  it('keeps edited form values until Go changes the field, and emits exact decimal strings', async () => {
    const field = node('entry', 'field/amount', 'Amount', '1');
    const emit = vi.fn(); await mount(<Field node={field} emit={emit}/>);
    const input = host.querySelector('input')!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '1.000000000000000001');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    expect(emit).toHaveBeenCalledWith('field/amount', '1.000000000000000001');
    await rerender(<Field node={{ ...field }} emit={emit}/>);
    expect(input.value).toBe('1.000000000000000001');
    await rerender(<Field node={{ ...field, Value: '2' }} emit={emit}/>);
    expect(input.value).toBe('2');
  });
  it('uses typed combo and checkbox changes, including absent options', async () => {
    const emit = vi.fn(), field = node('combo', 'field/region', 'Region');
    await mount(<Field node={field} emit={emit}/>);
    expect(host.querySelector('option')!.textContent).toBe('Select…');
    field.Options = [{ ID: 'official', Text: 'Official' }];
    await rerender(<Field node={field} emit={emit}/>);
    const select = host.querySelector('select')!; select.value = 'official';
    await dispatch(select, new Event('change', { bubbles: true }));
    expect(emit).toHaveBeenCalledWith('field/region', 'official');
    await rerender(<Field node={{ ...field, Value: 'official' }} emit={emit}/>);
    expect(host.querySelectorAll('option')).toHaveLength(1);
    await rerender(<Field node={node('check', 'field/hidden', 'Hidden', 'false')} emit={emit}/>);
    await dispatch(host.querySelector('input')!, new MouseEvent('click', { bubbles: true }));
    expect(emit).toHaveBeenCalledWith('field/hidden', 'true');
  });
  it('opens table rows from keyboard without treating child controls as row activation', async () => {
    const row = node('row', 'open/public');
    row.Children = [node('button', 'open/public', 'Ethereum'), node('label', '', '1 ETH'), node('address', '', '', '0x123'), node('button', 'copyReceive/public', 'Copy')];
    const heading = node('row'); heading.Children = [node('heading', '', 'Network')];
    const emit = vi.fn();
    await mount(<PublicContent page={{ ID: 'group/0', Title: 'Main', Nodes: [heading, row, node('label', '', 'After wallets')] }} emit={emit}/>);
    expect(host.querySelector('.portfolio-toolbar')!.textContent).not.toContain('Network');
    expect(host.textContent).toContain('After wallets');
    const tr = host.querySelector('tbody tr')!;
    for (const key of ['Enter', ' ']) {
      const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
      await dispatch(tr, event); expect(event.defaultPrevented).toBe(true);
    }
    await dispatch(tr, new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    await dispatch(tr.querySelector('button')!, new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    expect(emit.mock.calls).toEqual([['open/public'], ['open/public']]);
  });
  it('handles explicit tab close, brand navigation, empty pages and missing active tabs', async () => {
    const state = view(); state.Active = 'missing';
    state.Pages!.push({ ID: 'wallet/public', Title: 'ETH', Nodes: null });
    const emit = vi.fn(); await mount(<WalletShell view={state} status="" emit={emit}/>);
    await dispatch(host.querySelector('.brand')!, new MouseEvent('click', { bubbles: true, cancelable: true }));
    await click('×');
    expect(emit.mock.calls).toEqual([['tab', 'group/0'], ['closeTab', 'wallet/public']]);
    for (const button of [0, 2]) await dispatch(host.querySelector('.tab')!, new MouseEvent('auxclick', { button, bubbles: true }));
    expect(emit).toHaveBeenCalledTimes(2);
    await rerender(<WalletShell view={{ ...state, Pages: null }} status="" emit={emit}/>);
    expect(host.textContent).toContain('Opening your encrypted wallet');
    await dispatch(host.querySelector('.brand')!, new MouseEvent('click', { bubbles: true }));
    expect(emit).toHaveBeenCalledTimes(2);
    await rerender(<PublicContent page={{ ID: 'settings', Title: 'Settings', Nodes: null }} emit={emit}/>);
    expect(host.querySelector('.content-card')).not.toBeNull();
  });
  it('reports invalid and rejected snapshots without rendering their data', async () => {
    publicBridge({ ...view(), Title: '\0invalid-view-marker' });
    await mount(<PublicApp/>);
    expect(host.textContent).toContain('invalid view');
    expect(host.textContent).not.toContain('invalid-view-marker');
    await act(async () => { root!.unmount(); }); root = undefined;
    publicBridge().Snapshot.mockRejectedValueOnce(new Error('provider-private-error-marker'));
    await mount(<PublicApp/>);
    expect(host.textContent).toContain('connection is unavailable');
    expect(host.textContent).not.toContain('provider-private-error-marker');
  });
  it('serializes failures without dropping later actions and ignores activity failures', async () => {
    const state = view(); state.Pages![0].Nodes = [node('button', 'refresh', 'Refresh')];
    const bridge = publicBridge(state), pending = deferred<boolean>();
    bridge.Event.mockImplementationOnce(() => pending.promise);
    await mount(<PublicApp/>); await click('Refresh'); await click('Refresh');
    expect(bridge.Event).toHaveBeenCalledTimes(1);
    await act(async () => { pending.resolve(false); });
    expect(bridge.Event).toHaveBeenCalledTimes(2);
    expect(host.textContent).toContain('Request failed');
    bridge.Event.mockRejectedValueOnce(new Error('private-transport-error-marker'));
    await dispatch(window, new KeyboardEvent('keydown', { key: 'Tab' }));
    expect(host.textContent).not.toContain('private-transport-error-marker');
  });
  it('throttles value-free activity and unregisters listeners after teardown', async () => {
    vi.useFakeTimers(); vi.setSystemTime(10000);
    const bridge = publicBridge(); await mount(<PublicApp/>);
    for (const type of ['pointerdown', 'keydown', 'pointerdown']) await dispatch(window, new Event(type));
    expect(bridge.Event.mock.calls).toEqual([[{ ID: 'activity', Value: '' }]]);
    await act(async () => { vi.advanceTimersByTime(1000); });
    await dispatch(window, new Event('keydown'));
    expect(bridge.Event).toHaveBeenCalledTimes(2);
    await act(async () => { root!.unmount(); }); root = undefined;
    await dispatch(window, new Event('pointerdown'));
    expect(bridge.Event).toHaveBeenCalledTimes(2); expect(unsubscribe).toHaveBeenCalledOnce();
  });
  it('does not publish late snapshots, pushes or rejected actions after teardown', async () => {
    const snapshot = deferred<unknown>(), event = deferred<boolean>();
    const bridge = publicBridge(); bridge.Snapshot.mockImplementation(() => snapshot.promise);
    bridge.Event.mockImplementation(() => event.promise);
    await mount(<PublicApp/>); await dispatch(window, new Event('keydown'));
    await act(async () => { root!.unmount(); }); root = undefined;
    await act(async () => { snapshot.resolve(view()); pushed(view()); event.reject(new Error('late-failure')); });
    expect(host.textContent).toBe(''); expect(unsubscribe).toHaveBeenCalledOnce();
  });
  it('handles a missing public bridge or event subscription without exposing internals', async () => {
    delete window.runtime; await mount(<PublicApp/>);
    await dispatch(window, new Event('keydown'));
    expect(host.textContent).toContain('connection is unavailable');
  });
});

describe('private prompt transport and revocation', () => {
  it.each(['missing', 'rejected', 'invalid'] as const)('closes unavailable %s prompt transport without exposing its error', async failure => {
    if (failure !== 'missing') {
      const bridge = privateBridge({ id, kind: 'input', title: 'Invalid', labels: [] });
      if (failure === 'rejected') bridge.Prompt.mockRejectedValueOnce(new Error('private-failure-marker'));
    }
    await mount(<PrivateApp/>);
    expect(host.textContent).toContain('no longer available');
    expect(host.textContent).not.toContain('private-failure-marker');
    expect(host.querySelector('input')).toBeNull();
    expect((host.querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(true);
  });
  it.each(['false', 'throw', 'missing'] as const)('clears input and uses a fixed error for a %s reply', async failure => {
    const bridge = privateBridge({ id, kind: 'input', title: 'Unlock', labels: ['Password'] });
    if (failure === 'false') bridge.Reply.mockResolvedValue(false);
    if (failure === 'throw') bridge.Reply.mockRejectedValue(new Error('private-reply-marker'));
    await mount(<PrivateApp/>); host.querySelector('input')!.value = 'synthetic-disposable-input';
    if (failure === 'missing') delete window.go;
    await submit();
    expect(host.querySelector('input')).toBeNull();
    expect(host.textContent).toContain('no longer available');
    expect(host.textContent).not.toContain('synthetic-disposable-input');
    expect(host.textContent).not.toContain('private-reply-marker');
  });
  it.each(['Cancel', 'Escape', 'pagehide', 'beforeunload', 'hidden'] as const)('revokes through %s with a single value-free cancellation', async action => {
    const bridge = privateBridge({ id, kind: 'input', title: 'Unlock', labels: ['Password'] });
    await mount(<PrivateApp/>); host.querySelector('input')!.value = 'synthetic-cancelled-input';
    if (action === 'Cancel') await click('Cancel');
    else if (action === 'Escape') await dispatch(window, new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
    else if (action === 'hidden') {
      vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
      await dispatch(document, new Event('visibilitychange'));
    } else await dispatch(window, new Event(action));
    await submit();
    expect(bridge.Reply.mock.calls).toEqual([[{ promptId: id, accepted: false }]]);
    expect(host.querySelector('input')).toBeNull();
  });
  it('keeps visible prompts active and blocks duplicate submissions until the original reply settles', async () => {
    const pending = deferred<boolean>();
    const bridge = privateBridge({ id, kind: 'confirm', title: 'Exact transaction', summary: 'Public exact effects' }, () => pending.promise);
    await mount(<PrivateApp/>);
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    await dispatch(document, new Event('visibilitychange'));
    expect(host.textContent).toContain('Public exact effects');
    await submit(); await submit();
    await dispatch(window, new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(bridge.Reply).toHaveBeenCalledTimes(1);
    await act(async () => { pending.resolve(true); });
  });
  it.each(['false', 'throw'] as const)('reports %s clipboard failure, permits retry and never sends phrase arguments', async failure => {
    const bridge = privateBridge({ id, kind: 'recoveryBackup', title: 'Backup', phrase });
    if (failure === 'false') bridge.Copy.mockResolvedValueOnce(false);
    else bridge.Copy.mockRejectedValueOnce(new Error('private-copy-marker'));
    await mount(<PrivateApp/>); await click('Copy phrase');
    expect(host.textContent).toContain('Could not copy');
    expect(host.textContent).not.toContain('private-copy-marker');
    await click('Copy phrase');
    expect(host.textContent).toContain('Recovery phrase copied');
    expect(bridge.Copy.mock.calls).toEqual([[], []]);
  });
  it.each(['resolve', 'reject'] as const)('ignores a clipboard %s arriving after revocation and erases the QR', async outcome => {
    const pending = deferred<boolean>();
    const bridge = privateBridge({ id, kind: 'receive', title: 'Receive', summary: 'Ethereum', address: '0x123', png: 'iVBORw0KGgo=' });
    bridge.Copy.mockImplementation(() => pending.promise);
    await mount(<PrivateApp/>); const image = host.querySelector('img')!;
    await click('Copy address');
    expect((host.querySelector('.receive-address button') as HTMLButtonElement).disabled).toBe(true);
    await act(async () => { pushed(); });
    expect(image.getAttribute('src')).toBeNull();
    await act(async () => { if (outcome === 'resolve') pending.resolve(true); else pending.reject(new Error('late-copy-error')); });
    expect(host.textContent).not.toContain('copied'); expect(host.textContent).not.toContain('late-copy-error');
    expect(bridge.Reply.mock.calls).toEqual([[{ promptId: id, accepted: false }]]);
  });
  it('acknowledges only the local address copy, without address parameters', async () => {
    const bridge = privateBridge({ id, kind: 'receive', title: 'Receive', summary: '', address: '0x123', png: 'iVBORw0KGgo=' });
    await mount(<PrivateApp/>); await click('Copy address');
    expect(host.textContent).toContain('Address copied.'); expect(bridge.Copy.mock.calls).toEqual([[]]);
  });
  it('blocks browser phrase export, while ordinary input copy remains available', async () => {
    privateBridge({ id, kind: 'recovery', title: 'Record', phrase }); await mount(<PrivateApp/>);
    for (const name of ['copy', 'cut', 'contextmenu', 'dragstart']) {
      const event = new Event(name, { bubbles: true, cancelable: true });
      await dispatch(document, event); expect(event.defaultPrevented).toBe(true);
    }
    for (const key of ['c', 'X', 'p', 's']) {
      const event = new KeyboardEvent('keydown', { key, metaKey: true, cancelable: true });
      await dispatch(window, event); expect(event.defaultPrevented).toBe(true);
    }
    const ordinary = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true });
    await dispatch(window, ordinary); expect(ordinary.defaultPrevented).toBe(false);
    await act(async () => { root!.unmount(); }); root = undefined;
    privateBridge({ id, kind: 'input', title: 'Import', labels: ['Phrase'] }); await mount(<PrivateApp/>);
    const event = new Event('copy', { bubbles: true, cancelable: true });
    await dispatch(document, event); expect(event.defaultPrevented).toBe(false);
  });
  it('throttles private activity without values and tolerates unavailable monitoring', async () => {
    vi.useFakeTimers(); vi.setSystemTime(10000);
    const bridge = privateBridge({ id, kind: 'notice', title: 'Notice', summary: 'Public notice' });
    bridge.Activity.mockRejectedValue(new Error('private-activity-marker'));
    await mount(<PrivateApp/>);
    await dispatch(window, new Event('pointerdown')); await dispatch(window, new Event('keydown'));
    expect(bridge.Activity.mock.calls).toEqual([[]]);
    await act(async () => { vi.advanceTimersByTime(1000); }); await dispatch(window, new Event('keydown'));
    expect(bridge.Activity).toHaveBeenCalledTimes(2);
    expect(host.textContent).not.toContain('private-activity-marker');
    expect([...host.querySelectorAll('button')].map(button => button.textContent)).toEqual(['Close']);
    await click('Close');
    expect(bridge.Reply.mock.calls).toEqual([[{ promptId: id, accepted: true }]]);
    await dispatch(window, new Event('pointerdown')); expect(bridge.Activity).toHaveBeenCalledTimes(2);
  });
  it.each(['resolve', 'reject'] as const)('does not revive a prompt whose delivery %s after teardown', async outcome => {
    const pending = deferred<unknown>(), bridge = privateBridge(null);
    bridge.Prompt.mockImplementation(() => pending.promise);
    await mount(<PrivateApp/>);
    await act(async () => { root!.unmount(); }); root = undefined;
    await act(async () => { if (outcome === 'resolve') pending.resolve({ id, kind: 'recovery', title: 'Record', phrase }); else pending.reject(new Error('late-prompt')); });
    expect(host.textContent).toBe(''); expect(bridge.Reply).not.toHaveBeenCalled();
    expect(unsubscribe).toHaveBeenCalledOnce();
  });
});
