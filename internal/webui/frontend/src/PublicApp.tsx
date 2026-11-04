import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent } from 'react';
import type { ErrorNotice, Node, Page, View, WalletEvent } from './types';
import { applyAppearance, inputWithinLimit, isAssetTable, isWalletRow, validView } from './protocol';
import { DialogHost } from './Dialog';
import { appendErrorNotice } from './error_notices';
import { ExplorerLink, TransactionTable } from './TransactionTable';
import { AddressBookIcon, CopyIcon, ExchangeIcon, InfoIcon, LockIcon, Mark, PlusIcon, RefreshIcon, SaveAddressIcon, SettingsIcon } from './icons';

type Emit = (id: string, value?: string) => void;
const publicFieldLimit = 4096;
const inputRejectedEvent = 'wallet:input-rejected';
const inputRejectedMessage = 'Input is too long or contains unsupported characters. The previous value was kept.';
const blank: View = { Title: 'CryptoWallet', Status: '', StatusRevision: 0, Active: '', Pages: [] };

function HistoryInformation({ node }: { node: Node }) {
  const [open, setOpen] = useState(false), container = useRef<HTMLSpanElement>(null);
  const [position, setPosition] = useState({ left: 16, top: 16, maxHeight: 440 });
  const informationID = 'history-information-' + encodeURIComponent(node.ID);
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => { if (event.target instanceof window.Node && !container.current?.contains(event.target)) setOpen(false); };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
    const resize = () => setOpen(false);
    document.addEventListener('pointerdown', outside); document.addEventListener('keydown', escape); window.addEventListener('resize', resize);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape); window.removeEventListener('resize', resize); };
  }, [open]);
  const toggle = () => {
    if (!open) {
      const bounds = container.current?.getBoundingClientRect();
      if (bounds) {
        const width = Math.min(600, Math.max(0, window.innerWidth - 32)), height = Math.min(440, window.innerHeight * .6);
        const top = window.innerHeight - bounds.bottom >= 176 ? bounds.bottom + 8 : Math.max(16, bounds.top - height - 8);
        setPosition({ left: Math.max(16, Math.min(bounds.left, window.innerWidth - width - 16)), top, maxHeight: Math.max(0, Math.min(height, window.innerHeight - top - 16)) });
      }
    }
    setOpen(value => !value);
  };
  return <span className="history-information" ref={container}><button type="button" className="icon-button" title="History information" aria-label="History information" aria-expanded={open} aria-controls={informationID} onClick={toggle}><InfoIcon/></button>{open && <span className="history-information-popover" style={position} role="note" tabIndex={0} aria-label="Transaction history information" id={informationID}>{node.Tooltip}</span>}</span>;
}

function AssetSelector({ node, tooltip, emit }: { node: Node; tooltip: string; emit: Emit }) {
  const [hovered, setHovered] = useState(false), [focused, setFocused] = useState(false), [dismissed, setDismissed] = useState(false);
  const visible = (hovered || focused) && !dismissed;
  const tooltipID = 'asset-identity-' + encodeURIComponent(node.ID);
  return <span className="asset-selector" tabIndex={node.Disabled ? 0 : undefined} onMouseEnter={() => { setHovered(true); setDismissed(false); }} onMouseLeave={() => setHovered(false)} onFocus={() => { setFocused(true); setDismissed(false); }} onBlur={() => setFocused(false)} onKeyDown={event => { if (event.key === 'Escape') setDismissed(true); }} aria-describedby={visible && tooltip ? tooltipID : undefined}><button type="button" data-action={node.ID} disabled={node.Disabled} aria-describedby={visible && tooltip ? tooltipID : undefined} onClick={() => emit(node.ID)}>{node.Text}</button>{visible && tooltip && <span className="asset-tooltip" role="tooltip" id={tooltipID}>{tooltip}</span>}</span>;
}

export function AssetTable({ node, emit }: { node: Node; emit: Emit }) {
  return <div className="asset-table-wrap"><table className="asset-table" aria-label={node.Text || 'Wallet assets'}><thead><tr><th scope="col">Token</th><th scope="col">Balance</th><th scope="col">Decimals</th></tr></thead><tbody>{(node.Children || []).map(row => {
    const [selection, balance, decimals] = row.Children!;
    return <tr key={row.ID}><td><AssetSelector node={selection} tooltip={row.Tooltip || selection.Tooltip} emit={emit}/></td><td><PublicNode node={balance} emit={emit}/></td><td><PublicNode node={decimals} emit={emit}/></td></tr>;
  })}</tbody></table></div>;
}

function isStablecoinBalanceTable(node: Node): boolean {
  const rows = node.Children || [];
  return node.ID === 'freeStablecoinBalances' && rows.length === 3 && rows.every(row => row.Kind === 'row' && row.Children?.length === 3 && row.Children.every(cell => cell.Kind === 'label'));
}

