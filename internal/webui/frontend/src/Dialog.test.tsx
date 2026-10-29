// @vitest-environment happy-dom
// Only public fixtures and synthetic credentials; the real modal components
// share the ordinary application root, bridge and DOM.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogHost, PromptContents } from './Dialog';
import { PublicApp } from './PublicApp';
import { id, node, phrase, view } from './test-fixtures';
import type { Prompt, Reply } from './types';

let root: Root | undefined, host: HTMLDivElement;
const events = new Map<string, (...args: unknown[]) => void>();
const removed = vi.fn();
const inputPrompt: Prompt = { id, kind: 'input', title: 'Unlock local wallet', labels: ['Storage password'] };
const nextID = 'b'.repeat(32);
function deferred<T>() { let resolve!: (value: T) => void, reject!: (error: Error) => void; const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail; }); return { promise, resolve, reject }; }
function bridge(value: unknown = null) {
  const result = { Snapshot: vi.fn(async () => value), Reply: vi.fn(async (_reply: Reply) => true), Copy: vi.fn(async (_promptId: string) => true), Activity: vi.fn(async (_promptId: string) => true) };
  window.go = { webui: { DialogBridge: result } }; return result;
}
async function mount(component = <DialogHost/>) { await act(async () => { root = createRoot(host); root.render(component); }); }
async function dispatch(target: EventTarget, event: Event) { await act(async () => { target.dispatchEvent(event); }); }
async function push(value: unknown) { await act(async () => { events.get('wallet:dialog')!(value); }); }
async function click(label: string) {
  const element = [...host.querySelectorAll('button')].find(button => button.textContent === label);
  expect(element, label).toBeDefined(); await dispatch(element!, new MouseEvent('click', { bubbles: true }));
}
async function submit() { await dispatch(host.querySelector('form')!, new Event('submit', { bubbles: true, cancelable: true })); }
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host);
  events.clear(); removed.mockReset(); delete window.go;
  window.runtime = { EventsOn: (name, callback) => { events.set(name, callback); return removed; } };
});
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; vi.restoreAllMocks(); vi.useRealTimers(); });

