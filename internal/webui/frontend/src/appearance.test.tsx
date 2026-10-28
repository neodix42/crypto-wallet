// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyAppearance, validAppearance, validPrompt, validView } from './protocol';
import { PublicApp, PublicNode, WalletShell } from './PublicApp';
import { PrivateApp } from './PrivateApp';
import { id, node, view } from './test-fixtures';
import type { Prompt, Reply } from './types';

let host: HTMLDivElement, root: Root | undefined;
async function mount(value: React.ReactNode) { await act(async () => { root = createRoot(host); root.render(value); }); }
async function key(target: EventTarget, options: KeyboardEventInit = {}) { await act(async () => { target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true, ...options })); }); }
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = ''; host = document.createElement('div'); document.body.append(host);
  delete window.go; delete window.runtime; delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.fontSize;
});
afterEach(async () => { if (root) await act(async () => { root!.unmount(); }); root = undefined; vi.restoreAllMocks(); });

describe('bounded public appearance preferences', () => {
  it('uses Light18 by default and accepts only finite allowlisted presentation values', () => {
    applyAppearance();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.dataset.fontSize).toBe('18');
    for (const theme of ['light', 'dark']) for (const fontSize of [14, 16, 18, 20, 22, 24]) {
      expect(validAppearance({ theme, fontSize })).toBe(true);
    }
    for (const value of [null, [], {}, { theme: 'LIGHT', fontSize: 18 }, { theme: 'url(https://remote.invalid)', fontSize: 18 }, { theme: 'dark', fontSize: 25 }, { theme: 'light', fontSize: 17 }, { theme: 'light', fontSize: '18' }, { theme: 'light', fontSize: Infinity }, { theme: 'light', fontSize: 18, password: 'SYNTHETIC' }]) {
      expect(validAppearance(value)).toBe(false);
      applyAppearance(value as never);
      expect(document.documentElement.dataset.theme).toBe('light');
      expect(document.documentElement.dataset.fontSize).toBe('18');
      expect(validView({ ...view(), Appearance: value })).toBe(false);
      expect(validPrompt({ id, kind: 'input', title: 'Unlock', labels: ['Password'], appearance: value })).toBe(false);
    }
    expect(validView({ ...view(), Appearance: { theme: 'dark', fontSize: 24 } })).toBe(true);
    expect(validPrompt({ id, kind: 'input', title: 'Unlock', labels: ['Password'], appearance: { theme: 'dark', fontSize: 24 } })).toBe(true);
  });
  it('applies only Go-delivered public preferences and marks the selected settings subtab', async () => {
    const state = { ...view(), Appearance: { theme: 'dark' as const, fontSize: 22 } };
    window.go = { webui: { PublicBridge: { Snapshot: async () => state, Event: vi.fn(async () => true) } } };
    await mount(<PublicApp/>);
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.fontSize).toBe('22');
    const sections = node('row', 'settingsSections', 'Settings sections', 'Appearance');
    sections.Children = [node('button', 'settingsWallet', 'Wallet'), node('button', 'settingsAppearance', 'Appearance')];
    await act(async () => { root!.render(<PublicNode node={sections} emit={vi.fn()}/>); });
    expect(host.querySelector('[role="tablist"]')?.getAttribute('aria-label')).toBe('Settings sections');
    const selected = host.querySelector('[aria-selected="true"]');
    expect(selected?.textContent).toBe('Appearance');
    expect(selected?.classList.contains('selected')).toBe(true);
    expect(host.querySelector('[data-action="settingsWallet"]')?.getAttribute('aria-selected')).toBe('false');
  });
});

describe('Enter unlock without expanding secret authority', () => {
  it('focuses Unlock and routes Enter once from the locked public page only', async () => {
    const state = view(); state.Active = 'group/0';
    state.Pages = [{ ID: 'group/0', Title: 'Wallet', Nodes: [node('button', 'unlock', 'Unlock')] }, { ID: 'settings', Title: 'Settings', Nodes: [] }];
    const emit = vi.fn(); await mount(<WalletShell view={state} status="" emit={emit}/>);
    const unlock = host.querySelector('[data-action="unlock"]')!;
    expect(document.activeElement).toBe(unlock);
    await key(window); expect(emit).toHaveBeenCalledExactlyOnceWith('unlock'); emit.mockClear();
    for (const options of [{ repeat: true }, { ctrlKey: true }, { metaKey: true }, { altKey: true }, { key: 'Escape' }]) await key(window, options);
    await key(unlock); expect(emit).not.toHaveBeenCalled();
    state.Pages[0].Nodes![0].Disabled = true;
    await act(async () => { root!.render(<WalletShell view={state} status="" emit={emit}/>); });
    await key(window); expect(emit).not.toHaveBeenCalled();
    state.Pages[0].Nodes![0].Disabled = false; state.Active = 'settings';
    await act(async () => { root!.render(<WalletShell view={state} status="" emit={emit}/>); });
    await key(window); expect(emit).not.toHaveBeenCalled();
  });
  it('inherits private appearance and submits masked fields once, clearing before reply', async () => {
    const prompt: Prompt = { id, kind: 'input', title: 'Unlock local wallet', labels: ['App password'], appearance: { theme: 'dark', fontSize: 24 } };
    const Reply = vi.fn(async (reply: Reply) => {
      expect(host.querySelector('input')).toBeNull();
      expect(reply).toEqual({ promptId: id, accepted: true, values: ['SYNTHETIC-PASSWORD'] });
      return true;
    });
    window.go = { webui: { PrivateBridge: { Prompt: async () => prompt, Reply, Copy: vi.fn(async () => false), Activity: vi.fn(async () => true) } } };
    await mount(<PrivateApp/>);
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.fontSize).toBe('24');
    const input = host.querySelector('input')!; expect(input.type).toBe('password'); input.value = 'SYNTHETIC-PASSWORD';
    await key(input, { isComposing: true }); await key(input, { repeat: true });
    expect(Reply).not.toHaveBeenCalled();
    await key(input); await key(window);
    expect(Reply).toHaveBeenCalledTimes(1);
    expect(host.textContent).not.toContain('SYNTHETIC-PASSWORD');
  });
});
