import { useEffect, useRef, useState } from 'react';
import type { Prompt, Reply } from './types';
import { applyAppearance, inputWithinLimit, phraseWords, validPrompt } from './protocol';
import { CopyIcon } from './icons';

const unavailable = 'The wallet dialog could not be completed. Try again.';
const restartRequired = 'The wallet dialog could not be recovered. Restart the app and try again.';

export function PromptContents({ prompt, error, copyStatus, copying, copy }: { prompt: Prompt; error: string; copyStatus: string; copying: boolean; copy: () => void }) {
  const recovery = prompt.kind === 'recovery' || prompt.kind === 'recoveryBackup';
  return <div className="dialog-body">{prompt.kind === 'input' && <div className="dialog-fields">{(prompt.labels || []).map((label, index) => <label className="field" key={index}><span>{label}</span><input name={`dialog-${index}`} type="password" autoComplete="off" autoCorrect="off" autoCapitalize="none" spellCheck={false} maxLength={4096}/></label>)}</div>}{recovery && <><p className="recovery-warning">Keep these words in order. Anyone with your recovery phrase can access your wallets.</p><ol className="recovery-words">{phraseWords(prompt).map((word, index) => <li key={index} data-recovery-word><span className="word-number">{index + 1}</span><span>{word}</span></li>)}</ol><p className="dialog-note">{prompt.kind === 'recovery' ? 'Next, confirm a few words from your saved copy.' : 'This is the same recovery phrase for all your wallets.'} Keep any additional BIP39 passphrase separately.</p><div className="phrase-copy"><button type="button" disabled={copying} onClick={copy}><CopyIcon/>Copy phrase</button><p className="dialog-note">Other apps and clipboard history may retain copied words. Clear the clipboard after use; existing copies may remain.</p><p className="copy-feedback" role="status">{copyStatus}</p></div></>}{prompt.kind === 'receive' && <><div className="qr-frame"><img draggable={false} src={`data:image/png;base64,${prompt.png}`} alt="Receive address QR code"/></div><div className="receive-address"><code tabIndex={0}>{prompt.address}</code><button type="button" onClick={copy} disabled={copying}><CopyIcon/>Copy address</button></div><p className="copy-feedback" role="status">{copyStatus}</p></>}{(prompt.kind === 'receive' || prompt.kind === 'confirm' || prompt.kind === 'notice') && <pre className="prompt-summary">{prompt.summary}</pre>}{error && <p className="dialog-error" role="alert">{error}</p>}</div>;
}

