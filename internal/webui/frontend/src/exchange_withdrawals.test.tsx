// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { PublicNode } from './PublicApp';
import { node } from './test-fixtures';

let root: Root, host: HTMLDivElement;
beforeEach(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }); host = document.createElement('div'); document.body.append(host); root = createRoot(host); });
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
function requests() {
  const table = node('column', 'freeWithdrawalRequests');
  table.Children = [['Date and time (UTC)', 'Asset / network', 'Amount', 'Fee', 'Destination', 'Status', 'Request ID', 'Transaction ID'], ['2026-11-04 05:00:00', 'USDT · TON', '3', '0.15', 'UQ-public-fixture', 'Rejected\nNew address withdrawal lock\nEXCHANGE_BYBIT_131098', 'Local public-operation', '—']].map(cells => {
    const row = node('row'); row.Children = cells.map(text => node('label', '', text)); return row;
  });
  table.Children[1].Children![2].Tooltip = 'plannedDebit';
  table.Children[1].Children![3].Tooltip = 'estimated';
  return table;
}

it('shows the exact withdrawal status, reason, request identity and amount basis without authorization controls', async () => {
  const emit = vi.fn(); await act(async () => root.render(<PublicNode node={requests()} emit={emit}/>));
  const table = host.querySelector('table[aria-label="Recent Bybit withdrawal requests"]')!;
  expect(table.querySelectorAll('thead th[scope="col"]')).toHaveLength(8);
  expect(table.querySelector('tbody')!.textContent).toContain('Rejected\nNew address withdrawal lock');
  expect(table.querySelector('tbody')!.textContent).toContain('EXCHANGE_BYBIT_131098');
  expect(table.querySelector('tbody')!.textContent).toContain('Local public-operation');
  expect(table.querySelectorAll('td')[2].getAttribute('title')).toBe('Planned total debit, including the estimated fee.');
  expect(table.querySelectorAll('td')[3].getAttribute('title')).toBe('Estimated at approval.');
  expect(table.querySelector('button, input, select, a')).toBeNull();
  await act(async () => table.dispatchEvent(new MouseEvent('click', { bubbles: true })));
  expect(emit).not.toHaveBeenCalled();
});

it('escapes provider-facing public text and replaces an older state instead of retaining success', async () => {
  const table = requests(), emit = vi.fn(); table.Children![1].Children![5].Text = 'Pending';
  await act(async () => root.render(<PublicNode node={table} emit={emit}/>));
  table.Children![1].Children![5].Text = 'Rejected <script>public-canary</script>';
  await act(async () => root.render(<PublicNode node={table} emit={emit}/>));
  expect(host.textContent).toContain('Rejected <script>public-canary</script>');
  expect(host.textContent).not.toContain('Pending'); expect(host.querySelector('script')).toBeNull(); expect(emit).not.toHaveBeenCalled();
});

it('does not silently drop an unexpected interactive node under a table identifier', async () => {
  const table = requests(), emit = vi.fn(); table.Children![1].Children![7] = node('button', 'freeRefreshWithdrawals', 'Refresh withdrawals');
  await act(async () => root.render(<PublicNode node={table} emit={emit}/>));
  expect(host.querySelector('table')).toBeNull();
  await act(async () => host.querySelector('button')!.click()); expect(emit.mock.calls).toEqual([['freeRefreshWithdrawals']]);
});