function StablecoinBalances({ node }: { node: Node }) {
  const [head, ...rows] = node.Children!;
  return <div className="exchange-balances-wrap"><table className="exchange-balances" aria-label="Bybit available stablecoin funds"><thead><tr>{head.Children!.map((cell, index) => <th key={index} scope="col">{cell.Text}</th>)}</tr></thead><tbody>{rows.map((row, index) => <tr key={row.ID || index}>{row.Children!.map((cell, column) => column === 0 ? <th key={column} scope="row">{cell.Text}</th> : <td key={column} title={cell.Tooltip}>{cell.Text}</td>)}</tr>)}</tbody></table></div>;
}

function isWithdrawalRequestsTable(node: Node): boolean {
  const rows = node.Children || [];
  return node.ID === 'freeWithdrawalRequests' && rows.length >= 1 && rows.length <= 11 && rows.every(row => row.Kind === 'row' && row.Children?.length === 8 && row.Children.every(cell => cell.Kind === 'label'));
}

function WithdrawalRequests({ node }: { node: Node }) {
  const [head, ...rows] = node.Children!;
  const hint = (value: string) => ({ plannedDebit: 'Planned total debit, including the estimated fee.', estimated: 'Estimated at approval.', providerReported: 'Reported by Bybit; this does not independently establish receipt.' }[value] || value);
  return <div className="withdrawal-table-wrap"><table className="withdrawal-table" aria-label="Recent Bybit withdrawal requests"><thead><tr>{head.Children!.map((cell, index) => <th key={index} scope="col">{cell.Text}</th>)}</tr></thead><tbody>{rows.map((row, index) => <tr key={index} title={row.Tooltip}>{row.Children!.map((cell, column) => <td key={column} title={hint(cell.Tooltip)}>{cell.Text}</td>)}</tr>)}</tbody></table></div>;
}

function fieldErrorID(node: Node): string { return 'wallet-' + node.ID + '-error'; }

export function Field({ node, emit, hideInlineError = false }: { node: Node; emit: Emit; hideInlineError?: boolean }) {
  const [value, setValue] = useState(node.Value);
  const previous = useRef({ id: node.ID, value: node.Value });
  const pending = useRef<string[]>([]), input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (previous.current.id !== node.ID || node.Disabled) {
      pending.current = []; setValue(node.Value);
    } else if (previous.current.value !== node.Value) {
      const acknowledged = pending.current.indexOf(node.Value);
      if (node.Kind === 'entry' && input.current === document.activeElement && acknowledged >= 0) {
        pending.current.splice(0, acknowledged + 1);
        if (!pending.current.length) setValue(node.Value);
      } else {
        pending.current = []; setValue(node.Value);
      }
    }
    previous.current = { id: node.ID, value: node.Value };
  }, [node.ID, node.Kind, node.Value, node.Disabled]);
  const update = (v: string) => {
    // Apply the same UTF-8/NUL bound as Go before changing the controlled
    // draft or its pending echoes. A rejected edit must never look newer
    // than the value the coordinator will use for a later action.
    if (!inputWithinLimit(v)) {
      if (input.current) input.current.value = value;
      window.dispatchEvent(new Event(inputRejectedEvent));
      return;
    }
    if (node.Kind === 'entry') {
      // Public draft values only. Bound echoes retained while a field is
      // focused; unknown Go values and explicit busy/reset views still win.
      pending.current.push(v);
      if (pending.current.length > publicFieldLimit) pending.current.shift();
    }
    setValue(v); emit(node.ID, v);
  };
  const id = 'wallet-' + node.ID, errorID = fieldErrorID(node);
  if (node.Kind === 'check') return <label className="check-field" title={node.Tooltip}><input type="checkbox" checked={value === 'true'} disabled={node.Disabled} onChange={event => update(String(event.target.checked))}/>{node.Text}</label>;
  return <label className="field" htmlFor={id} title={node.Tooltip}><span>{node.Text}</span>{node.Kind === 'combo'
    ? <select id={id} value={value} disabled={node.Disabled} onChange={event => update(event.target.value)}>{!(node.Options || []).some(option => option.ID === value) && <option value="" disabled>Select…</option>}{(node.Options || []).map(option => <option key={option.ID} value={option.ID}>{option.Text}</option>)}</select>
    : <input ref={input} id={id} value={value} disabled={node.Disabled} autoComplete="off" spellCheck={false} inputMode={node.InputMode || undefined} pattern={node.InputMode === 'decimal' ? '[0-9]+(\\.[0-9]+)?' : undefined} aria-invalid={node.Error ? true : undefined} aria-describedby={node.Error ? errorID : undefined} maxLength={publicFieldLimit} onBlur={() => { pending.current = []; }} onChange={event => update(event.target.value)}/>}{node.Error && !hideInlineError && <span className="field-error" id={errorID} role="alert">{node.Error}</span>}</label>;
}