describe('ordinary in-window wallet dialogs', () => {
  it('starts without a modal and displays subsequent requests in the same application root', async () => {
    const dialogs = bridge(), publicView = view(); publicView.Pages![0].Nodes = [node('button', 'refresh', 'Refresh')];
    window.go!.webui!.PublicBridge = { Snapshot: async () => publicView, Event: async () => true };
    await mount(<PublicApp/>);
    expect(host.querySelector('.wallet-shell')).not.toBeNull(); expect(host.querySelector('[role="dialog"]')).toBeNull();
    await push(inputPrompt);
    expect(host.querySelectorAll('.wallet-shell')).toHaveLength(1);
    expect(host.querySelectorAll('[role="dialog"]')).toHaveLength(1);
    expect(host.querySelector('input')!.type).toBe('password');
    expect(host.querySelector('button.primary')!.textContent).toBe('Unlock');
    expect(dialogs.Snapshot).toHaveBeenCalledOnce();
  });
  it('keeps a newer event when the initial snapshot resolves late', async () => {
    const snapshot = deferred<unknown>(), api = bridge(); api.Snapshot.mockImplementation(() => snapshot.promise);
    await mount();
    await push({ ...inputPrompt, id: nextID, title: 'Current request' });
    await act(async () => { snapshot.resolve(inputPrompt); });
    expect(host.textContent).toContain('Current request'); expect(host.textContent).not.toContain('Unlock local wallet');
  });
  it('does not erase an edited field when the same dialog is observed again', async () => {
    bridge(inputPrompt); await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-EDIT';
    await push({ ...inputPrompt }); expect(input.value).toBe('SYNTHETIC-EDIT');
  });
  it('clears sensitive DOM before replying and reuses the modal for a consecutive setup request', async () => {
    const api = bridge(inputPrompt), pending = deferred<boolean>(); let sent: Reply | undefined;
    api.Reply.mockImplementation(value => { expect(host.querySelector('input')?.value || '').toBe(''); sent = structuredClone(value); return pending.promise; });
    await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-PASSWORD';
    await submit(); expect(input.value).toBe(''); expect(host.querySelector('input')).toBeNull();
    expect(sent).toEqual({ promptId: id, accepted: true, values: ['SYNTHETIC-PASSWORD'] });
    await push({ id: nextID, kind: 'recovery', title: 'Record recovery phrase', phrase });
    await act(async () => pending.resolve(false));
    expect(host.querySelectorAll('[data-recovery-word]')).toHaveLength(12);
    expect(host.textContent).not.toContain('could not be completed');
    await push(null); expect(host.textContent).not.toContain('abandon');
  });
  it('rejects excessive UTF-8 input locally and allows a corrected submit', async () => {
    const api = bridge(inputPrompt); await mount(); const input = host.querySelector('input')!;
    input.value = 'é'.repeat(2049); await submit();
    expect(api.Reply).not.toHaveBeenCalled(); expect(host.textContent).toContain('4096 bytes');
    input.value = 'SYNTHETIC-CORRECTED'; await submit(); expect(api.Reply).toHaveBeenCalledOnce();
  });
  it('submits Enter once, ignores composition/repeat and labels Unlock explicitly', async () => {
    const api = bridge(inputPrompt); await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-ENTER';
    for (const properties of [{ repeat: true }, { isComposing: true }]) await dispatch(input, new KeyboardEvent('keydown', { key: 'Enter', ...properties, bubbles: true, cancelable: true }));
    expect(api.Reply).not.toHaveBeenCalled();
    await dispatch(input, new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    expect(api.Reply).toHaveBeenCalledOnce(); expect(host.querySelector('input')).toBeNull();
  });
  it.each(['Escape', 'Cancel'])('cancels through %s without sending entered values', async action => {
    const api = bridge(inputPrompt); await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-CANCEL';
    if (action === 'Cancel') await click('Cancel');
    else await dispatch(input, new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(api.Reply.mock.calls).toEqual([[{ promptId: id, accepted: false }]]); expect(input.value).toBe('');
  });
  it('traps Tab and focus inside the dialog and restores the prior wallet control after dismissal', async () => {
    bridge(); const state = view(); state.Pages![0].Nodes = [node('button', 'refresh', 'Refresh')];
    window.go!.webui!.PublicBridge = { Snapshot: async () => state, Event: async () => true };
    await mount(<PublicApp/>); const background = host.querySelector('[data-action="refresh"]') as HTMLButtonElement; background.focus();
    await push(inputPrompt); const shell = host.querySelector<HTMLElement>('.wallet-shell')!, input = host.querySelector('input')!, primary = host.querySelector('button.primary') as HTMLButtonElement;
    expect(shell.inert).toBe(true); expect(shell.getAttribute('aria-hidden')).toBe('true'); expect(document.activeElement).toBe(input);
    const backwards = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }); await dispatch(input, backwards);
    expect(backwards.defaultPrevented).toBe(true); expect(document.activeElement).toBe(primary);
    const forwards = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }); await dispatch(primary, forwards);
    expect(forwards.defaultPrevented).toBe(true); expect(document.activeElement).toBe(input);
    await act(async () => background.focus()); expect(document.activeElement).toBe(input);
    await push(null); expect(shell.inert).toBe(false); expect(shell.hasAttribute('aria-hidden')).toBe(false); expect(document.activeElement).toBe(background);
  });
  it('does not cancel when switching apps, hiding the page or opening native menus', async () => {
    const api = bridge(inputPrompt); await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-PRESERVED';
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    await dispatch(document, new Event('visibilitychange'));
    for (const name of ['blur', 'pagehide', 'beforeunload']) await dispatch(window, new Event(name));
    expect(input.value).toBe('SYNTHETIC-PRESERVED'); expect(api.Reply).not.toHaveBeenCalled();
    await push(null); expect(input.value).toBe(''); expect(host.querySelector('[role="dialog"]')).toBeNull();
  });
  it.each(['recovery', 'recoveryBackup'] as const)('uses normal selection, context menus and explicit copying for %s words', async kind => {
    const api = bridge({ id, kind, title: 'Recovery phrase', phrase }); await mount();
    expect(host.querySelectorAll('[data-recovery-word]')).toHaveLength(12); expect(api.Copy).not.toHaveBeenCalled();
    const words = host.querySelector('.recovery-words')!;
    for (const name of ['copy', 'cut', 'paste', 'contextmenu', 'dragstart']) { const event = new Event(name, { bubbles: true, cancelable: true }); await dispatch(words, event); expect(event.defaultPrevented).toBe(false); }
    for (const shiftKey of [false, true]) { const event = new KeyboardEvent('keydown', { key: 'c', ctrlKey: true, shiftKey, bubbles: true, cancelable: true }); await dispatch(words, event); expect(event.defaultPrevented).toBe(false); }
    await click('Copy phrase'); expect(api.Copy.mock.calls).toEqual([[id]]); expect(host.textContent).toContain('Recovery phrase copied.');
    await push(null); expect(words.textContent).toBe(''); expect(host.textContent).not.toContain('abandon');
  });
  it('shows the local receive QR/address and copies without replacing the main window', async () => {
    const api = bridge({ id, kind: 'receive', title: 'Receive ETH', summary: 'Ethereum mainnet', address: '0x123', png: 'iVBORw0KGgo=' }); await mount();
    expect(host.querySelector('.dialog-receive')).not.toBeNull(); const image = host.querySelector('img')!;
    expect(image.src).toBe('data:image/png;base64,iVBORw0KGgo='); expect(host.querySelector('code')!.textContent).toBe('0x123');
    await click('Copy address'); expect(api.Copy.mock.calls).toEqual([[id]]); expect(host.textContent).toContain('Address copied.');
    await push(null); expect(image.getAttribute('src')).toBeNull(); expect(host.querySelector('img')).toBeNull();
  });
  it.each(['false', 'throw'] as const)('shows a fixed %s copy failure and permits a retry', async failure => {
    const api = bridge({ id, kind: 'recoveryBackup', title: 'Backup', phrase });
    if (failure === 'false') api.Copy.mockResolvedValueOnce(false); else api.Copy.mockRejectedValueOnce(new Error('SYNTHETIC-COPY-INTERNAL'));
    await mount(); await click('Copy phrase'); expect(host.textContent).toContain('Could not copy.'); expect(host.textContent).not.toContain('SYNTHETIC-COPY-INTERNAL');
    await click('Copy phrase'); expect(api.Copy).toHaveBeenCalledTimes(2); expect(host.textContent).toContain('Recovery phrase copied.');
  });
  it.each(['resolve', 'reject'] as const)('ignores a late clipboard %s after the dialog is replaced', async outcome => {
    const pending = deferred<boolean>(), api = bridge({ id, kind: 'recoveryBackup', title: 'Backup', phrase }); api.Copy.mockImplementation(() => pending.promise);
    await mount(); await click('Copy phrase');
    expect((host.querySelector('.phrase-copy button') as HTMLButtonElement).disabled).toBe(true);
    await push({ ...inputPrompt, id: nextID });
    await act(async () => { if (outcome === 'resolve') pending.resolve(true); else pending.reject(new Error('SYNTHETIC-LATE-COPY')); });
    expect(host.textContent).not.toContain('copied'); expect(host.textContent).not.toContain('SYNTHETIC-LATE-COPY'); expect(host.querySelector('input')).not.toBeNull();
  });
  it('correlates an old Receive copy and a newer recovery copy to their own dialog IDs', async () => {
    const pending = deferred<boolean>(), api = bridge({ id, kind: 'receive', title: 'Receive', summary: 'Ethereum mainnet', address: '0x123', png: 'iVBORw0KGgo=' });
    api.Copy.mockImplementationOnce(() => pending.promise); await mount(); await click('Copy address');
    await push({ id: nextID, kind: 'recoveryBackup', title: 'Recovery phrase', phrase }); await click('Copy phrase');
    expect(api.Copy.mock.calls).toEqual([[id], [nextID]]); expect(host.textContent).toContain('Recovery phrase copied.');
    await act(async () => pending.resolve(false)); expect(host.textContent).toContain('Recovery phrase copied.'); expect(host.textContent).not.toContain('Could not copy.');
  });
  it.each(['false', 'throw'] as const)('restores the current dialog with empty fields after a %s reply and permits a manual retry', async failure => {
    const api = bridge(inputPrompt); if (failure === 'false') api.Reply.mockResolvedValueOnce(false); else api.Reply.mockRejectedValueOnce(new Error('SYNTHETIC-REPLY-INTERNAL'));
    await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-FIRST-ENTRY'; await submit();
    expect(input.value).toBe(''); const restored = host.querySelector('input')!; expect(restored).not.toBe(input); expect(restored.value).toBe('');
    expect(api.Snapshot).toHaveBeenCalledTimes(2); expect(api.Reply).toHaveBeenCalledOnce(); expect(host.textContent).toContain('previous reply was not accepted'); expect(host.textContent).not.toContain('SYNTHETIC');
    let retried: Reply | undefined; api.Reply.mockImplementation(async reply => { retried = structuredClone(reply); return true; });
    restored.value = 'SYNTHETIC-RETRY'; await submit(); expect(retried).toEqual({ promptId: id, accepted: true, values: ['SYNTHETIC-RETRY'] }); expect(api.Reply).toHaveBeenCalledTimes(2); expect(host.querySelector('input')).toBeNull();
  });
  it.each(['missing', 'invalid', 'throw'] as const)('gives fixed restart guidance when the failed reply snapshot is %s', async failure => {
    const api = bridge(inputPrompt); api.Reply.mockResolvedValue(false); await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-FAILED-ENTRY';
    if (failure === 'missing') delete window.go; else if (failure === 'invalid') api.Snapshot.mockResolvedValue({ invalid: true }); else api.Snapshot.mockRejectedValue(new Error('SYNTHETIC-SNAPSHOT-FAILURE'));
    await submit(); expect(input.value).toBe(''); expect(host.querySelector('input')).toBeNull(); expect(host.textContent).toContain('Restart the app'); expect(host.textContent).not.toContain('SYNTHETIC');
  });
  it('does not restore a completed dialog when the reply result was lost but the current snapshot is null', async () => {
    const api = bridge(inputPrompt); await mount(); api.Reply.mockRejectedValue(new Error('SYNTHETIC-RESULT-LOST')); api.Snapshot.mockResolvedValue(null);
    await submit(); expect(api.Reply).toHaveBeenCalledOnce(); expect(host.querySelector('[role="dialog"]')).toBeNull(); expect(host.textContent).toContain('could not be completed');
  });
  it.each(['lock', 'replace', 'unmount'] as const)('does not resurrect an old dialog when its recovery snapshot arrives after %s', async action => {
    const api = bridge(inputPrompt), snapshot = deferred<unknown>(); api.Reply.mockResolvedValue(false); await mount(); api.Snapshot.mockImplementation(() => snapshot.promise); await submit();
    if (action === 'unmount') { await act(async () => root!.unmount()); root = undefined; }
    else await push(action === 'lock' ? null : { ...inputPrompt, id: nextID, title: 'New current request' });
    await act(async () => snapshot.resolve(inputPrompt)); expect(host.textContent).not.toContain('Unlock local wallet');
    expect(host.querySelectorAll('input')).toHaveLength(action === 'replace' ? 1 : 0); expect(api.Reply).toHaveBeenCalledOnce();
    if (action === 'replace') expect(host.textContent).toContain('New current request');
  });
  it.each(['invalid', 'rejected'] as const)('does not render malformed or unavailable %s dialog data', async mode => {
    const api = bridge({ ...inputPrompt, labels: [], title: 'SYNTHETIC-INVALID-DATA' }); if (mode === 'rejected') api.Snapshot.mockRejectedValue(new Error('SYNTHETIC-SNAPSHOT-ERROR'));
    await mount(); expect(host.querySelector('input')).toBeNull(); expect(host.textContent).toContain('could not be completed'); expect(host.textContent).not.toContain('SYNTHETIC');
  });
  it('ignores snapshot and event callbacks after unmount', async () => {
    const pending = deferred<unknown>(), api = bridge(); api.Snapshot.mockImplementation(() => pending.promise); await mount();
    await act(async () => root!.unmount()); root = undefined;
    await act(async () => { events.get('wallet:dialog')!({ id, kind: 'recovery', title: 'Late', phrase }); pending.resolve(inputPrompt); });
    expect(host.textContent).toBe(''); expect(api.Reply).not.toHaveBeenCalled(); expect(removed).toHaveBeenCalledOnce();
  });
  it('clears a held input reference when the modal component unmounts', async () => {
    bridge(inputPrompt); await mount(); const input = host.querySelector('input')!; input.value = 'SYNTHETIC-TEARDOWN';
    await act(async () => root!.unmount()); root = undefined; expect(input.value).toBe('');
  });
  it('throttles value-free activity and offers a single Close action for notices', async () => {
    vi.useFakeTimers(); vi.setSystemTime(10000); const api = bridge({ id, kind: 'notice', title: 'Notice', summary: 'Public notice' }); api.Activity.mockRejectedValue(new Error('SYNTHETIC-ACTIVITY'));
    await mount(); await dispatch(window, new Event('pointerdown')); await dispatch(window, new Event('keydown')); expect(api.Activity.mock.calls).toEqual([[id]]);
    await act(async () => vi.advanceTimersByTime(1000)); await dispatch(window, new Event('pointerdown')); expect(api.Activity).toHaveBeenCalledTimes(2);
    await push({ id: nextID, kind: 'notice', title: 'Next notice', summary: 'Public notice' }); await act(async () => vi.advanceTimersByTime(1000)); await dispatch(window, new Event('pointerdown'));
    expect(api.Activity.mock.calls).toEqual([[id], [id], [nextID]]);
    expect([...host.querySelectorAll('button')].map(button => button.textContent)).toEqual(['Close']); await click('Close'); expect(api.Reply.mock.calls).toEqual([[{ promptId: nextID, accepted: true }]]);
  });
  it('renders approval text literally and keeps ordinary clipboard APIs outside the component', () => {
    const text = renderToStaticMarkup(<PromptContents prompt={{ id, kind: 'confirm', title: 'Confirm', summary: '<iframe src="https://bad.invalid">' }} error="" copyStatus="" copying={false} copy={() => {}}/>);
    expect(text).toContain('&lt;iframe'); expect(text).not.toContain('<iframe');
  });
});
