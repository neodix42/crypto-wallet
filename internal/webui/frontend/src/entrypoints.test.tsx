// @vitest-environment happy-dom
// Observe the real entry modules mounting the real components. Only the native
// bridge is a synthetic fixture. The createRoot wrapper observes real roots so
// teardown can unmount them; it delegates every call to React unchanged.
import { act } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { id, view } from './test-fixtures';

const roots = vi.hoisted(() => [] as import('react-dom/client').Root[]);
vi.mock('react-dom/client', async importOriginal => {
  const actual = await importOriginal<typeof import('react-dom/client')>();
  return { ...actual, createRoot: (...args: Parameters<typeof actual.createRoot>) => {
    const root = actual.createRoot(...args); roots.push(root); return root;
  } };
});
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  document.body.innerHTML = '<div id="root"></div>';
  window.runtime = { EventsOn: () => () => {} };
});
afterEach(async () => {
  await act(async () => { for (const root of roots.splice(0)) root.unmount(); });
  vi.restoreAllMocks(); delete window.go; delete window.runtime;
});
it('mounts the wallet and its normal dialog through the single shipped entrypoint', async () => {
  window.go = { webui: {
    PublicBridge: { Snapshot: async () => view(), Event: async () => true },
    DialogBridge: { Snapshot: async () => ({ id, kind: 'input', title: 'Unlock local wallet', labels: ['App password'] }), Reply: async () => true, Copy: async () => true, Activity: async () => true },
  } };
  await act(async () => { await import('./main'); });
  expect(roots).toHaveLength(1);
  expect(document.querySelector('.wallet-shell')).not.toBeNull();
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  expect(document.querySelector('input[type="password"]')).not.toBeNull();
});