function ExchangeSteps({ node, emit }: { node: Node; emit: Emit }) {
  const current = Number(node.Value);
  return <ol className="exchange-steps" aria-label="Exchange steps">{(node.Children || []).map((step, index) => {
    const number = index + 1, selected = number === current;
    // Steps are navigation. An incorrectly authored child must never turn
    // this control into a deposit, order or withdrawal action.
    const interactive = step.Kind === 'button' && number <= 5 && step.ID === `wizardStep/${number}`;
    const content = <><span className="step-number" aria-hidden="true">{!interactive && number < current ? '✓' : number}</span><span>{step.Text}</span></>;
    return <li key={index} className={[selected ? 'current' : !interactive && number < current ? 'complete' : '', interactive ? 'interactive' : ''].filter(Boolean).join(' ')} aria-current={!interactive && selected ? 'step' : undefined}>{interactive
      ? <button type="button" className="exchange-step-button" disabled={step.Disabled} data-action={step.ID} aria-current={selected ? 'step' : undefined} title={step.Tooltip} onClick={() => emit(step.ID)}>{content}</button>
      : content}</li>;
  })}</ol>;
}

export function PublicNode({ node, emit }: { node: Node; emit: Emit }) {
  const children = () => (node.Children || []).map((child, index) => <PublicNode key={child.ID || `${child.Kind}-${index}`} node={child} emit={emit}/>);
  switch (node.Kind) {
    case 'heading': return <h2 className="node-heading" title={node.Tooltip}>{node.Text}</h2>;
    case 'label': if (node.ID.startsWith('historyInfo/')) return <HistoryInformation node={node}/>; if (node.ID === 'portfolioValuation') return <span className="portfolio-valuation" title={node.Tooltip} role="status">{node.Value === 'loading' && <span className="price-spinner" aria-label="Fetching USDT value"/>}{node.Text}</span>; return node.Text ? <p className="node-label" title={node.Tooltip}>{node.Text}</p> : null;
    case 'separator': return <hr/>;
    case 'transactionTable': return <TransactionTable node={node}/>;
    case 'explorer': return <ExplorerLink node={node}/>;
    case 'column': return node.ID.startsWith('bookComposer/') ? <>{children()}</> : isAssetTable(node) ? <AssetTable node={node} emit={emit}/> : isStablecoinBalanceTable(node) ? <StablecoinBalances node={node}/> : isWithdrawalRequestsTable(node) ? <WithdrawalRequests node={node}/> : <div className="node-column">{children()}</div>;
    case 'row': {
      if (node.ID === 'exchangeSteps') return <ExchangeSteps node={node} emit={emit}/>;
      if (node.ID.startsWith('walletSections/') || node.ID === 'settingsSections') return <nav className="wallet-sections" role="tablist" aria-label={node.Text}>{(node.Children || []).map(child => <button key={child.ID} type="button" role="tab" aria-selected={child.Text === node.Value} className={child.Text === node.Value ? 'selected' : ''} disabled={child.Disabled} data-action={child.ID} onClick={() => emit(child.ID)}>{child.Text}</button>)}</nav>;
      if (node.ID.startsWith('sendAmount/')) {
        const [amount, maximum] = node.Children || [], owner = node.ID.slice('sendAmount/'.length);
        if ((node.Children || []).length === 2 && owner && amount.Kind === 'entry' && amount.ID === 'field/amount/' + owner && maximum.Kind === 'button' && maximum.ID === 'max/' + owner) {
          return <div className="send-amount-field"><Field node={amount} emit={emit} hideInlineError/><PublicNode node={maximum} emit={emit}/>{amount.Error && <span className="field-error send-amount-error" id={fieldErrorID(amount)} role="alert">{amount.Error}</span>}</div>;
        }
        return <div className="node-row" title={node.Tooltip}>{children()}</div>;
      }
      if (node.ID.startsWith('recipientField/')) {
        const [recipient, load, save] = node.Children || [], owner = node.ID.slice('recipientField/'.length);
        if ((node.Children || []).length === 3 && owner && recipient.Kind === 'entry' && recipient.ID === 'field/recipient/' + owner && load.Kind === 'button' && load.ID === 'addressBookLoad/' + owner && save.Kind === 'button' && save.ID === 'addressBookSave/' + owner) {
          return <div className="recipient-field"><Field node={recipient} emit={emit} hideInlineError/><PublicNode node={load} emit={emit}/><PublicNode node={save} emit={emit}/>{recipient.Error && <span className="field-error recipient-error" id={fieldErrorID(recipient)} role="alert">{recipient.Error}</span>}</div>;
        }
        return <div className="node-row" title={node.Tooltip}>{children()}</div>;
      }
      if (node.ID.startsWith('walletActions/')) return <div className="wallet-actions">{children()}</div>;
      if (node.ID.startsWith('historyToolbar/')) return <div className="history-toolbar">{children()}</div>;
      const entryErrors = (node.Children || []).filter(child => child.Kind === 'entry' && child.Error);
      const rowClass = (node.Children || []).some(child => child.Kind === 'entry') && (node.Children || []).some(child => child.Kind === 'button') ? 'node-row entry-action-row' : 'node-row';
      if (entryErrors.length) return <div className={rowClass} title={node.Tooltip}>{(node.Children || []).map((child, index) => child.Kind === 'entry' ? <Field key={child.ID || index} node={child} emit={emit} hideInlineError/> : <PublicNode key={child.ID || index} node={child} emit={emit}/>)}{entryErrors.map(field => <span className="field-error node-row-error" key={field.ID} id={fieldErrorID(field)} role="alert">{field.Error}</span>)}</div>;
      return <div className={rowClass} title={node.Tooltip}>{children()}</div>;
    }
    case 'address': return <div className={`address-field ${/^(Receive address|Owned wallet receive address)$/.test(node.Text) ? 'receive-address-line' : ''}`} title={node.Tooltip}>{node.Text && <span className="address-label">{node.Text}{/^(Receive address|Owned wallet receive address)$/.test(node.Text) ? ':' : ''}</span>}<code className="address">{node.Value}</code></div>;
    case 'entry': case 'combo': case 'check': return <Field node={node} emit={emit}/>;
    case 'button': {
      if (node.ID === 'addNextAccount' || /^(?:renameWallet|deleteWallet|renameGroup|deleteGroup)\//.test(node.ID)) return null;
      const copy = node.ID.startsWith('copyReceive/');
      const refreshHistory = node.ID.startsWith('walletHistory/');
      const refreshState = node.ID === 'refresh', lock = node.ID === 'lock';
      const icon = copy ? 'Copy address' : refreshHistory ? 'Refresh history' : refreshState ? 'Refresh wallet state' : lock ? 'Lock wallet' : '';
      const loadAddress = node.ID.startsWith('addressBookLoad/'), saveAddress = node.ID.startsWith('addressBookSave/');
      const primary = /^(create12|unlock|confirmRecovery|send\/|prepare\/|stakingSend\/|exchangeConnect|exchangeSwap|wizardDeposit$|wizardMove$|wizardSwap$|wizardWithdraw$)/.test(node.ID);
      return <button type="button" autoFocus={node.ID === 'unlock'} data-action={node.ID} className={`${copy ? 'copy-button icon-button' : icon ? 'icon-button' : ''} ${primary ? 'primary' : ''} ${loadAddress || saveAddress ? 'addressbook-action' : ''}`} disabled={node.Disabled} title={icon || node.Tooltip || (loadAddress || saveAddress ? node.Text : undefined)} aria-label={icon || (loadAddress || saveAddress ? node.Text : undefined)} onClick={() => copy ? emit(node.ID, node.Value) : emit(node.ID)}>{copy ? <CopyIcon/> : refreshHistory || refreshState ? <RefreshIcon/> : lock ? <LockIcon/> : loadAddress ? <AddressBookIcon/> : saveAddress ? <SaveAddressIcon/> : node.Text}</button>;
    }
  }
}

function findPublicNode(nodes: Node[] | null | undefined, id: string): Node | undefined {
  for (const node of nodes || []) {
    if (node.ID === id) return node;
    const nested = findPublicNode(node.Children, id);
    if (nested) return nested;
  }
}

function portfolioBody(nodes: Node[]): Node[] {
  return nodes.flatMap(node => {
    if (['field/group', 'group', 'refresh', 'lock', 'networkCount'].includes(node.ID)) return [];
    if (node.Children && (node.Kind === 'row' || node.Kind === 'column')) {
      const children = portfolioBody(node.Children);
      return children.length ? [{ ...node, Children: children }] : [];
    }
    return [node];
  });
}

function Portfolio({ page, emit }: { page: Page; emit: Emit }) {
  const original = page.Nodes || [], refresh = findPublicNode(original, 'refresh');
  const nodes = portfolioBody(original), rows = nodes.filter(isWalletRow);
  const headingRow = (node: Node) => node.Kind === 'row' && node.Children?.[0]?.Kind === 'heading' && node.Children[0].Text === 'Network';
  const selectingRow = (row: HTMLTableRowElement) => {
    const selection = window.getSelection();
    return !!selection && !selection.isCollapsed && (row.contains(selection.anchorNode) || row.contains(selection.focusNode));
  };
  const first = nodes.findIndex(isWalletRow), last = nodes.reduce((found, node, index) => isWalletRow(node) ? index : found, -1);
  return <><div className="page-heading"><div><p className="eyebrow">YOUR WALLETS</p><h1>{page.Title}</h1>{nodes.filter(node => node.ID === 'portfolioValuation').map(node => <PublicNode key={node.ID} node={node} emit={emit}/>)}</div></div><div className="portfolio-toolbar">{nodes.slice(0, first < 0 ? nodes.length : first).filter(node => !headingRow(node) && node.ID !== 'portfolioValuation').map((node, index) => <PublicNode key={node.ID || index} node={node} emit={emit}/>)}</div><div className="wallet-table-wrap"><table className="wallet-table"><thead><tr><th>Network</th><th>Native balance</th><th>Receive address</th><th className="wallet-refresh-cell">{refresh ? <PublicNode node={refresh} emit={emit}/> : <span className="sr-only">Copy</span>}</th></tr></thead><tbody>{rows.map(row => <tr key={row.ID} tabIndex={0} onClick={event => { if (!(event.target as HTMLElement).closest('button,input,select,.address-field') && !selectingRow(event.currentTarget)) emit(row.ID); }} onKeyDown={event => { if (event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); emit(row.ID); } }}>{(row.Children || []).map((node, index) => <td key={node.ID || index}>{index === 0 ? <div className="network-name"><span className="network-avatar" aria-hidden="true">{node.Text.slice(0, 1)}</span><PublicNode node={node} emit={emit}/></div> : <PublicNode node={node} emit={emit}/>}</td>)}</tr>)}</tbody></table></div>{last >= 0 && nodes.slice(last + 1).map((node, index) => <PublicNode key={node.ID || index} node={node} emit={emit}/>)}</>;
}

