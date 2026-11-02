import { useEffect, useRef } from 'react';
import type { ErrorNotice } from './types';

export function ErrorDialog({ notice, close }: { notice: ErrorNotice; close: (id: string) => void }) {
  const panel = useRef<HTMLFormElement>(null);
  useEffect(() => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const shell = document.querySelector<HTMLElement>('.wallet-shell');
    const previousInert = shell?.inert || false, previousHidden = shell?.getAttribute('aria-hidden');
    if (shell) { shell.inert = true; shell.setAttribute('aria-hidden', 'true'); }
    const first = () => panel.current?.querySelector<HTMLButtonElement>('button');
    first()?.focus();
    const focus = (event: FocusEvent) => { if (event.target instanceof Node && !panel.current?.contains(event.target)) first()?.focus(); };
    document.addEventListener('focusin', focus);
    return () => {
      document.removeEventListener('focusin', focus);
      if (shell) { shell.inert = previousInert; if (previousHidden === null || previousHidden === undefined) shell.removeAttribute('aria-hidden'); else shell.setAttribute('aria-hidden', previousHidden); }
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [notice.ID]);
  return <div className="dialog-backdrop"><form className="dialog-panel dialog-error-notice" role="alertdialog" aria-modal="true" aria-labelledby="wallet-error-title" aria-describedby="wallet-error-message" ref={panel} onSubmit={event => { event.preventDefault(); close(notice.ID); }} onKeyDown={event => {
    if (event.key === 'Escape' || event.key === 'Enter') { event.preventDefault(); if (!event.repeat && !event.nativeEvent.isComposing) close(notice.ID); }
    if (event.key === 'Tab') { event.preventDefault(); panel.current?.querySelector<HTMLButtonElement>('button')?.focus(); }
  }}><header className="dialog-header"><h1 id="wallet-error-title">{notice.Title}</h1></header><div className="dialog-body"><dl className="error-details"><dt>Operation</dt><dd>{notice.Operation || 'Wallet operation'}</dd><dt>Code</dt><dd>{notice.Code}</dd><dt>Reason</dt><dd id="wallet-error-message">{notice.Message}</dd></dl></div><footer className="dialog-footer"><button type="submit" className="primary">Close</button></footer></form></div>;
}
