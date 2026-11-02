// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Field, PublicApp } from './PublicApp';
import { node, view } from './test-fixtures';
import type { WalletEvent } from './types';

let root: Root | undefined, host: HTMLDivElement;
async function render(element: React.ReactNode) { await act(async () => { if (!root) root = createRoot(host); root.render(element); }); }
async function edit(input: HTMLInputElement, value: string) { await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })); }); }
beforeEach(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }); document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host); delete window.go; window.runtime = { EventsOn: () => () => {} }; });
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; vi.useRealTimers(); });
const rejection = 'Input is too long or contains unsupported characters. The previous value was kept.';

describe('public input transport limits keep visible and authoritative drafts consistent', () => {
  it.each([
    ['recipient', '0x' + '1'.repeat(40), 'é'.repeat(2200), '0x' + '2'.repeat(40)],
    ['recipient', '0x' + '1'.repeat(40), 'NUL\0INPUT', '0x' + '2'.repeat(40)],
    ['amount', '1.25', '123\0', '2.000000000000000001'],
  ])('keeps the previous %s visible after a rejected edit, reports fixed footer feedback and accepts the next valid edit', async (field, previous, rejected, next) => {
    const state = view(); state.Active = 'wallet/owned';
    const recipient = node('entry', 'field/recipient/owned', 'Recipient address', field === 'recipient' ? previous : '0x' + '1'.repeat(40));
    const amount = { ...node('entry', 'field/amount/owned', 'Amount', field === 'amount' ? previous : '1.25'), InputMode: 'decimal' as const };
    state.Pages!.push({ ID: 'wallet/owned', Title: 'Ethereum', ParentID: 'group/0', Nodes: [recipient, amount, node('button', 'prepare/owned', 'Send payment')] });
    const bridge = { Snapshot: vi.fn(async () => state), Event: vi.fn(async (_event: WalletEvent) => true) };
    window.go = { webui: { PublicBridge: bridge } };
    await render(<PublicApp/>);
    const input = host.querySelector<HTMLInputElement>(`[id="wallet-field/${field}/owned"]`)!;
    await edit(input, rejected);
    expect(input.value).toBe(previous); expect(bridge.Event).not.toHaveBeenCalled();
    expect(host.querySelector('.status-bar')!.textContent).toBe(rejection); expect(host.textContent).not.toContain('NUL');
    expect(host.querySelector('.field-error')).toBeNull();
    await edit(input, next); expect(input.value).toBe(next);
    expect(bridge.Event.mock.calls).toEqual([[{ ID: `field/${field}/owned`, Value: next }]]);
  });
  it('uses the UTF-8 byte boundary without truncating or placing rejected values into the local notification', async () => {
    const field = node('entry', 'field/recipient/owned', 'Recipient address', 'original'), emit = vi.fn(), notifications: Event[] = [];
    const listener = (event: Event) => notifications.push(event); window.addEventListener('wallet:input-rejected', listener);
    try {
      await render(<Field node={field} emit={emit}/>); const input = host.querySelector('input')!;
      const boundary = 'é'.repeat(2048); await edit(input, boundary); expect(input.value).toBe(boundary);
      expect(emit.mock.calls).toEqual([['field/recipient/owned', boundary]]);
      await edit(input, boundary + 'é'); expect(input.value).toBe(boundary); expect(emit).toHaveBeenCalledTimes(1);
      expect(notifications).toHaveLength(1); expect((notifications[0] as CustomEvent).detail).toBeUndefined();
    } finally { window.removeEventListener('wallet:input-rejected', listener); }
  });
  it('does not contaminate focused pending echoes with rejected edits', async () => {
    const field = node('entry', 'field/recipient/owned', 'Recipient', 'old'), emit = vi.fn();
    await render(<Field node={field} emit={emit}/>); const input = host.querySelector('input')!; input.focus();
    await edit(input, 'valid-first'); await edit(input, 'bad\0'); expect(input.value).toBe('valid-first');
    await edit(input, 'valid-second');
    await render(<Field node={{ ...field, Value: 'valid-first' }} emit={emit}/>); expect(input.value).toBe('valid-second');
    await render(<Field node={{ ...field, Value: 'valid-second' }} emit={emit}/>); expect(input.value).toBe('valid-second');
    expect(emit.mock.calls).toEqual([['field/recipient/owned', 'valid-first'], ['field/recipient/owned', 'valid-second']]);
  });
  it('auto-hides repeated rejection feedback after seven seconds and removes the notification listener on teardown', async () => {
    vi.useFakeTimers(); const state = view(); state.Pages![0].Nodes = [node('entry', 'field/name', 'Name', 'old')];
    window.go = { webui: { PublicBridge: { Snapshot: async () => state, Event: vi.fn(async () => true) } } };
    await render(<PublicApp/>); const input = host.querySelector('input')!;
    await edit(input, 'bad\0'); await act(async () => { vi.advanceTimersByTime(6000); });
    await edit(input, 'bad\0'); await act(async () => { vi.advanceTimersByTime(1000); }); expect(host.querySelector('.status-bar')!.textContent).toBe(rejection);
    await act(async () => { vi.advanceTimersByTime(6000); }); expect(host.querySelector('.status-bar')!.textContent).toBe('');
    await act(async () => { root!.unmount(); }); root = undefined;
    await act(async () => { window.dispatchEvent(new Event('wallet:input-rejected')); }); expect(host.textContent).toBe('');
  });
});
