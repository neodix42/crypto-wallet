export interface Option { ID: string; Text: string }
export interface Node {
  Kind: 'label' | 'heading' | 'separator' | 'row' | 'column' | 'button' | 'entry' | 'combo' | 'check' | 'address' | 'transactionTable' | 'explorer';
  ID: string; Text: string; Value: string; Tooltip: string; Disabled: boolean;
  Options: Option[] | null; Children: Node[] | null;
  Error?: string; InputMode?: '' | 'decimal';
}
export interface Page { ID: string; Title: string; Nodes: Node[] | null; ParentID?: string }
export interface Appearance { theme: 'light' | 'dark'; fontSize: number }
export interface ErrorNotice { ID: string; Title: string; Operation: string; Code: string; Message: string }
export interface View { Title: string; Status: string; StatusRevision: number; Active: string; Pages: Page[] | null; Appearance?: Appearance; AccountGroupID?: string; ErrorNotice?: ErrorNotice | null }
export interface WalletEvent { ID: string; Value: string }
export interface Prompt {
  id: string; kind: 'input' | 'publicInput' | 'confirm' | 'notice' | 'receive' | 'recovery' | 'recoveryBackup' | 'pending';
  title: string; labels?: string[]; summary?: string; phrase?: string; png?: string; address?: string;
  appearance?: Appearance;
  choices?: { id: string; label: string; summary: string }[];
}
export interface Reply { promptId: string; accepted: boolean; values?: string[] }
declare global {
  interface Window {
    go?: { webui?: {
      PublicBridge?: { Snapshot(): Promise<unknown>; Event(event: WalletEvent): Promise<boolean> };
      DialogBridge?: { Snapshot(): Promise<unknown>; Reply(reply: Reply): Promise<boolean>; Copy(promptId: string): Promise<boolean>; Activity(promptId: string): Promise<boolean> };
    } };
    runtime?: { EventsOn(name: string, callback: (...args: unknown[]) => void): () => void; ClipboardSetText?(text: string): Promise<boolean>; BrowserOpenURL?(url: string): void };
  }
}
