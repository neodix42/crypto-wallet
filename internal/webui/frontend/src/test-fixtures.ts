import type { Node, View } from './types';
// Public BIP39 fixture only. Never substitute a user phrase in tests.
export const id = '1234567890abcdef1234567890abcdef';
export const phrase = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
export const node = (Kind: Node['Kind'], ID = '', Text = '', Value = ''): Node => ({ Kind, ID, Text, Value, Tooltip: '', Disabled: false, Options: null, Children: null });
export const view = (): View => ({ Title: 'CryptoWallet · 0.1.1 · mainnet', Status: '', StatusRevision: 0, Active: 'group/0', Pages: [{ ID: 'group/0', Title: 'Main account', Nodes: [node('heading', '', 'Your wallets')] }] });
