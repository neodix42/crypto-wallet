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
  table.Children = [['Date and time (UTC)', 'Asset / network', 'Amount', 'Fee', 'Destination', 'Status', 'Request ID', 'Transaction ID'], ['2026-11-04 05:00:00', 'USDT · TON', '3', '0.15', 'UQ-public-fixture', 'rejected', 'Local public-operation', '—']].map(cells => {
    const row = node('row'); row.Children = cells.map(text => node('label', '', text)); return row;
  });
  table.Children[1].Children![2].Tooltip = 'plannedDebit';
  table.Children[1].Children![3].Tooltip = 'estimated';
  table.Children[1].Children![5].Tooltip = 'Source: bybit+local\nRaw status: HighValueReviewRejectedRefunded\nState: REJECTED\nReason: New address withdrawal lock\nCode: EXCHANGE_BYBIT_131098';
  return table;
}

it('shows one status word and discloses complete details only through an inert information icon', async () => {
  const emit = vi.fn(); await act(async () => root.render(<PublicNode node={requests()} emit={emit}/>));
  const table = host.querySelector('table[aria-label="Recent Bybit withdrawal requests"]')!;
  expect(table.querySelectorAll('thead th[scope="col"]')).toHaveLength(8);
  const status = table.querySelectorAll('td')[5];
  expect(status.textContent).toBe('rejected'); expect(status.getAttribute('title')).toBeNull();
  expect(table.textContent).not.toContain('New address withdrawal lock');
  expect(table.textContent).not.toContain('EXCHANGE_BYBIT_131098');
  expect(table.querySelector('tbody')!.textContent).toContain('Local public-operation');
  expect(table.querySelectorAll('td')[2].getAttribute('title')).toBe('Planned total debit, including the estimated fee.');
  expect(table.querySelectorAll('td')[3].getAttribute('title')).toBe('Estimated at approval.');
  expect(table.querySelectorAll('button')).toHaveLength(1); expect(table.querySelector('input, select, a, [data-action]')).toBeNull();
  const information = status.querySelector('button')!;
  expect(information.getAttribute('aria-label')).toBe('Withdrawal status information'); expect(information.getAttribute('aria-expanded')).toBe('false');
  await act(async () => information.click());
  const note = status.querySelector('[role="note"]')!;
  expect(note.textContent).toBe(requests().Children![1].Children![5].Tooltip);
  expect(information.getAttribute('aria-controls')).toBe(note.id); expect(information.getAttribute('aria-describedby')).toBe(note.id);
  expect(note.getAttribute('tabindex')).toBe('0'); expect(status.querySelector('.withdrawal-status-word')!.textContent).toBe('rejected');
  await act(async () => table.dispatchEvent(new MouseEvent('click', { bubbles: true })));
  expect(emit).not.toHaveBeenCalled();
});

it.each(['pending', 'success', 'rejected', 'failed', 'cancelled', 'unknown', 'requested'])('keeps the %s status as a lowercase word without provider detail text', async status => {
  const table = requests(); table.Children![1].Children![5].Text = status;
  await act(async () => root.render(<PublicNode node={table} emit={vi.fn()}/>));
  expect(host.querySelector('tbody td:nth-child(6)')!.textContent).toBe(status);
});

it('escapes complete provider-facing details and replaces an older state instead of retaining success', async () => {
  const table = requests(), emit = vi.fn(); table.Children![1].Children![5].Text = 'success';
  await act(async () => root.render(<PublicNode node={table} emit={emit}/>));
  await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Withdrawal status information"]')!.click());
  table.Children![1].Children![5].Text = 'unknown';
  table.Children![1].Children![5].Tooltip = 'Raw status: <script>public-canary</script>\nReason: ' + 'Complete public reason. '.repeat(100);
  await act(async () => root.render(<PublicNode node={table} emit={emit}/>));
  expect(host.querySelector('[role="note"]')).toBeNull(); expect(host.querySelector('tbody td:nth-child(6)')!.textContent).toBe('unknown');
  await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Withdrawal status information"]')!.click());
  expect(host.querySelector('[role="note"]')!.textContent).toBe(table.Children![1].Children![5].Tooltip);
  expect(host.textContent).not.toContain('success'); expect(host.querySelector('script')).toBeNull(); expect(emit).not.toHaveBeenCalled();
});

it('shows details on hover and keyboard focus and supports click, Escape and outside dismissal without emitting events', async () => {
  const emit = vi.fn(); await act(async () => root.render(<PublicNode node={requests()} emit={emit}/>));
  const container = host.querySelector('.withdrawal-status-information')!, information = container.querySelector('button')!;
  await act(async () => container.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
  expect(host.querySelector('[role="note"]')).not.toBeNull();
  await act(async () => container.dispatchEvent(new MouseEvent('mouseout', { bubbles: true, relatedTarget: document.body })));
  expect(host.querySelector('[role="note"]')).toBeNull();
  await act(async () => information.focus()); expect(host.querySelector('[role="note"]')).not.toBeNull();
  await act(async () => host.querySelector<HTMLElement>('[role="note"]')!.focus());
  await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
  expect(host.querySelector('[role="note"]')).toBeNull(); expect(document.activeElement).toBe(information);
  await act(async () => information.click()); expect(host.querySelector('[role="note"]')).not.toBeNull();
  await act(async () => information.click()); expect(host.querySelector('[role="note"]')).toBeNull();
  await act(async () => information.click());
  await act(async () => document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })));
  expect(host.querySelector('[role="note"]')).toBeNull(); expect(emit).not.toHaveBeenCalled();
});

it('keeps complete status information inside a small viewport and closes it on resize', async () => {
  vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(500); vi.spyOn(window, 'innerHeight', 'get').mockReturnValue(600);
  await act(async () => root.render(<PublicNode node={requests()} emit={vi.fn()}/>));
  vi.spyOn(host.querySelector('.withdrawal-status-information')!, 'getBoundingClientRect').mockReturnValue({ x: 420, y: 540, left: 420, right: 448, top: 540, bottom: 568, width: 28, height: 28, toJSON() {} });
  await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Withdrawal status information"]')!.click());
  const note = host.querySelector<HTMLElement>('[role="note"]')!;
  expect(note.style.left).toBe('16px'); expect(note.style.top).toBe('540px'); expect(note.style.maxHeight).toBe('360px'); expect(note.style.transform).toBe('translateY(-100%)');
  await act(async () => window.dispatchEvent(new Event('resize'))); expect(host.querySelector('[role="note"]')).toBeNull(); vi.restoreAllMocks();
});

it('keeps an unexpected status string in information while visibly showing unknown', async () => {
  const table = requests(); table.Children![1].Children![5].Text = 'success unverified provider text';
  await act(async () => root.render(<PublicNode node={table} emit={vi.fn()}/>));
  expect(host.querySelector('tbody td:nth-child(6)')!.textContent).toBe('unknown');
  await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Withdrawal status information"]')!.click());
  expect(host.querySelector('[role="note"]')!.textContent).toContain('Raw display status: success unverified provider text');
});

it('does not silently drop an unexpected interactive node under a table identifier', async () => {
  const table = requests(), emit = vi.fn(); table.Children![1].Children![7] = node('button', 'freeRefreshWithdrawals', 'Refresh withdrawals');
  await act(async () => root.render(<PublicNode node={table} emit={emit}/>));
  expect(host.querySelector('table')).toBeNull();
  await act(async () => host.querySelector('button')!.click()); expect(emit.mock.calls).toEqual([['freeRefreshWithdrawals']]);
});