export function DialogHost({ onError }: { onError?: (message: string) => void } = {}) {
  const [prompt, setPrompt] = useState<Prompt | null>(null), [error, setError] = useState('');
  const [copying, setCopying] = useState(false), [copyStatus, setCopyStatus] = useState('');
  const current = useRef<Prompt | null>(null), epoch = useRef(0), replying = useRef<string | null>(null), copyPending = useRef(false);
  const form = useRef<HTMLFormElement>(null), errorHandler = useRef(onError);
  errorHandler.current = onError;
  const eraseDOM = (container = form.current) => {
    container?.querySelectorAll('input').forEach(input => { input.value = ''; });
    container?.querySelectorAll('[data-recovery-word]').forEach(word => { word.textContent = ''; });
    container?.querySelectorAll('img').forEach(image => image.removeAttribute('src'));
  };
  const clear = () => {
    eraseDOM(); current.current = null; epoch.current++;
    copyPending.current = false; setPrompt(null); setCopying(false); setCopyStatus(''); setError('');
  };
  const reportError = (message = unavailable) => {
    if (errorHandler.current) errorHandler.current(message);
    else setError(message);
  };
  const finish = async (accepted: boolean) => {
    const active = current.current;
    if (!active || replying.current === active.id) return;
    const values = accepted && active.kind === 'input' ? Array.from(form.current?.querySelectorAll('input') || []).map(input => input.value) : undefined;
    if (values && !values.every(inputWithinLimit)) { values.fill(''); setError('Each field must be at most 4096 bytes.'); return; }
    const reply: Reply = { promptId: active.id, accepted, ...(values ? { values } : {}) };
    replying.current = active.id; clear(); const sentEpoch = epoch.current;
    let failed = false;
    try {
      const ok = await window.go?.webui?.DialogBridge?.Reply(reply);
      failed = ok !== true;
    } catch { failed = true; }
    finally {
      values?.fill(''); if (reply.values) reply.values = [];
      if (replying.current === active.id) replying.current = null;
    }
    if (!failed || epoch.current !== sentEpoch) return;
    try {
      const latest = await window.go?.webui?.DialogBridge?.Snapshot();
      if (epoch.current !== sentEpoch) return;
      if (latest === null) { reportError(); return; }
      if (!validPrompt(latest)) { reportError(restartRequired); return; }
      epoch.current++; current.current = latest; copyPending.current = false;
      setPrompt(latest); setCopying(false); setCopyStatus(''); setError('The previous reply was not accepted. Review and try again.');
      if (latest.appearance) applyAppearance(latest.appearance);
    } catch { if (epoch.current === sentEpoch) reportError(restartRequired); }
  };
  const copy = async () => {
    const active = current.current, copiedEpoch = epoch.current;
    if (!active || copyPending.current || !['receive', 'recovery', 'recoveryBackup'].includes(active.kind)) return;
    copyPending.current = true; setCopying(true); setCopyStatus('');
    try {
      const ok = await window.go?.webui?.DialogBridge?.Copy(active.id);
      if (epoch.current === copiedEpoch && current.current?.id === active.id) setCopyStatus(ok === true ? active.kind === 'receive' ? 'Address copied.' : 'Recovery phrase copied.' : 'Could not copy. Try again.');
    } catch { if (epoch.current === copiedEpoch && current.current?.id === active.id) setCopyStatus('Could not copy. Try again.'); }
    finally {
      if (epoch.current === copiedEpoch) { copyPending.current = false; setCopying(false); }
    }
  };
  useEffect(() => {
    let disposed = false, pushed = false;
    const accept = (value: unknown) => {
      if (disposed) return;
      if (value === null) { clear(); return; }
      if (!validPrompt(value)) { clear(); reportError(); return; }
      if (current.current?.id === value.id && current.current.kind === value.kind) return;
      eraseDOM(); epoch.current++; current.current = value; copyPending.current = false;
      setPrompt(value); setError(''); setCopying(false); setCopyStatus('');
      if (value.appearance) applyAppearance(value.appearance);
    };
    const bridge = window.go?.webui?.DialogBridge;
    const unsubscribe = window.runtime?.EventsOn('wallet:dialog', value => { pushed = true; accept(value); });
    bridge?.Snapshot().then(value => { if (!pushed) accept(value); }).catch(() => { if (!disposed && !pushed) reportError(); });
    let lastActivity = 0;
    const activity = () => {
      const now = Date.now();
      if (current.current && now - lastActivity >= 1000) { lastActivity = now; void bridge?.Activity(current.current.id).catch(() => {}); }
    };
    window.addEventListener('pointerdown', activity); window.addEventListener('keydown', activity);
    return () => {
      disposed = true; eraseDOM(); current.current = null; epoch.current++;
      unsubscribe?.(); window.removeEventListener('pointerdown', activity); window.removeEventListener('keydown', activity);
    };
  }, []);
  useEffect(() => {
    if (!prompt) return;
    const content = form.current;
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const shell = document.querySelector<HTMLElement>('.wallet-shell');
    const previousInert = shell?.inert || false, previousHidden = shell?.getAttribute('aria-hidden');
    if (shell) { shell.inert = true; shell.setAttribute('aria-hidden', 'true'); }
    const first = () => form.current?.querySelector<HTMLElement>('input, button.primary, button, [tabindex="0"]');
    first()?.focus();
    const focus = (event: FocusEvent) => {
      if (current.current && event.target instanceof Node && !form.current?.contains(event.target)) first()?.focus();
    };
    document.addEventListener('focusin', focus);
    return () => {
      document.removeEventListener('focusin', focus); eraseDOM(content);
      if (shell) { shell.inert = previousInert; if (previousHidden === null || previousHidden === undefined) shell.removeAttribute('aria-hidden'); else shell.setAttribute('aria-hidden', previousHidden); }
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [prompt?.id]);
  if (!prompt) return error && !onError ? <p className="dialog-status-error" role="alert">{error}</p> : null;
  const recovery = prompt.kind === 'recovery' || prompt.kind === 'recoveryBackup';
  const descriptions: Record<Prompt['kind'], string> = { input: 'Enter the requested information to continue.', confirm: 'Review the details before you confirm.', notice: '', receive: 'Verify the network before sending to this address.', recovery: 'Your recovery phrase restores all your wallets.', recoveryBackup: 'Your recovery phrase backup.' };
  const buttons: Record<Prompt['kind'], string> = { input: /^unlock\b/i.test(prompt.title) ? 'Unlock' : 'Continue', confirm: 'Confirm', notice: 'Close', receive: 'Done', recovery: 'I wrote it down', recoveryBackup: 'Done' };
  return <div className="dialog-backdrop"><form key={prompt.id} className={`dialog-panel ${prompt.kind === 'input' ? 'dialog-input' : ''} ${recovery ? 'dialog-recovery' : ''} ${prompt.kind === 'receive' ? 'dialog-receive' : ''}`} role="dialog" aria-modal="true" aria-labelledby="wallet-dialog-title" ref={form} onSubmit={event => { event.preventDefault(); void finish(true); }} onKeyDown={event => {
    if (event.key === 'Escape') { event.preventDefault(); void finish(false); return; }
    if (event.key === 'Enter' && !event.repeat && !event.nativeEvent.isComposing && event.target instanceof HTMLInputElement) { event.preventDefault(); void finish(true); return; }
    if (event.key === 'Tab') {
      const controls = Array.from(form.current?.querySelectorAll<HTMLElement>('input:not(:disabled), button:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]') || []);
      const first = controls[0], last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }
  }}><header className="dialog-header"><h1 id="wallet-dialog-title">{prompt.title}</h1>{descriptions[prompt.kind] && <p>{descriptions[prompt.kind]}</p>}</header><PromptContents prompt={prompt} error={error} copyStatus={copyStatus} copying={copying} copy={() => void copy()}/><footer className="dialog-footer">{prompt.kind !== 'notice' && <button type="button" onClick={() => void finish(false)}>Cancel</button>}<button type="submit" className="primary">{buttons[prompt.kind]}</button></footer></form></div>;
}
