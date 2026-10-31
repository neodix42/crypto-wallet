import { describe, expect, it } from 'vitest';
import { inputWithinLimit, isAssetTable, isWalletRow, phraseWords, validPrompt, validView } from './protocol';
import type { Node, Prompt } from './types';
import { id, node, phrase, view } from './test-fixtures';

describe('public representation', () => {
  it('accepts public trees and rejects secret input identities, malformed payloads and allocation excess', () => {
    expect(validView(view())).toBe(true);
    const input = node('entry', 'field/recipient', 'Address');
    const test = view(); test.Pages![0].Nodes = [input];
    expect(validView(test)).toBe(true);
    for (const key of ['password', 'secret', 'mnemonic', 'api_key', 'credential', 'phrase', 'apikey']) { input.ID = `field/${key}`; expect(validView(test)).toBe(false); }
    input.ID = 'field/amount'; input.Value = 'a'.repeat(4097); expect(validView(test)).toBe(false);
    input.Value = ''; input.Kind = 'script' as Node['Kind']; expect(validView(test)).toBe(false);
    input.Kind = 'label'; input.Text = '\0'; expect(validView(test)).toBe(false);
    input.Text = ''; input.Children = [input]; expect(validView(test)).toBe(false);
    expect(validView({ ...view(), StatusRevision: -1 })).toBe(false);
    const duplicate = view(); duplicate.Pages!.push(duplicate.Pages![0]); expect(validView(duplicate)).toBe(false);
    expect(validView({ ...view(), Pages: Array(1001).fill({}) })).toBe(false);
    expect(validView(null)).toBe(false);
  });
  it('recognizes a wallet table row only from its complete structural identity', () => {
    const row = node('row', 'open/public-account'); row.Children = [node('button'), node('label'), node('address'), node('button')];
    expect(isWalletRow(row)).toBe(true); row.Children.pop(); expect(isWalletRow(row)).toBe(false);
  });
  it('recognizes assets only as complete three-cell Go-authored rows', () => {
    const table = node('column', 'walletAssets/owned'), row = node('row', 'walletAsset/exact');
    row.Children = [node('button', 'asset/owned/exact', 'TOKEN'), node('label', '', 'Unknown'), node('label', '', '6')];
    table.Children = [row]; expect(isAssetTable(table)).toBe(true);
    row.Children[0].Kind = 'label'; expect(isAssetTable(table)).toBe(false);
    row.Children[0].Kind = 'button'; row.Children.pop(); expect(isAssetTable(table)).toBe(false);
    table.Children = []; expect(isAssetTable(table)).toBe(false);
  });
  it('validates every combo option before accepting a settings view', () => {
    const state = view(), select = node('combo', 'field/region', 'Region');
    state.Pages![0].Nodes = [select];
    select.Options = [{ ID: 'official', Text: 'Official endpoint' }];
    expect(validView(state)).toBe(true);
    for (const option of [null, [], { ID: 'x\0', Text: 'Endpoint' }, { ID: 'official', Text: 'x'.repeat(4097) }]) {
      select.Options = [option] as typeof select.Options;
      expect(validView(state)).toBe(false);
    }
    const empty = view(); empty.Pages = null;
    expect(validView(empty)).toBe(true);
  });
  it('accepts only complete readonly seven-cell transaction rows without nested controls', () => {
    const table = node('transactionTable'), row = node('row', 'walletHistory/exact');
    const cells = [node('label'), node('address'), node('address'), node('label'), node('explorer'), node('label'), node('label')];
    const state = view(); state.Pages![0].Nodes = [table];
    for (const empty of [null, []]) { table.Children = empty; expect(validView(state)).toBe(true); }
    table.Children = [row]; row.Children = cells; expect(validView(state)).toBe(true);
    cells[0].Children = []; expect(validView(state)).toBe(true);
    cells[0].Children = [node('button', 'send/forbidden')]; expect(validView(state)).toBe(false);
    cells[0].Children = null;
    for (const malformed of [null, 'string', { Kind: 'row', Children: null }, { ...row, Kind: 'button' }, { ...row, Children: cells.slice(1) }, { ...row, Children: [null, ...cells.slice(1)] }, { ...row, Children: [node('entry'), ...cells.slice(1)] }]) {
      table.Children = [malformed] as Node[]; expect(validView(state)).toBe(false);
    }
  });
  it('accepts only exact original-operation action sets in the Status cell and keeps provider cells inert', () => {
    const state = view(), table = node('transactionTable'), row = node('row', 'owned-history-row');
    state.Pages![0].Nodes = [table]; table.Children = [row];
    function status(operationState = 'PREPARED', kinds = ['sendOperation', 'cancelPrepared']): Node {
      const cell = node('column', 'history-status', operationState, 'original-plan');
      cell.Children = [node('label', '', operationState), ...kinds.map(kind => node('button', `${kind}/original-plan`, kind))]; return cell;
    }
    const inert = [node('label'), node('address'), node('address'), node('label'), node('explorer'), node('label')];
    for (const [operationState, kinds] of [['PREPARED', ['sendOperation', 'cancelPrepared']], ['SIGNED_NOT_SUBMITTED', ['retry', 'reconcile']], ['UNKNOWN', ['retry', 'reconcile']], ['SUBMITTED', ['reconcile']], ['SUBMITTING', ['reconcile']], ['REORGED', ['reconcile']]] as [string, string[]][]) {
      row.Children = [...inert, status(operationState, kinds)]; expect(validView(state)).toBe(true);
    }
    for (const mutate of [
      (cell: Node) => { cell.Text = 'CONFIRMED'; }, (cell: Node) => { cell.Text = '__proto__'; }, (cell: Node) => { cell.Text = 'constructor'; },
      (cell: Node) => { cell.Value = ''; }, (cell: Node) => { cell.Value = 'foreign-operation'; },
      (cell: Node) => { cell.Children![1].ID = 'sendOperation/replacement'; },
      (cell: Node) => { cell.Children![1].Kind = 'entry'; }, (cell: Node) => { cell.Children![1].Value = 'replacement'; },
      (cell: Node) => { cell.Children![1].Children = [node('button', 'send/replacement')]; },
      (cell: Node) => { cell.Children![1].Options = [{ ID: 'replacement', Text: '' }]; },
      (cell: Node) => { cell.Children!.push(node('button', 'send/replacement')); },
      (cell: Node) => { cell.Children = null; }, (cell: Node) => { cell.Children![0].Kind = 'button'; },
    ]) {
      const cell = status(); mutate(cell); row.Children = [...inert, cell]; expect(validView(state)).toBe(false);
    }
    row.Children = [...inert, status()];
    for (let index = 0; index < inert.length; index++) {
      const cells = [...inert, status()]; cells[index] = status(); row.Children = cells; expect(validView(state)).toBe(false);
    }
  });
});