export function PublicContent({ page, emit }: { page: Page; emit: Emit }) {
  const nodes = page.Nodes || [];
  const portfolio = page.ID.startsWith('group/') && (nodes.some(isWalletRow) || nodes.some(node => node.ID === 'networkCount'));
  if (portfolio) return <Portfolio page={page} emit={emit}/>;
  const onboarding = page.ID.startsWith('group/') && !portfolio;
  return <div className={onboarding ? 'welcome-card' : 'content-card'}>{onboarding && <div className="welcome-symbol"><Mark/></div>}{nodes.map((node, index) => <PublicNode key={node.ID || `${node.Kind}-${index}`} node={node} emit={emit}/>)}</div>;
}

function pageMenuActions(page: Page): Node[] {
  const kind = page.ID.startsWith('group/') ? 'Group' : page.ID.startsWith('wallet/') ? 'Wallet' : '';
  if (!kind) return [];
  const id = page.ID.slice(kind === 'Group' ? 6 : 7);
  return ['rename', 'delete'].flatMap(action => {
    const item = findPublicNode(page.Nodes, action + kind + '/' + id);
    return item?.Kind === 'button' ? [item] : [];
  });
}
function menuBinding(page: Page, view: View): string {
  return JSON.stringify([view.Active, view.AccountGroupID || '', page.ID, page.Title, page.ParentID || '', pageMenuActions(page).map(item => [item.ID, item.Text, item.Tooltip, item.Disabled])]);
}
type TabMenu = { pageID: string; binding: string; left: number; top: number };

