import { describe, expect, it } from 'vitest';
import { inputWithinLimit, isWalletRow, phraseWords, validPrompt, validView } from './protocol';
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
