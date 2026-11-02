// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PublicApp } from './PublicApp';
import { validErrorNotice, validView } from './protocol';
import { appendErrorNotice } from './error_notices';
import { id, node, view } from './test-fixtures';
import type { ErrorNotice, Prompt, View, WalletEvent } from './types';

let root: Root | undefined, host: HTMLDivElement;
const events = new Map<string, (...args: unknown[]) => void>();
const failure: ErrorNotice = { ID: 'server-error-1', Title: 'Wallet operation failed', Operation: 'Refresh Arbitrum history', Code: 'RPC_UNAVAILABLE', Message: 'The configured RPC could not be reached.\nNo successful history response was observed.' };
const password: Prompt = { id, kind: 'input', title: 'Unlock local wallet', labels: ['App password'] };
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(accept => { resolve = accept; }); return { promise, resolve }; }
function install(state = view(), prompt: unknown = null) {
  const publicBridge = { Snapshot: vi.fn(async () => state), Event: vi.fn(async (_event: WalletEvent) => true) };
  const dialogs = { Snapshot: vi.fn(async () => prompt), Reply: vi.fn(async () => true), Copy: vi.fn(async () => true), Activity: vi.fn(async () => true) };
  window.go = { webui: { PublicBridge: publicBridge, DialogBridge: dialogs } };
  return { publicBridge, dialogs };
}
async function mount() { await act(async () => { root = createRoot(host); root.render(<PublicApp/>); }); }
async function dispatch(target: EventTarget, event: Event) { await act(async () => { target.dispatchEvent(event); }); }
async function push(state: View) { await act(async () => { events.get('wallet:view')!(state); }); }
async function prompt(value: unknown) { await act(async () => { events.get('wallet:dialog')!(value); }); }
async function close() { await dispatch(host.querySelector('[role="alertdialog"] button')!, new MouseEvent('click', { bubbles: true })); }
beforeEach(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }); document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host); events.clear(); window.runtime = { EventsOn: (name, callback) => { events.set(name, callback); return () => {}; } }; delete window.go; });
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; vi.useRealTimers(); });