export function WalletShell({ view, status, emit }: { view: View; status: string; emit: Emit }) {
  const sourcePages = view.Pages || [], utilityIDs = ['exchange', 'addressbook', 'settings'];
  const [exchangeOpen, setExchangeOpen] = useState(view.Active === 'exchange');
  const [menu, setMenu] = useState<TabMenu | null>(null), menuElement = useRef<HTMLDivElement>(null), menuOwner = useRef<HTMLButtonElement | null>(null);
  const menuPage = menu ? sourcePages.find(page => page.ID === menu.pageID && menuBinding(page, view) === menu.binding) : undefined;
  const menuActions = menuPage ? pageMenuActions(menuPage) : [];
  const menuValid = !!menu && !!menuPage && menuActions.length > 0;
  useEffect(() => {
    if (!menu) return;
    if (!menuValid) { setMenu(null); return; }
    const first = menuElement.current?.querySelector<HTMLButtonElement>('button:not(:disabled)');
    if (first) first.focus(); else menuElement.current?.focus();
    const outside = (event: PointerEvent) => { if (event.target instanceof window.Node && !menuElement.current?.contains(event.target)) setMenu(null); };
    const reposition = () => setMenu(null);
    document.addEventListener('pointerdown', outside); document.addEventListener('scroll', reposition, true); window.addEventListener('resize', reposition);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('scroll', reposition, true); window.removeEventListener('resize', reposition); };
  }, [menu, menuValid]);
  const openMenu = (page: Page, owner: HTMLButtonElement, left: number, top: number) => {
    if (!pageMenuActions(page).length) return false;
    menuOwner.current = owner;
    setMenu({ pageID: page.ID, binding: menuBinding(page, view), left: Math.max(12, Math.min(left, window.innerWidth - 212)), top: Math.max(12, Math.min(top, window.innerHeight - 112)) });
    return true;
  };
  const contextMenu = (event: ReactMouseEvent<HTMLButtonElement>, page: Page) => { if (openMenu(page, event.currentTarget, event.clientX, event.clientY)) event.preventDefault(); };
  const contextKey = (event: ReactKeyboardEvent<HTMLButtonElement>, page: Page) => {
    if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    if (openMenu(page, event.currentTarget, bounds.left, bounds.bottom + 4)) event.preventDefault();
  };
  const menuKey = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') { event.preventDefault(); setMenu(null); if (menuOwner.current?.isConnected) menuOwner.current.focus(); return; }
    if (event.key === 'Tab') { setMenu(null); return; }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const items = [...(menuElement.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])];
    if (!items.length) return;
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    const index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : event.key === 'ArrowUp' ? (current - 1 + items.length) % items.length : (current + 1) % items.length;
    items[index].focus();
  };
  const groups = sourcePages.filter(page => page.ID.startsWith('group/'));
  const utilities = utilityIDs.flatMap(id => sourcePages.filter(page => page.ID === id));
  const active = sourcePages.find(page => page.ID === view.Active) || groups[0] || utilities[0];
  const parentID = active?.ID.startsWith('group/') ? active.ID : active?.ParentID || view.AccountGroupID || groups[0]?.ID;
  const group = groups.find(page => page.ID === parentID) || groups[0];
  const exchangePage = sourcePages.find(page => page.ID === 'exchange');
  useEffect(() => {
    if (!exchangePage) setExchangeOpen(false);
    else if (active?.ID === 'exchange') setExchangeOpen(true);
  }, [!!exchangePage, active?.ID]);
  const closeExchange = () => {
    setExchangeOpen(false);
    if (active?.ID === 'exchange' && group) emit('tab', group.ID);
  };
  const wallets = sourcePages.filter(page => page.ID.startsWith('wallet/') && (page.ParentID || groups[0]?.ID) === parentID);
  const metadata = view.Title.split(' · ').slice(1).join(' · ');
  const networkCount = findPublicNode(group?.Nodes, 'networkCount');
  const countText = networkCount?.Text || (group?.Nodes?.some(isWalletRow) ? `${group.Nodes.filter(isWalletRow).length} networks` : '');
  const lock = findPublicNode(group?.Nodes, 'lock'), addAccount = findPublicNode(group?.Nodes, 'addNextAccount');
  const portfolio = active?.ID.startsWith('group/') && (active.Nodes?.some(isWalletRow) || active.Nodes?.some(node => node.ID === 'networkCount'));
  const unlock = active?.ID.startsWith('group/') && active.Nodes?.some(node => node.Kind === 'button' && node.ID === 'unlock' && !node.Disabled);
  useEffect(() => {
    if (!unlock) return;
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Enter' && !event.repeat && !event.ctrlKey && !event.metaKey && !event.altKey && !(event.target instanceof Element && event.target.closest('button,a,input,select,textarea'))) {
        event.preventDefault(); emit('unlock');
      }
    };
    window.addEventListener('keydown', keydown);
    return () => window.removeEventListener('keydown', keydown);
  }, [unlock, emit]);
  return <div className="wallet-shell"><header className="app-header"><div className="app-identity"><a className="brand" href="#" onClick={event => { event.preventDefault(); if (group) emit('tab', group.ID); }}><Mark/><span>CryptoWallet</span></a><span className="build-metadata">{metadata}</span></div><nav className="utility-navigation" role="tablist" aria-label="Application pages">{countText && <span className="network-count">{countText}</span>}{utilities.map(page => <span className="utility-item" key={page.ID}>{page.ID === 'settings' && lock && <PublicNode node={lock} emit={emit}/>}<button type="button" role="tab" className="icon-button" title={page.Title} aria-label={page.Title} aria-selected={active?.ID === page.ID} aria-controls="wallet-panel" onClick={() => { if (page.ID === 'exchange') setExchangeOpen(true); emit('tab', page.ID); }}>{page.ID === 'exchange' ? <ExchangeIcon/> : page.ID === 'addressbook' ? <AddressBookIcon/> : <SettingsIcon/>}</button></span>)}</nav></header><nav className="tabs account-tabs" role="tablist" aria-label="Account groups">{groups.map(page => <div className={`tab ${page.ID === parentID && active?.ID !== 'exchange' ? 'active' : ''}`} key={page.ID}><button type="button" role="tab" aria-selected={page.ID === parentID && active?.ID !== 'exchange'} aria-controls="wallet-panel" aria-haspopup={pageMenuActions(page).length ? 'menu' : undefined} onContextMenu={event => contextMenu(event, page)} onKeyDown={event => contextKey(event, page)} onClick={() => emit('tab', page.ID)}>{page.Title}</button></div>)}{exchangePage && exchangeOpen && <div className={`tab exchange-tab ${active?.ID === 'exchange' ? 'active' : ''}`} onAuxClick={event => { if (event.button === 1) { event.preventDefault(); closeExchange(); } }}><button type="button" role="tab" aria-selected={active?.ID === 'exchange'} aria-controls="wallet-panel" onClick={() => emit('tab', 'exchange')}>{exchangePage.Title}</button><button className="close-tab" type="button" aria-label="Close Exchange" onClick={closeExchange}>×</button></div>}{addAccount?.Kind === 'button' && <button type="button" className="account-add icon-button" data-action={addAccount.ID} title="Add deterministic account" aria-label="Add deterministic account" disabled={addAccount.Disabled} onClick={() => emit(addAccount.ID)}><PlusIcon/></button>}</nav>{wallets.length > 0 && active?.ID !== 'exchange' && <nav className="wallet-subtabs" role="tablist" aria-label={`${group?.Title || 'Account'} wallets`}>{wallets.map(page => <div className={`tab ${active?.ID === page.ID ? 'active' : ''}`} key={page.ID} onAuxClick={event => { if (event.button === 1) { event.preventDefault(); emit('closeTab', page.ID); } }}><button type="button" role="tab" aria-selected={active?.ID === page.ID} aria-controls="wallet-panel" aria-haspopup={pageMenuActions(page).length ? 'menu' : undefined} onContextMenu={event => contextMenu(event, page)} onKeyDown={event => contextKey(event, page)} onClick={() => emit('tab', page.ID)}>{page.Title}</button><button className="close-tab" type="button" aria-label={`Close ${page.Title}`} onClick={() => emit('closeTab', page.ID)}>×</button></div>)}</nav>}<main className={`workspace ${portfolio ? 'portfolio-workspace' : active?.ID.startsWith('group/') ? 'welcome-workspace' : ''}`} id="wallet-panel" role="tabpanel" aria-label={active?.Title}>{active ? <PublicContent page={active} emit={emit}/> : <div className="connecting"><Mark/><p>Opening your encrypted wallet…</p></div>}</main>{menuValid && menu && <div className="tab-context-menu" role="menu" tabIndex={-1} aria-label={`${menuPage!.Title} actions`} ref={menuElement} style={{ left: menu.left, top: menu.top }} onKeyDown={menuKey}>{menuActions.map(action => <button key={action.ID} type="button" role="menuitem" tabIndex={-1} disabled={action.Disabled} title={action.Tooltip || undefined} onClick={() => { const target = sourcePages.find(page => page.ID === menu.pageID && menuBinding(page, view) === menu.binding); const current = target && pageMenuActions(target).find(item => item.ID === action.ID && !item.Disabled); setMenu(null); if (current) emit(current.ID); }}>{action.ID.startsWith('rename') ? 'Rename' : 'Delete'}</button>)}</div>}<footer className="status-bar" aria-live="polite" aria-atomic="true"><span className={status ? 'status-dot visible' : 'status-dot'} aria-hidden="true"/><span>{status}</span></footer></div>;
}

