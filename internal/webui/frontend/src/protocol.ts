import type { Appearance, Node, Prompt, View } from './types';

const kinds = new Set(['label', 'heading', 'separator', 'row', 'column', 'button', 'entry', 'combo', 'check', 'address', 'transactionTable', 'explorer']);
const transactionCells = ['label', 'address', 'address', 'label', 'explorer', 'label'];
const encoder = new TextEncoder();
export function boundedText(value: unknown, max: number, empty = true): value is string {
  return typeof value === 'string' && (empty || value.length > 0) && !value.includes('\0') && encoder.encode(value).length <= max;
}
function object(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function list(value: unknown, max: number): value is unknown[] | null { return value === null || Array.isArray(value) && value.length <= max; }
export function validAppearance(value: unknown): value is Appearance {
  return object(value) && Object.keys(value).length === 2 && (value.theme === 'light' || value.theme === 'dark') && Number.isInteger(value.fontSize) && Number(value.fontSize) >= 14 && Number(value.fontSize) <= 24 && Number(value.fontSize) % 2 === 0;
}
export function applyAppearance(value?: Appearance): void {
  const appearance = validAppearance(value) ? value : { theme: 'light', fontSize: 18 };
  document.documentElement.dataset.theme = appearance.theme;
  document.documentElement.dataset.fontSize = String(appearance.fontSize);
}
export function validView(value: unknown): value is View {
  if (object(value) && value.Appearance !== undefined && !validAppearance(value.Appearance)) return false;
  if (!object(value) || !boundedText(value.Title, 256) || !boundedText(value.Status, 65536) || !boundedText(value.Active, 256) || !Number.isSafeInteger(value.StatusRevision) || Number(value.StatusRevision) < 0 || !list(value.Pages, 1000)) return false;
  let remaining = 20000;
  function nodes(values: unknown[] | null, depth: number): boolean {
    if (depth > 32 || !list(values, 20000)) return false;
    return (values || []).every(n => {
      if (--remaining < 0 || !object(n) || !kinds.has(String(n.Kind)) || !boundedText(n.ID, 256) || !boundedText(n.Text, 65536) || !boundedText(n.Tooltip, 65536) || !boundedText(n.Value, 4096) || typeof n.Disabled !== 'boolean' || !list(n.Options, 10000) || !list(n.Children, 20000)) return false;
      if (n.Kind === 'entry' && /password|phrase|mnemonic|secret|apikey|api_key|credential|passphrase/i.test(n.ID)) return false;
      if (n.Kind === 'transactionTable' && !validTransactionRows(n.Children as Node[] | null)) return false;
      return (n.Options || []).every(option => object(option) && boundedText(option.ID, 4096) && boundedText(option.Text, 4096)) && nodes(n.Children, depth + 1);
    });
  }
  const seen = new Set<string>();
  return (value.Pages || []).every(page => {
    if (!object(page) || !boundedText(page.ID, 256, false) || seen.has(page.ID) || !boundedText(page.Title, 256) || !list(page.Nodes, 20000) || !nodes(page.Nodes, 0)) return false;
    seen.add(page.ID); return true;
  });
}
function validTransactionRows(rows: Node[] | null): boolean {
  return (rows || []).every(row => object(row) && row.Kind === 'row' && Array.isArray(row.Children) && row.Children.length === transactionCells.length + 1 && row.Children.slice(0, transactionCells.length).every((cell, index) => object(cell) && cell.Kind === transactionCells[index] && emptyChildren(cell)) && validTransactionStatus(row.Children[transactionCells.length]));
}
function emptyChildren(node: Record<string, unknown>): boolean { return node.Children === null || Array.isArray(node.Children) && node.Children.length === 0; }
function validTransactionStatus(status: unknown): boolean {
  if (!object(status)) return false;
  if (status.Kind === 'label') return emptyChildren(status);
  if (status.Kind !== 'column' || !boundedText(status.Value, 128, false)) return false;
  const actions: Record<string, string[]> = { PREPARED: ['sendOperation', 'cancelPrepared'], SIGNED_NOT_SUBMITTED: ['retry', 'reconcile'], UNKNOWN: ['retry', 'reconcile'], SUBMITTED: ['reconcile'], SUBMITTING: ['reconcile'], REORGED: ['reconcile'] };
  const kinds = Object.hasOwn(actions, String(status.Text)) ? actions[String(status.Text)] : undefined;
  if (!kinds || !Array.isArray(status.Children) || status.Children.length !== kinds.length + 1 || !object(status.Children[0]) || status.Children[0].Kind !== 'label' || !emptyChildren(status.Children[0])) return false;
  const children = status.Children;
  return kinds.every((kind, index) => { const button: unknown = children[index + 1]; return object(button) && button.Kind === 'button' && button.ID === `${kind}/${status.Value}` && button.Value === '' && emptyChildren(button) && (button.Options === null || Array.isArray(button.Options) && button.Options.length === 0); });
}
export function isWalletRow(node: Node): boolean {
  return node.Kind === 'row' && node.ID.startsWith('open/') && (node.Children || []).length === 4 && node.Children?.[2]?.Kind === 'address';
}
export function isAssetTable(node: Node): boolean {
  return node.Kind === 'column' && node.ID.startsWith('walletAssets/') && Array.isArray(node.Children) && node.Children.length > 0 && node.Children.every(row => row.Kind === 'row' && row.ID.startsWith('walletAsset/') && row.Children?.length === 3 && row.Children[0].Kind === 'button' && row.Children[1].Kind === 'label' && row.Children[2].Kind === 'label');
}
export function validPrompt(value: unknown): value is Prompt {
  if (!object(value) || typeof value.id !== 'string' || !/^[a-f0-9]{32}$/.test(value.id) || !boundedText(value.title, 256, false)) return false;
  let fields = ['id', 'kind', 'title'];
  if (Object.hasOwn(value, 'appearance')) {
    if (!validAppearance(value.appearance)) return false;
    fields.push('appearance');
  }
  switch (value.kind) {
    case 'input':
      fields = fields.concat('labels');
      if (!Array.isArray(value.labels) || value.labels.length < 1 || value.labels.length > 8 || !value.labels.every(v => boundedText(v, 256, false))) return false;
      break;
    case 'recovery': case 'recoveryBackup':
      fields = fields.concat('phrase');
      if (!boundedText(value.phrase, 512, false) || !/^[a-z]+(?: [a-z]+)*$/.test(value.phrase) || ![12, 15, 18, 21, 24].includes(value.phrase.split(' ').length)) return false;
      break;
    case 'receive':
      fields = fields.concat('summary', 'address', 'png');
      if (!boundedText(value.address, 512, false) || !boundedText(value.png, 349528, false) || !/^iVBORw0KGgo[A-Za-z0-9+/]*={0,2}$/.test(value.png)) return false;
      if (!boundedText(value.summary, 65536)) return false;
      break;
    case 'confirm': case 'notice':
      fields = fields.concat('summary');
      if (!boundedText(value.summary, 65536)) return false;
      break;
    default: return false;
  }
  return Object.keys(value).length === fields.length && fields.every(field => Object.hasOwn(value, field));
}
export function inputWithinLimit(value: string): boolean { return boundedText(value, 4096); }
export function phraseWords(prompt: Prompt): string[] { return prompt.kind === 'recovery' || prompt.kind === 'recoveryBackup' ? (prompt.phrase || '').split(' ') : []; }
