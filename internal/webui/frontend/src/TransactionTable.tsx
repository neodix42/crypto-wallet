import type { Node } from './types';
import { safeExplorerURL } from './explorer';

export function ExplorerLink({ node }: { node: Node }) {
  if (!safeExplorerURL(node.Value)) return <span className="transaction-id" title={node.Tooltip}>{node.Text}</span>;
  return <button type="button" className="explorer-link" disabled={node.Disabled} title={node.Value} aria-label={`View transaction ${node.Text} in external explorer`} onClick={() => { if (safeExplorerURL(node.Value)) window.runtime?.BrowserOpenURL?.(node.Value); }}>{node.Text}<span aria-hidden="true">↗</span></button>;
}

export function TransactionTable({ node, emit }: { node: Node; emit: (id: string) => void }) {
  return <div className="transaction-table-wrap" tabIndex={0} role="region" aria-label={node.Text || 'Transaction history'}><table className="transaction-table"><caption>{node.Text || 'Transaction history'}</caption><thead><tr>{['Date and time', 'Source', 'Destination', 'Amount', 'Explorer', 'Comment', 'Status'].map(title => <th key={title} scope="col">{title}</th>)}</tr></thead><tbody>{(node.Children || []).map(row => <tr key={row.ID}>{(row.Children || []).map((cell, index) => <td key={index} title={cell.Tooltip}>{cell.Kind === 'address' ? <code className="address">{cell.Value}</code> : cell.Kind === 'explorer' ? <ExplorerLink node={cell}/> : index === 6 && cell.Kind === 'column' ? <div className="transaction-status"><span>{cell.Children?.[0]?.Text}</span><div className="transaction-actions">{(cell.Children || []).slice(1).map(action => <button key={action.ID} type="button" data-action={action.ID} disabled={node.Disabled || row.Disabled || cell.Disabled || action.Disabled} title={action.Tooltip} onClick={() => emit(action.ID)}>{action.Text}</button>)}</div></div> : cell.Text}</td>)}</tr>)}</tbody></table></div>;
}