describe('private prompt validation', () => {
  const fixtures: Prompt[] = [
    { id, kind: 'input', title: 'Unlock', labels: ['App password'] },
    { id, kind: 'confirm', title: 'Approve', summary: 'Exact Go-authored details' },
    { id, kind: 'notice', title: 'Notice', summary: '' },
    { id, kind: 'receive', title: 'Receive', summary: 'Ethereum', address: '0x123', png: 'iVBORw0KGgo=' },
    { id, kind: 'recovery', title: 'Record', phrase },
    { id, kind: 'recoveryBackup', title: 'Backup', phrase },
  ];
  it('accepts exactly the supported prompt shapes and rejects cross-kind data', () => {
    for (const prompt of fixtures) {
      expect(validPrompt(prompt)).toBe(true);
      expect(validPrompt({ ...prompt, extra: 'secret' })).toBe(false);
      expect(validPrompt({ ...prompt, id: 'replayed' })).toBe(false);
      expect(validPrompt({ ...prompt, title: null })).toBe(false);
    }
    expect(validPrompt({ id, kind: 'file', title: 'Path', save: true })).toBe(false);
    expect(validPrompt({ id, kind: 'copy', title: 'Copy', address: 'x' })).toBe(false);
    expect(validPrompt({ id, kind: 'input', title: 'Input', labels: Array(9).fill('Field') })).toBe(false);
    expect(validPrompt({ id, kind: 'receive', title: 'Receive', summary: '', address: 'x', png: 'https://tracking.invalid/qr' })).toBe(false);
    expect(validPrompt({ id, kind: 'recovery', title: 'Phrase', phrase: phrase + '  extra' })).toBe(false);
  });
  it('bounds UTF-8 bytes rather than treating multibyte characters as one byte', () => {
    expect(inputWithinLimit('é'.repeat(2048))).toBe(true);
    expect(inputWithinLimit('é'.repeat(2049))).toBe(false);
    expect(inputWithinLimit('x\0')).toBe(false);
    expect(phraseWords(fixtures[4])).toHaveLength(12);
    expect(phraseWords(fixtures[0])).toEqual([]);
  });
});
