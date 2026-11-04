import type { Node } from './types';
import { safeExplorerURL } from './explorer';
import { ExternalLinkIcon } from './icons';

export function ExplorerLink({ node }: { node: Node }) {
  if (!safeExplorerURL(node.Value)) return <span className="transaction-explorer-unavailable" title="Transaction explorer unavailable" aria-label="Transaction explorer unavailable">—</span>;
  return <button type="button" className="explorer-link" disabled={node.Disabled} title={node.Value} aria-label={`View transaction ${node.Text || node.Value} in external explorer`} onClick={() => { if (!node.Disabled && safeExplorerURL(node.Value)) window.runtime?.BrowserOpenURL?.(node.Value); }}><ExternalLinkIcon/></button>;
}

function shortAddress(value: string): string {
  if (value.length <= 12 || !/^[a-zA-Z0-9_.:-]+$/.test(value) || /^(unknown|unavailable|missing|pending)(?:$|[:._-])/i.test(value)) return value;
  return value.slice(0, 6) + ' . . . ' + value.slice(-6);
}

function TransactionCell({ cell, index }: { cell: Node; index: number }) {
  if (index === 4) return <td className="transaction-explorer-cell"><ExplorerLink node={cell}/></td>;
  if (index === 5) return <td className="transaction-comment-cell" title={cell.Text}><span className="transaction-comment" title={cell.Text} aria-label={cell.Text}>{cell.Text}</span></td>;
  if (cell.Kind === 'address') {
    const context = index === 1 ? 'Source address' : index === 2 ? 'Destination address' : 'Address';
    return <td title={cell.Value} aria-label={`${context}: ${cell.Value}`}><code className="address transaction-address" title={cell.Value} aria-hidden="true">{shortAddress(cell.Value)}</code></td>;
  }
  return <td title={cell.Tooltip}>{cell.Text}</td>;
}

export function TransactionTable({ node }: { node: Node }) {
  return <div className="transaction-table-wrap" tabIndex={0} role="region" aria-label={node.Text || 'Transaction history'}><table className="transaction-table"><caption>{node.Text || 'Transaction history'}</caption><thead><tr>{['Date and time', 'Source', 'Destination', 'Amount', 'Explorer', 'Comment', 'Status'].map(title => <th key={title} scope="col">{title}</th>)}</tr></thead><tbody>{(node.Children || []).map(row => <tr key={row.ID}>{(row.Children || []).map((cell, index) => <TransactionCell key={index} cell={cell} index={index}/>)}</tr>)}</tbody></table></div>;
}
