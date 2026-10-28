export interface Option { ID: string; Text: string }
export interface Node {
  Kind: 'label' | 'heading' | 'separator' | 'row' | 'column' | 'button' | 'entry' | 'combo' | 'check' | 'address';
  ID: string; Text: string; Value: string; Tooltip: string; Disabled: boolean;
  Options: Option[] | null; Children: Node[] | null;
}
export interface Page { ID: string; Title: string; Nodes: Node[] | null }
export interface Appearance { theme: 'light' | 'dark'; fontSize: number }
export interface View { Title: string; Status: string; StatusRevision: number; Active: string; Pages: Page[] | null; Appearance?: Appearance }
export interface WalletEvent { ID: string; Value: string }
export interface Prompt {
  id: string; kind: 'input' | 'confirm' | 'notice' | 'receive' | 'recovery' | 'recoveryBackup';
  title: string; labels?: string[]; summary?: string; phrase?: string; png?: string; address?: string;
  appearance?: Appearance;
}
export interface Reply { promptId: string; accepted: boolean; values?: string[] }
declare global {
  interface Window {
    go?: { webui?: {
      PublicBridge?: { Snapshot(): Promise<unknown>; Event(event: WalletEvent): Promise<boolean> };
      PrivateBridge?: { Prompt(): Promise<unknown>; Reply(reply: Reply): Promise<boolean>; Copy(): Promise<boolean>; Activity(): Promise<boolean> };
    } };
    runtime?: { EventsOn(name: string, callback: (...args: unknown[]) => void): () => void };
  }
}
