import { useEffect, useRef, useState } from 'react';
import type { Node, Page, View, WalletEvent } from './types';
import { applyAppearance, isAssetTable, isWalletRow, validView } from './protocol';
import { AddressBookIcon, CopyIcon, Mark, SaveAddressIcon, SettingsIcon } from './icons';

type Emit = (id: string, value?: string) => void;
const publicFieldLimit = 4096;
const blank: View = { Title: 'CryptoWallet', Status: '', StatusRevision: 0, Active: '', Pages: [] };

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

export function Field({ node, emit }: { node: Node; emit: Emit }) {
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
    if (node.Kind === 'entry') {
      // Public draft values only. Bound echoes retained while a field is
      // focused; unknown Go values and explicit busy/reset views still win.
      pending.current.push(v);
      if (pending.current.length > publicFieldLimit) pending.current.shift();
    }
    setValue(v); emit(node.ID, v);
  };
  const id = 'wallet-' + node.ID;
  if (node.Kind === 'check') return <label className="check-field" title={node.Tooltip}><input type="checkbox" checked={value === 'true'} disabled={node.Disabled} onChange={event => update(String(event.target.checked))}/>{node.Text}</label>;
  return <label className="field" htmlFor={id} title={node.Tooltip}><span>{node.Text}</span>{node.Kind === 'combo'
    ? <select id={id} value={value} disabled={node.Disabled} onChange={event => update(event.target.value)}>{!(node.Options || []).some(option => option.ID === value) && <option value="" disabled>Select…</option>}{(node.Options || []).map(option => <option key={option.ID} value={option.ID}>{option.Text}</option>)}</select>
    : <input ref={input} id={id} value={value} disabled={node.Disabled} autoComplete="off" spellCheck={false} maxLength={publicFieldLimit} onBlur={() => { pending.current = []; }} onChange={event => update(event.target.value)}/>}</label>;
}

export function PublicNode({ node, emit }: { node: Node; emit: Emit }) {
  const children = () => (node.Children || []).map((child, index) => <PublicNode key={child.ID || `${child.Kind}-${index}`} node={child} emit={emit}/>);
  switch (node.Kind) {
    case 'heading': return <h2 className="node-heading" title={node.Tooltip}>{node.Text}</h2>;
    case 'label': return node.Text ? <p className="node-label" title={node.Tooltip}>{node.Text}</p> : null;
    case 'separator': return <hr/>;
    case 'column': return node.ID.startsWith('bookComposer/') ? <>{children()}</> : isAssetTable(node) ? <AssetTable node={node} emit={emit}/> : <div className="node-column">{children()}</div>;
    case 'row': {
      if (node.ID.startsWith('walletSections/') || node.ID === 'settingsSections') return <nav className="wallet-sections" role="tablist" aria-label={node.Text}>{(node.Children || []).map(child => <button key={child.ID} type="button" role="tab" aria-selected={child.Text === node.Value} className={child.Text === node.Value ? 'selected' : ''} disabled={child.Disabled} data-action={child.ID} onClick={() => emit(child.ID)}>{child.Text}</button>)}</nav>;
      if (node.ID.startsWith('recipientField/')) return <div className="recipient-field">{children()}</div>;
      return <div className="node-row" title={node.Tooltip}>{children()}</div>;
    }
    case 'address': return <div className={`address-field ${/^(Receive address|Owned wallet receive address)$/.test(node.Text) ? 'receive-address-line' : ''}`} title={node.Tooltip}>{node.Text && <span className="address-label">{node.Text}{/^(Receive address|Owned wallet receive address)$/.test(node.Text) ? ':' : ''}</span>}<code className="address">{node.Value}</code></div>;
    case 'entry': case 'combo': case 'check': return <Field node={node} emit={emit}/>;
    case 'button': {
      const copy = node.ID.startsWith('copyReceive/');
      const loadAddress = node.ID.startsWith('addressBookLoad/'), saveAddress = node.ID.startsWith('addressBookSave/');
      const primary = /^(create12|unlock|confirmRecovery|send\/|prepare\/|stakingSend\/|exchangeConnect|exchangeSwap)/.test(node.ID);
      return <button type="button" autoFocus={node.ID === 'unlock'} data-action={node.ID} className={`${copy ? 'copy-button' : ''} ${primary ? 'primary' : ''} ${loadAddress || saveAddress ? 'addressbook-action' : ''}`} disabled={node.Disabled} title={node.Tooltip || (copy ? 'Copy receive address' : loadAddress || saveAddress ? node.Text : undefined)} aria-label={node.Text === '⧉' ? 'Copy receive address' : loadAddress || saveAddress ? node.Text : undefined} onClick={() => emit(node.ID)}>{copy ? <><CopyIcon/>{node.Text !== '⧉' && <span>Copy address</span>}</> : loadAddress ? <AddressBookIcon/> : saveAddress ? <SaveAddressIcon/> : node.Text}</button>;
    }
  }
}

