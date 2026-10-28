import { useEffect, useRef, useState } from 'react';
import type { Prompt, Reply } from './types';
import { applyAppearance, inputWithinLimit, phraseWords, validPrompt } from './protocol';
import { CopyIcon, Mark } from './icons';

const unavailable = 'This request is no longer available. Return to your wallet and try again.';
type Identity = { id: string; kind: Prompt['kind'] };

export function PromptContents({ prompt, error, copyStatus, copying, copy }: { prompt: Prompt; error: string; copyStatus: string; copying: boolean; copy: () => void }) {
  const recovery = prompt.kind === 'recovery' || prompt.kind === 'recoveryBackup';
  return <><div className="private-body">{prompt.kind === 'input' && <div className="private-fields">{(prompt.labels || []).map((label, index) => <label className="field" key={index}><span>{label}</span><input name={`private-${index}`} type="password" autoComplete="off" autoCorrect="off" autoCapitalize="none" spellCheck={false} data-lpignore="true" maxLength={4096} autoFocus={index === 0}/></label>)}</div>}{recovery && <><p className="recovery-warning">Keep these words offline and in order. Anyone with your phrase can access your wallets.</p><ol className="recovery-words">{phraseWords(prompt).map((word, index) => <li key={index} data-recovery-word><span className="word-number">{index + 1}</span><span>{word}</span></li>)}</ol><p className="private-note">{prompt.kind === 'recovery' ? 'Next, confirm a few words from your written copy.' : 'This is the same recovery phrase for all your wallets.'} Keep any additional BIP39 passphrase separately.</p>{prompt.kind === 'recoveryBackup' && <div className="phrase-copy"><button type="button" disabled={copying} onClick={copy}><CopyIcon/>Copy phrase</button><p className="private-note">Copying exposes these words to other apps and clipboard history. Clear the clipboard after use; existing copies may remain.</p><p className="copy-feedback" role="status">{copyStatus}</p></div>}</>}{prompt.kind === 'receive' && <><div className="qr-frame"><img draggable={false} src={`data:image/png;base64,${prompt.png}`} alt="Receive address QR code"/></div><div className="receive-address"><code>{prompt.address}</code><button type="button" onClick={copy} disabled={copying}><CopyIcon/>Copy address</button></div><p className="copy-feedback" role="status">{copyStatus}</p></>}{(prompt.kind === 'receive' || prompt.kind === 'confirm' || prompt.kind === 'notice') && <pre className="prompt-summary">{prompt.summary}</pre>}{error && <p className="private-error" role="alert">{error}</p>}</div></>;
}

