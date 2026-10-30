// @vitest-environment happy-dom
// Public address fixtures only. Native clipboard integration is tested on an
// owned desktop separately; these tests verify the shipped React event flow.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PublicApp } from './PublicApp';
import { node, view } from './test-fixtures';
import type { View, WalletEvent } from './types';

const address = '0x9858EfFD232B4033E47d90003D41EC34EcaEda94';
let root: Root | undefined, host: HTMLDivElement;
const events = new Map<string, (...args: unknown[]) => void>();
function deferred<T>() { let resolve!: (value: T) => void, reject!: (error: Error) => void; const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail; }); return { promise, resolve, reject }; }
function overview(value = address) {
  const state = view(), row = node('row', 'open/public');
  row.Children = [node('button', 'open/public', 'Ethereum'), node('label', '', 'Unknown ETH'), node('address', '', '', value), node('button', 'copyReceive/public', '⧉', value)];
  state.Pages![0].Nodes = [node('button', 'refresh', 'Refresh'), row]; return state;
}
function install(state = overview()) {
  const bridge = { Snapshot: vi.fn(async () => state), Event: vi.fn(async (_event: WalletEvent) => true) }, clipboard = vi.fn(async (_value: string) => true);
  window.go = { webui: { PublicBridge: bridge } }; window.runtime!.ClipboardSetText = clipboard;
  return { bridge, clipboard };
}
async function mount() { await act(async () => { root = createRoot(host); root.render(<PublicApp/>); }); }
async function click(action: string) { const button = host.querySelector(`[data-action="${action}"]`); expect(button).not.toBeNull(); await act(async () => { button!.dispatchEvent(new MouseEvent('click', { bubbles: true })); }); }
async function push(state: View) { await act(async () => { events.get('wallet:view')!(state); }); }
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host); events.clear(); delete window.go;
  window.runtime = { EventsOn: (name, callback) => { events.set(name, callback); return () => {}; } };
});
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; vi.useRealTimers(); });