describe('structured error dialogs and informational status bar', () => {
  it('shows complete escaped backend details, traps focus and acknowledges only the exact current error', async () => {
    const state = view(); state.Pages![0].Nodes = [node('button', 'refresh', 'Refresh')]; state.ErrorNotice = { ...failure, Message: failure.Message + '\n<img src=x onerror=alert(1)>' };
    const { publicBridge } = install(state); await mount();
    const dialog = host.querySelector('[role="alertdialog"]')!, shell = host.querySelector<HTMLElement>('.wallet-shell')!, button = dialog.querySelector<HTMLButtonElement>('button')!;
    expect(dialog.textContent).toContain(failure.Operation); expect(dialog.textContent).toContain(failure.Code); expect(dialog.textContent).toContain(state.ErrorNotice.Message);
    expect(dialog.querySelector('img')).toBeNull(); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    expect(shell.inert).toBe(true); expect(document.activeElement).toBe(button);
    await act(async () => { host.querySelector<HTMLButtonElement>('[data-action="refresh"]')!.focus(); }); expect(document.activeElement).toBe(button);
    const tab = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }); await dispatch(button, tab); expect(tab.defaultPrevented).toBe(true);
    for (const properties of [{ repeat: true }, { isComposing: true }]) { const enter = new KeyboardEvent('keydown', { key: 'Enter', ...properties, bubbles: true, cancelable: true }); await dispatch(button, enter); expect(enter.defaultPrevented).toBe(true); }
    expect(publicBridge.Event.mock.calls.filter(([event]) => event.ID !== 'activity')).toHaveLength(0);
    await dispatch(button, new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(publicBridge.Event.mock.calls.filter(([event]) => event.ID !== 'activity')).toEqual([[{ ID: 'ackError', Value: failure.ID }]]); expect(host.querySelector('[role="alertdialog"]')).not.toBeNull();
    await push({ ...state, ErrorNotice: null }); expect(host.querySelector('[role="alertdialog"]')).toBeNull(); expect(shell.inert).toBe(false);
  });
  it('defers queued backend and local errors during a real prompt and its pending Reply without losing the prompt input', async () => {
    const state = view(), { dialogs } = install(state, password), reply = deferred<boolean>(); dialogs.Reply.mockImplementation(() => reply.promise);
    await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-ONLY';
    await push({ ...state, ErrorNotice: failure }); expect(host.querySelector('[role="alertdialog"]')).toBeNull(); expect(input.value).toBe('SYNTHETIC-ONLY');
    await dispatch(host.querySelector('form')!, new Event('submit', { bubbles: true, cancelable: true }));
    expect(input.value).toBe(''); expect(host.querySelector('[role="alertdialog"]')).toBeNull();
    await act(async () => { reply.resolve(true); }); expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain(failure.Code);
  });
  it('lets a new real prompt temporarily replace an error dialog, then resumes the same unacknowledged error', async () => {
    const state = { ...view(), ErrorNotice: failure }; install(state); await mount();
    expect(host.querySelector('[role="alertdialog"]')).not.toBeNull(); await prompt(password);
    expect(host.querySelector('[role="alertdialog"]')).toBeNull(); expect(host.querySelector('input')!.type).toBe('password'); expect(host.querySelector<HTMLElement>('.wallet-shell')!.inert).toBe(true);
    await prompt(null); expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain(failure.Code);
  });
  it('keeps copy failure visible inside Receive and queues full details until that real dialog closes', async () => {
    const { dialogs } = install(view(), { id, kind: 'receive', title: 'Receive ETH', address: '0x123', summary: 'Ethereum', png: 'iVBORw0KGgo=' }); dialogs.Copy.mockResolvedValue(false);
    await mount(); await dispatch(host.querySelector('[aria-label="Copy address"]')!, new MouseEvent('click', { bubbles: true }));
    expect(host.querySelector('[role="dialog"] .dialog-error')!.textContent).toContain('Could not copy'); expect(host.querySelector('[role="alertdialog"]')).toBeNull(); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    await dispatch(host.querySelector('form')!, new Event('submit', { bubbles: true, cancelable: true }));
    expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain('CLIPBOARD_REJECTED'); expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain('did not acknowledge');
    await close(); expect(host.querySelector('[role="alertdialog"]')).toBeNull();
  });
  it('uses local dialogs for bridge/protocol failures and retains following backend errors until exact acknowledgement', async () => {
    const state = view(); state.Pages![0].Nodes = [node('button', 'refresh', 'Refresh')]; const { publicBridge } = install(state); publicBridge.Event.mockResolvedValue(false);
    await mount(); await dispatch(host.querySelector('[data-action="refresh"]')!, new MouseEvent('click', { bubbles: true }));
    expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain('WALLET_REQUEST_FAILED'); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    await push({ ...state, ErrorNotice: failure }); expect(host.querySelector('[role="alertdialog"]')!.textContent).not.toContain(failure.Operation);
    await close(); expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain(failure.Operation);
    publicBridge.Event.mockResolvedValue(true); await close(); expect(publicBridge.Event.mock.calls.at(-1)).toEqual([{ ID: 'ackError', Value: failure.ID }]);
    await push({ ...state, ErrorNotice: { ...failure, ID: 'server-error-2', Code: 'NEXT_FAILURE' } });
    await close(); expect(publicBridge.Event.mock.calls.at(-1)).toEqual([{ ID: 'ackError', Value: 'server-error-2' }]);
  });
  it('does not create an error loop when an acknowledgement becomes stale after locking, but reports a real current ack failure', async () => {
    const state = { ...view(), ErrorNotice: failure }, { publicBridge } = install(state), reply = deferred<boolean>(); publicBridge.Event.mockImplementation(() => reply.promise);
    await mount(); await close(); await push({ ...state, ErrorNotice: null }); await act(async () => { reply.resolve(false); });
    expect(host.querySelector('[role="alertdialog"]')).toBeNull(); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    publicBridge.Event.mockResolvedValue(false); await push(state); await close();
    expect(host.querySelector('[role="alertdialog"]')!.textContent).toContain('Acknowledge error');
  });
  it('validates exact ErrorNotice fields and UTF-8 bounds before accepting a backend snapshot', () => {
    expect(validErrorNotice(failure)).toBe(true); expect(validView({ ...view(), ErrorNotice: null })).toBe(true);
    for (const key of ['ID', 'Title', 'Code', 'Message'] as const) expect(validErrorNotice({ ...failure, [key]: '' })).toBe(false);
    for (const [key, maximum] of [['ID', 64], ['Title', 256], ['Operation', 256], ['Code', 80], ['Message', 8192]] as const) {
      expect(validErrorNotice({ ...failure, [key]: 'x'.repeat(maximum) })).toBe(true); expect(validErrorNotice({ ...failure, [key]: 'x'.repeat(maximum + 1) })).toBe(false);
      expect(validErrorNotice({ ...failure, [key]: 'bad\0' })).toBe(false);
    }
    expect(validErrorNotice({ ...failure, rawStack: 'SYNTHETIC-EXCEPTION' })).toBe(false);
    expect(validView({ ...view(), ErrorNotice: { ...failure, Message: 'é'.repeat(4097) } })).toBe(false);
    expect(validView({ ...view(), ErrorNotice: { ...failure, Message: 'é'.repeat(4096) } })).toBe(true);
  });
  it('bounds local error queues with explicit overflow and deduplicates only matching reviewed notices', () => {
    let queue: ErrorNotice[] = [];
    for (let index = 0; index < 100; index++) queue = appendErrorNotice(queue, { ...failure, ID: `local-${index}`, Code: `ERROR_${index}` });
    expect(queue).toHaveLength(32); expect(queue[0].Code).toBe('ERROR_0'); expect(queue[30].Code).toBe('ERROR_30'); expect(queue[31].Code).toBe('ERROR_QUEUE_OVERFLOW'); expect(queue[31].Message).toContain('grouped');
    const one = appendErrorNotice([], failure); expect(appendErrorNotice(one, { ...failure, ID: 'different-id' })).toBe(one);
  });
});