export function PrivateApp() {
  const [prompt, setPrompt] = useState<Prompt | null>(null), [error, setError] = useState('');
  const [copying, setCopying] = useState(false), [copyStatus, setCopyStatus] = useState('');
  const identity = useRef<Identity | null>(null), busy = useRef(false), abandoned = useRef(false);
  const form = useRef<HTMLFormElement>(null);
  const erase = () => {
    form.current?.querySelectorAll('input').forEach(input => { input.value = ''; });
    form.current?.querySelectorAll('[data-recovery-word]').forEach(word => { word.textContent = ''; });
    form.current?.querySelectorAll('img').forEach(image => image.removeAttribute('src'));
    identity.current = null; setPrompt(null); setCopyStatus('');
  };
  const finish = async (accepted: boolean) => {
    const current = identity.current;
    if (!current || busy.current) return;
    const values = accepted && current.kind === 'input' ? Array.from(form.current?.querySelectorAll('input') || []).map(input => input.value) : undefined;
    if (values && !values.every(inputWithinLimit)) { values.fill(''); setError('Each field must be at most 4096 bytes.'); return; }
    const reply: Reply = { promptId: current.id, accepted, ...(values ? { values } : {}) };
    busy.current = true; erase();
    try {
      const result = await window.go?.webui?.PrivateBridge?.Reply(reply);
      if (result !== true) setError(unavailable);
    } catch { setError(unavailable); }
    finally { values?.fill(''); if (reply.values) reply.values = []; }
  };
  const copy = async () => {
    const current = identity.current;
    if (!current || busy.current || copying || !['recoveryBackup', 'receive'].includes(current.kind)) return;
    setCopying(true); setCopyStatus('');
    try {
      const ok = await window.go?.webui?.PrivateBridge?.Copy();
      if (identity.current?.id === current.id) setCopyStatus(ok === true ? current.kind === 'receive' ? 'Address copied.' : 'Recovery phrase copied. Clear the clipboard after use.' : 'Could not copy. Try again.');
    } catch { if (identity.current?.id === current.id) setCopyStatus('Could not copy. Try again.'); }
    finally { if (identity.current?.id === current.id) setCopying(false); }
  };
  useEffect(() => {
    let disposed = false; applyAppearance();
    const bridge = window.go?.webui?.PrivateBridge;
    if (!bridge) setError(unavailable);
    else bridge.Prompt().then(value => {
      if (disposed || abandoned.current) return;
      if (!validPrompt(value)) { setError(unavailable); return; }
      applyAppearance(value.appearance); identity.current = { id: value.id, kind: value.kind }; setPrompt(value); document.title = value.title;
    }).catch(() => { if (!disposed) setError(unavailable); });
    const abandon = () => { abandoned.current = true; void finish(false); erase(); };
    const visibility = () => { if (document.visibilityState === 'hidden') abandon(); };
    const stopExport = (event: Event) => { if (['recovery', 'recoveryBackup'].includes(identity.current?.kind || '')) event.preventDefault(); };
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Enter' && !event.repeat && !event.isComposing && identity.current?.kind === 'input' && event.target instanceof HTMLInputElement) { event.preventDefault(); void finish(true); return; }
      if (event.key === 'Escape') { event.preventDefault(); void finish(false); return; }
      if (['recovery', 'recoveryBackup'].includes(identity.current?.kind || '') && (event.ctrlKey || event.metaKey) && ['c', 'x', 'p', 's'].includes(event.key.toLowerCase())) event.preventDefault();
    };
    let lastActivity = 0;
    const activity = () => {
      const now = Date.now();
      if (identity.current && !busy.current && now - lastActivity >= 1000) { lastActivity = now; void bridge?.Activity().catch(() => {}); }
    };
    const revoke = window.runtime?.EventsOn('wallet:revoke', abandon);
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('pagehide', abandon); window.addEventListener('beforeunload', abandon);
    window.addEventListener('keydown', keydown); window.addEventListener('keydown', activity); window.addEventListener('pointerdown', activity);
    for (const name of ['copy', 'cut', 'contextmenu', 'dragstart']) document.addEventListener(name, stopExport);
    return () => {
      disposed = true; abandoned.current = true; erase(); revoke?.();
      document.removeEventListener('visibilitychange', visibility);
      window.removeEventListener('pagehide', abandon); window.removeEventListener('beforeunload', abandon);
      window.removeEventListener('keydown', keydown); window.removeEventListener('keydown', activity); window.removeEventListener('pointerdown', activity);
      for (const name of ['copy', 'cut', 'contextmenu', 'dragstart']) document.removeEventListener(name, stopExport);
    };
  }, []);
  const kind = prompt?.kind;
  const recovery = kind === 'recovery' || kind === 'recoveryBackup';
  const descriptions: Record<Prompt['kind'], string> = { input: 'Enter the requested information to continue.', confirm: 'Review the exact details before you confirm.', notice: 'A notice from your wallet.', receive: 'Verify the network before sending to this address.', recovery: 'Your recovery phrase restores all your wallets.', recoveryBackup: 'Your authenticated recovery phrase backup.' };
  const buttons: Record<Prompt['kind'], string> = { input: 'Continue', confirm: 'Confirm', notice: 'Close', receive: 'Done', recovery: 'I wrote it down', recoveryBackup: 'Done' };
  return <form className={`private-shell ${kind === 'input' ? 'private-input' : ''} ${recovery ? 'private-recovery' : ''}`} ref={form} onSubmit={event => { event.preventDefault(); void finish(true); }}><header className="private-header"><div className="private-brand"><Mark/>CryptoWallet<span>PRIVATE REQUEST</span></div><h1>{prompt?.title || (error ? 'Wallet request closed' : 'Opening wallet request…')}</h1>{kind && <p>{descriptions[kind]}</p>}</header>{prompt ? <PromptContents prompt={prompt} error={error} copyStatus={copyStatus} copying={copying} copy={() => void copy()}/> : <div className="private-body">{error && <p className="private-error" role="alert">{error}</p>}</div>}<footer className="private-footer">{prompt && prompt.kind !== 'notice' && <button type="button" onClick={() => void finish(false)} disabled={busy.current}>Cancel</button>}<button type="submit" className="primary" disabled={!prompt || busy.current}>{kind ? buttons[kind] : 'Continue'}</button></footer></form>;
}