describe('direct copying of displayed public receive addresses', () => {
  it('copies immediately while a backend action is stalled, without adding a wallet event or opening the wallet', async () => {
    const { bridge, clipboard } = install(), pending = deferred<boolean>(); bridge.Event.mockImplementationOnce(() => pending.promise);
    await mount(); await click('refresh'); expect(bridge.Event).toHaveBeenCalledOnce(); await click('copyReceive/public');
    expect(clipboard.mock.calls).toEqual([[address]]); expect(bridge.Event.mock.calls).toEqual([[{ ID: 'refresh', Value: '' }]]);
    expect(host.querySelector('.wallet-table code')!.textContent).toBe(address); expect(host.querySelector('[role="dialog"]')).toBeNull();
    expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.'); expect(host.textContent).not.toContain('Waiting for wallet confirmation');
    await act(async () => pending.resolve(true));
  });
  it('does not require an available wallet bridge to copy already displayed public data', async () => {
    const { bridge, clipboard } = install(); await mount(); delete window.go; await click('copyReceive/public');
    expect(clipboard).toHaveBeenCalledWith(address); expect(bridge.Event).not.toHaveBeenCalled(); expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.');
  });
  it.each([address, 'alice.near', '0x' + '1'.repeat(64), 'EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c'])('copies the complete displayed address without rewriting it: %s', async value => {
    const { clipboard, bridge } = install(overview(value)); await mount(); await click('copyReceive/public');
    expect(host.querySelector('code')!.textContent).toBe(value); expect(clipboard.mock.calls).toEqual([[value]]); expect(bridge.Event).not.toHaveBeenCalled();
  });
  it('does not announce success until the native clipboard has acknowledged the write', async () => {
    const { clipboard } = install(), pending = deferred<boolean>(); clipboard.mockImplementation(() => pending.promise);
    await mount(); await click('copyReceive/public'); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    await act(async () => pending.resolve(true)); expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.');
  });
  it.each(['false', 'throw', 'synchronous throw', 'missing method', 'missing runtime'] as const)('shows fixed clipboard failure for %s without falling back to the signer', async failure => {
    const { clipboard, bridge } = install(); await mount();
    if (failure === 'false') clipboard.mockResolvedValue(false);
    else if (failure === 'throw') clipboard.mockRejectedValue(new Error('SYNTHETIC-CLIPBOARD-DETAIL'));
    else if (failure === 'synchronous throw') clipboard.mockImplementation(() => { throw new Error('SYNTHETIC-CLIPBOARD-DETAIL'); });
    else if (failure === 'missing method') delete window.runtime!.ClipboardSetText;
    else delete window.runtime;
    await click('copyReceive/public'); expect(host.querySelector('.status-bar')!.textContent).toBe('Could not copy address. Try again.');
    expect(host.textContent).not.toContain('SYNTHETIC'); expect(host.textContent).not.toContain('Address copied.'); expect(bridge.Event).not.toHaveBeenCalled();
  });
  it('restarts the seven-second status timeout after every successful copy', async () => {
    vi.useFakeTimers(); install(); await mount(); await click('copyReceive/public');
    await act(async () => vi.advanceTimersByTime(6000)); await click('copyReceive/public');
    await act(async () => vi.advanceTimersByTime(1000)); expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.');
    await act(async () => vi.advanceTimersByTime(5999)); expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.');
    await act(async () => vi.advanceTimersByTime(1)); expect(host.querySelector('.status-bar')!.textContent).toBe('');
  });
  it('continues the next copy after an older write fails without publishing stale failure feedback', async () => {
    const { clipboard } = install(), old = deferred<boolean>(); clipboard.mockImplementationOnce(() => old.promise);
    await mount(); await click('copyReceive/public'); await click('copyReceive/public'); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    await act(async () => old.reject(new Error('SYNTHETIC-OLD-COPY'))); expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.');
  });
  it.each(['acknowledged', 'rejected'] as const)('serializes two distinct address writes when the first is %s, leaving the latest click in the native clipboard', async outcome => {
    const state = overview(), secondAddress = 'alice.near', secondRow = node('row', 'open/second');
    secondRow.Children = [node('button', 'open/second', 'NEAR'), node('label', '', 'Unknown NEAR'), node('address', '', '', secondAddress), node('button', 'copyReceive/second', '⧉', secondAddress)]; state.Pages![0].Nodes!.push(secondRow);
    const { clipboard, bridge } = install(state), first = deferred<boolean>(); let nativeValue = 'public-native-sentinel';
    clipboard.mockImplementationOnce(async value => { const ok = await first.promise; if (ok) nativeValue = value; return ok; });
    clipboard.mockImplementationOnce(async value => { nativeValue = value; return true; });
    await mount(); await click('copyReceive/public'); await click('copyReceive/second');
    expect(clipboard.mock.calls).toEqual([[address]]); expect(nativeValue).toBe('public-native-sentinel');
    await act(async () => { if (outcome === 'acknowledged') first.resolve(true); else first.reject(new Error('SYNTHETIC-FIRST-WRITE')); });
    expect(clipboard.mock.calls).toEqual([[address], [secondAddress]]); expect(nativeValue).toBe(secondAddress);
    expect(bridge.Event).not.toHaveBeenCalled(); expect(host.querySelector('.status-bar')!.textContent).toBe('Address copied.');
  });
  it.each(['address change', 'lock', 'disabled', 'page change', 'unmount'] as const)('ignores an old clipboard result after %s', async change => {
    const { clipboard } = install(), pending = deferred<boolean>(); clipboard.mockImplementation(() => pending.promise); await mount(); await click('copyReceive/public');
    const next = change === 'lock' ? view() : overview(change === 'address change' ? 'alice.near' : address); next.Status = 'Current wallet state.'; next.StatusRevision++;
    if (change === 'disabled') next.Pages![0].Nodes![1].Children![3].Disabled = true;
    if (change === 'page change') { next.Pages!.push({ ID: 'wallet/public', Title: 'Ethereum', Nodes: next.Pages![0].Nodes }); next.Active = 'wallet/public'; }
    if (change === 'unmount') { await act(async () => root!.unmount()); root = undefined; } else await push(next);
    await act(async () => pending.resolve(true)); expect(host.textContent).not.toContain('Address copied.'); expect(host.textContent).not.toContain('Could not copy');
    if (change !== 'unmount') expect(host.querySelector('.status-bar')!.textContent).toBe('Current wallet state.');
  });
  it('does not replace the current wallet feedback after another public action starts', async () => {
    const { clipboard } = install(), pending = deferred<boolean>(); clipboard.mockImplementation(() => pending.promise); await mount(); await click('copyReceive/public'); await click('refresh');
    const state = overview(); state.Status = 'Refresh started.'; state.StatusRevision++; await push(state);
    await act(async () => pending.reject(new Error('SYNTHETIC-OLD-COPY'))); expect(host.querySelector('.status-bar')!.textContent).toBe('Refresh started.');
  });
  it.each(['address change', 'lock'] as const)('keeps an old clipboard attempt invalid after a temporary %s is reversed', async change => {
    const { clipboard } = install(), pending = deferred<boolean>(); clipboard.mockImplementation(() => pending.promise); await mount(); await click('copyReceive/public');
    await push(change === 'lock' ? view() : overview('alice.near'));
    const restored = overview(); restored.Status = 'Current profile restored.'; restored.StatusRevision++; await push(restored);
    await act(async () => pending.resolve(false)); expect(host.querySelector('.status-bar')!.textContent).toBe('Current profile restored.');
  });
  it('preserves newer backend feedback when the address remains unchanged', async () => {
    const { clipboard } = install(), pending = deferred<boolean>(); clipboard.mockImplementation(() => pending.promise); await mount(); await click('copyReceive/public');
    const newer = overview(); newer.Status = 'Newer wallet feedback.'; newer.StatusRevision++; await push(newer);
    await act(async () => pending.resolve(false)); expect(host.querySelector('.status-bar')!.textContent).toBe('Newer wallet feedback.');
  });
  it('does not write an empty address or dispatch its copy event', async () => {
    const { clipboard, bridge } = install(overview('')); await mount(); await click('copyReceive/public');
    expect(clipboard).not.toHaveBeenCalled(); expect(bridge.Event).not.toHaveBeenCalled(); expect(host.querySelector('.status-bar')!.textContent).toBe('Could not copy address. Try again.');
  });
  it('keeps a disabled copy button inert', async () => {
    const state = overview(); state.Pages![0].Nodes![1].Children![3].Disabled = true; const { clipboard, bridge } = install(state); await mount(); await click('copyReceive/public');
    expect(clipboard).not.toHaveBeenCalled(); expect(bridge.Event).not.toHaveBeenCalled();
  });
});