function Portfolio({ page, emit }: { page: Page; emit: Emit }) {
  const nodes = page.Nodes || [], rows = nodes.filter(isWalletRow);
  const headingRow = (node: Node) => node.Kind === 'row' && node.Children?.[0]?.Kind === 'heading' && node.Children[0].Text === 'Network';
  const first = nodes.findIndex(isWalletRow), last = nodes.reduce((found, node, index) => isWalletRow(node) ? index : found, -1);
  return <><div className="page-heading"><div><p className="eyebrow">YOUR WALLETS</p><h1>{page.Title}</h1></div><span className="network-count">{rows.length} networks</span></div><div className="portfolio-toolbar">{nodes.slice(0, first < 0 ? nodes.length : first).filter(node => !headingRow(node)).map((node, index) => <PublicNode key={node.ID || index} node={node} emit={emit}/>)}</div>{rows.length > 0 && <div className="wallet-table-wrap"><table className="wallet-table"><thead><tr><th>Network</th><th>Native balance</th><th>Receive address</th><th><span className="sr-only">Copy</span></th></tr></thead><tbody>{rows.map(row => <tr key={row.ID} tabIndex={0} onClick={event => { if (!(event.target as HTMLElement).closest('button,input,select')) emit(row.ID); }} onKeyDown={event => { if (event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); emit(row.ID); } }}>{(row.Children || []).map((node, index) => <td key={node.ID || index}>{index === 0 ? <div className="network-name"><span className="network-avatar" aria-hidden="true">{node.Text.slice(0, 1)}</span><PublicNode node={node} emit={emit}/></div> : <PublicNode node={node} emit={emit}/>}</td>)}</tr>)}</tbody></table></div>}{last >= 0 && nodes.slice(last + 1).map((node, index) => <PublicNode key={node.ID || index} node={node} emit={emit}/>)}</>;
}

export function PublicContent({ page, emit }: { page: Page; emit: Emit }) {
  const nodes = page.Nodes || [];
  const portfolio = page.ID.startsWith('group/') && nodes.some(isWalletRow);
  if (portfolio) return <Portfolio page={page} emit={emit}/>;
  const onboarding = page.ID.startsWith('group/') && !nodes.some(isWalletRow);
  return <div className={onboarding ? 'welcome-card' : 'content-card'}>{onboarding && <div className="welcome-symbol"><Mark/></div>}{nodes.map((node, index) => <PublicNode key={node.ID || `${node.Kind}-${index}`} node={node} emit={emit}/>)}</div>;
}