export function PublicApp() {
  const [view, setView] = useState(blank), [status, setStatus] = useState('');
  const [copyFeedbackRevision, setCopyFeedbackRevision] = useState(0);
  const [localErrors, setLocalErrors] = useState<ErrorNotice[]>([]), nextError = useRef(0);
  const queue = useRef(Promise.resolve()), clipboardQueue = useRef(Promise.resolve()), live = useRef(true), currentView = useRef(blank), copyRequest = useRef(0);
  const copyTarget = useRef<{ ID: string; Value: string; page: string } | null>(null);
  const copyable = (ID: string, Value: string, state = currentView.current) => {
    const pages = state.Pages || [], page = pages.find(page => page.ID === state.Active) || pages[0];
    const matches = (nodes: Node[]): boolean => nodes.some(node => node.Kind === 'button' && node.ID === ID && !node.Disabled && node.Value === Value || matches(node.Children || []));
    return !!Value && matches(page?.Nodes || []);
  };
  const reportClientError = (message: string, operation = 'Wallet action', code = 'REQUEST_FAILED') => {
    const notice = { ID: 'client-' + ++nextError.current, Title: 'Operation failed', Operation: operation, Code: code, Message: message };
    setLocalErrors(queue => appendErrorNotice(queue, notice));
  };
  const copyFeedback = (message: string) => { setStatus(message); setCopyFeedbackRevision(revision => revision + 1); };
  const emit: Emit = (ID, Value = '') => {
    if (ID.startsWith('copyReceive/')) {
      const request = ++copyRequest.current, page = currentView.current.Active;
      if (!copyable(ID, Value)) { copyTarget.current = null; reportClientError('Could not copy address. Try again. The displayed address is no longer available as the current copy target.', 'Copy receiving address', 'COPY_TARGET_CHANGED'); return; }
      copyTarget.current = { ID, Value, page };
      copyFeedback('');
      const current = () => live.current && request === copyRequest.current && page === currentView.current.Active && copyable(ID, Value);
      const write = async () => {
        if (!current()) return;
        try {
          const copied = await window.runtime?.ClipboardSetText?.(Value);
          if (current()) {
            if (copied === true) copyFeedback('Address copied.');
            else reportClientError('Could not copy address. Try again. The system clipboard did not acknowledge a successful write.', 'Copy receiving address', 'CLIPBOARD_REJECTED');
          }
        } catch { if (current()) reportClientError('Could not copy address. Try again. The copy request failed before a successful clipboard acknowledgement.', 'Copy receiving address', 'CLIPBOARD_REQUEST_FAILED'); }
        finally { if (request === copyRequest.current) copyTarget.current = null; }
      };
      // Native runtime calls can be dispatched concurrently. Keep clipboard
      // writes ordered without waiting for any wallet operation or approval.
      clipboardQueue.current = clipboardQueue.current.then(write, write);
      return;
    }
    if (ID !== 'activity') { copyRequest.current++; copyTarget.current = null; }
    const bridge = window.go?.webui?.PublicBridge;
    if (!bridge) { if (ID !== 'activity') reportClientError('The wallet connection is unavailable. Close and reopen CryptoWallet.', 'Contact wallet coordinator', 'WALLET_CONNECTION_UNAVAILABLE'); return; }
    const event: WalletEvent = { ID, Value };
    queue.current = queue.current.then(async () => {
      if (ID === 'ackError' && currentView.current.ErrorNotice?.ID !== Value) return;
      if (await bridge.Event(event) !== true && ID !== 'activity' && !(ID === 'ackError' && currentView.current.ErrorNotice?.ID !== Value)) throw new Error('Unavailable');
    }).catch(() => { if (live.current && ID !== 'activity' && !(ID === 'ackError' && currentView.current.ErrorNotice?.ID !== Value)) reportClientError('Request failed. Check wallet state before retrying. The wallet rejected this action or became unavailable before completion.', ID === 'ackError' ? 'Acknowledge error' : 'Wallet action', 'WALLET_REQUEST_FAILED'); });
  };
  useEffect(() => {
    live.current = true; applyAppearance();
    let disposed = false, pushed = false;
    const accept = (value: unknown) => {
      if (disposed) return;
      if (validView(value)) {
        const target = copyTarget.current;
        if (target && (target.page !== value.Active || value.Status !== currentView.current.Status || value.StatusRevision !== currentView.current.StatusRevision || !copyable(target.ID, target.Value, value))) { copyRequest.current++; copyTarget.current = null; }
        currentView.current = value; applyAppearance(value.Appearance); setView(value); document.title = value.Title;
      } else { copyRequest.current++; copyTarget.current = null; reportClientError('The wallet returned an invalid view. Close and reopen CryptoWallet.', 'Read wallet state', 'INVALID_WALLET_VIEW'); }
    };
    const bridge = window.go?.webui?.PublicBridge;
    const unsubscribe = window.runtime?.EventsOn('wallet:view', value => { pushed = true; accept(value); });
    if (bridge) bridge.Snapshot().then(value => { if (!pushed) accept(value); }).catch(() => { if (!disposed) reportClientError('The wallet connection is unavailable. Close and reopen CryptoWallet.', 'Read wallet state', 'WALLET_CONNECTION_UNAVAILABLE'); });
    else reportClientError('The wallet connection is unavailable. Close and reopen CryptoWallet.', 'Read wallet state', 'WALLET_CONNECTION_UNAVAILABLE');
    let lastActivity = 0;
    const activity = () => { const now = Date.now(); if (now - lastActivity >= 1000) { lastActivity = now; emit('activity'); } };
    const rejectedInput = () => { if (!disposed) copyFeedback(inputRejectedMessage); };
    window.addEventListener('pointerdown', activity); window.addEventListener('keydown', activity); window.addEventListener(inputRejectedEvent, rejectedInput);
    return () => { disposed = true; live.current = false; copyRequest.current++; copyTarget.current = null; unsubscribe?.(); window.removeEventListener('pointerdown', activity); window.removeEventListener('keydown', activity); window.removeEventListener(inputRejectedEvent, rejectedInput); };
  }, []);
  useEffect(() => { setStatus(view.Status); }, [view.Status, view.StatusRevision]);
  useEffect(() => { if (!status) return; const timer = window.setTimeout(() => setStatus(''), 7000); return () => window.clearTimeout(timer); }, [status, view.StatusRevision, copyFeedbackRevision]);
  const displayedError = localErrors[0] || view.ErrorNotice;
  const acknowledgeError = (id: string) => {
    if (localErrors[0]?.ID === id) setLocalErrors(queue => queue[0]?.ID === id ? queue.slice(1) : queue);
    else if (view.ErrorNotice?.ID === id) emit('ackError', id);
  };
  return <><WalletShell view={view} status={status} emit={emit}/><DialogHost onError={reportClientError} errorNotice={displayedError} onAcknowledgeError={acknowledgeError}/></>;
}
