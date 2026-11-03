// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { PublicNode } from './PublicApp';
import { node } from './test-fixtures';

let root: Root, host: HTMLDivElement;
beforeEach(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }); host = document.createElement('div'); document.body.append(host); root = createRoot(host); });
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
function balances(funding = '3.25', unified = '5') {
  const table = node('column', 'freeStablecoinBalances');
  table.Children = [['Asset', 'Funding available', 'Unified available'], ['USDT', funding, unified], ['USDC', 'not yet verified', '0']].map(cells => {
    const row = node('row'); row.Children = cells.map(text => node('label', '', text)); return row;
  });
  return table;
}

it('renders aligned stablecoin pools with column and asset row headers and no economic controls', async () => {
  const emit = vi.fn(); await act(async () => root.render(<PublicNode node={balances()} emit={emit}/>));
  const table = host.querySelector('table[aria-label="Bybit available stablecoin funds"]')!;
  expect([...table.querySelectorAll('thead th')].map(cell => cell.textContent)).toEqual(['Asset', 'Funding available', 'Unified available']);
  expect([...table.querySelectorAll('tbody th[scope="row"]')].map(cell => cell.textContent)).toEqual(['USDT', 'USDC']);
  expect([...table.querySelectorAll('tbody td')].map(cell => cell.textContent)).toEqual(['3.25', '5', 'not yet verified', '0']);
  expect(table.querySelector('button, input, select')).toBeNull(); expect(emit).not.toHaveBeenCalled();
});

it('replaces a previous numeric observation with unavailable without fabricating zero', async () => {
  const emit = vi.fn(); await act(async () => root.render(<PublicNode node={balances()} emit={emit}/>));
  await act(async () => root.render(<PublicNode node={balances('not yet verified', 'not yet verified')} emit={emit}/>));
  expect([...host.querySelectorAll('tbody td')].map(cell => cell.textContent)).toEqual(['not yet verified', 'not yet verified', 'not yet verified', '0']);
  expect(host.textContent).not.toContain('3.25'); expect(emit).not.toHaveBeenCalled();
});

it('keeps unexpected nodes in the generic renderer rather than silently hiding a control', async () => {
  const malformed = balances(), emit = vi.fn(); malformed.Children![1].Children![1] = node('button', 'freeRefreshFunding', 'Refresh balances');
  await act(async () => root.render(<PublicNode node={malformed} emit={emit}/>));
  expect(host.querySelector('table')).toBeNull(); const button = host.querySelector('button')!;
  await act(async () => button.click()); expect(emit.mock.calls).toEqual([['freeRefreshFunding']]);
});