export function WalletShell({ view, status, emit }: { view: View; status: string; emit: Emit }) {
  const pages = view.Pages || [], active = pages.find(page => page.ID === view.Active) || pages[0];
  const metadata = view.Title.split(' · ').slice(1).join(' · ');
  const portfolio = active?.ID.startsWith('group/') && active.Nodes?.some(isWalletRow);
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
  return <div className="wallet-shell"><header className="app-header"><a className="brand" href="#" onClick={event => { event.preventDefault(); const first = pages.find(page => page.ID.startsWith('group/')); if (first) emit('tab', first.ID); }}><Mark/><span>CryptoWallet</span></a><span className="build-metadata">{metadata}</span></header><nav className="tabs" aria-label="Wallet tabs">{pages.map(page => <div className={`tab ${active?.ID === page.ID ? 'active' : ''} ${page.ID === 'exchange' ? 'utility-start' : ''}`} key={page.ID} onAuxClick={event => { if (event.button === 1 && page.ID.startsWith('wallet/')) { event.preventDefault(); emit('closeTab', page.ID); } }}><button type="button" role="tab" aria-selected={active?.ID === page.ID} aria-controls="wallet-panel" onClick={() => emit('tab', page.ID)}>{page.ID === 'settings' && <SettingsIcon/>}{page.ID === 'addressbook' && <AddressBookIcon/>}{page.Title}</button>{page.ID.startsWith('wallet/') && <button className="close-tab" type="button" aria-label={`Close ${page.Title}`} onClick={() => emit('closeTab', page.ID)}>×</button>}</div>)}</nav><main className={`workspace ${portfolio ? 'portfolio-workspace' : active?.ID.startsWith('group/') ? 'welcome-workspace' : ''}`} id="wallet-panel" role="tabpanel" aria-label={active?.Title}>{active ? <PublicContent page={active} emit={emit}/> : <div className="connecting"><Mark/><p>Opening your encrypted wallet…</p></div>}</main><footer className="status-bar" aria-live="polite" aria-atomic="true"><span className={status ? 'status-dot visible' : 'status-dot'} aria-hidden="true"/><span>{status}</span></footer></div>;
}

export function PublicApp() {
  const [view, setView] = useState(blank), [status, setStatus] = useState('');
  const queue = useRef(Promise.resolve()), live = useRef(true);
  const emit: Emit = (ID, Value = '') => {
    const bridge = window.go?.webui?.PublicBridge;
    if (!bridge) { setStatus('The wallet connection is unavailable. Close and reopen CryptoWallet.'); return; }
    const event: WalletEvent = { ID, Value };
    queue.current = queue.current.then(async () => { if (await bridge.Event(event) !== true && ID !== 'activity') throw new Error('Unavailable'); }).catch(() => { if (live.current && ID !== 'activity') setStatus('Request failed. Check wallet state before retrying.'); });
  };
  useEffect(() => {
    live.current = true; applyAppearance();
    let disposed = false, pushed = false;
    const accept = (value: unknown) => { if (disposed) return; if (validView(value)) { applyAppearance(value.Appearance); setView(value); document.title = value.Title; } else setStatus('The wallet returned an invalid view. Close and reopen CryptoWallet.'); };
    const bridge = window.go?.webui?.PublicBridge;
    const unsubscribe = window.runtime?.EventsOn('wallet:view', value => { pushed = true; accept(value); });
    if (bridge) bridge.Snapshot().then(value => { if (!pushed) accept(value); }).catch(() => { if (!disposed) setStatus('The wallet connection is unavailable. Close and reopen CryptoWallet.'); });
    else setStatus('The wallet connection is unavailable. Close and reopen CryptoWallet.');
    let lastActivity = 0;
    const activity = () => { const now = Date.now(); if (now - lastActivity >= 1000) { lastActivity = now; emit('activity'); } };
    window.addEventListener('pointerdown', activity); window.addEventListener('keydown', activity);
    return () => { disposed = true; live.current = false; unsubscribe?.(); window.removeEventListener('pointerdown', activity); window.removeEventListener('keydown', activity); };
  }, []);
  useEffect(() => { setStatus(view.Status); }, [view.Status, view.StatusRevision]);
  useEffect(() => { if (!status) return; const timer = window.setTimeout(() => setStatus(''), 7000); return () => window.clearTimeout(timer); }, [status, view.StatusRevision]);
  return <WalletShell view={view} status={status} emit={emit}/>;
}
