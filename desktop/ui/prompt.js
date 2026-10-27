'use strict';
// This window has its own sandbox and preload. The ordinary wallet renderer
// cannot open it, read its contents, or supply its authoritative prompt data.
const $ = id => document.getElementById(id);
const kinds = new Set(['input', 'confirm', 'notice', 'receive', 'recovery', 'recoveryBackup']);
const recoveryKinds = new Set(['recovery', 'recoveryBackup']);
let current, pending = false, abandoned = false;
const unavailable = 'This request is no longer available. Return to your wallet and try again.';
function text(value, limit) { return typeof value === 'string' && value.length > 0 && value.length <= limit; }
function summary(value) { return typeof value === 'string' && value.length <= 65536; }
function valid(prompt) {
  if (!prompt || typeof prompt !== 'object' || Array.isArray(prompt) || typeof prompt.id !== 'string' || !/^[0-9a-f]{32}$/.test(prompt.id) || !kinds.has(prompt.kind) || !text(prompt.title, 256)) return false;
  if (prompt.kind === 'input') return Array.isArray(prompt.labels) && prompt.labels.length > 0 && prompt.labels.length <= 8 && prompt.labels.every(label => text(label, 256));
  if (recoveryKinds.has(prompt.kind)) return Object.keys(prompt).length === 4 && text(prompt.phrase, 512) && /^[a-z]+(?: [a-z]+)*$/.test(prompt.phrase) && [12, 15, 18, 21, 24].includes(prompt.phrase.split(' ').length);
  if (prompt.kind === 'receive') return summary(prompt.summary) && text(prompt.address, 512) && text(prompt.png, 350000) && /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(prompt.png);
  return summary(prompt.summary);
}
function erase() {
  for (const input of document.querySelectorAll('input')) input.value = '';
  for (const word of $('recoverywords').children) word.textContent = '';
  $('inputfields').replaceChildren(); $('recoverywords').replaceChildren();
  $('summary').textContent = ''; $('receivedomain').textContent = ''; $('receiveaddress').textContent = ''; $('receiveqr').removeAttribute('src');
  $('copystatus').textContent = ''; $('description').textContent = ''; $('recoverynote').textContent = '';
  $('prompttitle').textContent = 'Wallet request closed';
  for (const id of ['inputfields', 'summary', 'recovery', 'receive']) $(id).hidden = true;
  $('accept').disabled = true; $('cancel').disabled = true; $('copyaddress').disabled = true;
  delete $('promptform').dataset.kind;
  current = undefined;
}
function failed() {
  erase(); $('prompterror').textContent = unavailable; $('prompterror').hidden = false; $('prompterror').focus();
}
async function finish(accepted) {
  if (!current || pending) return;
  const prompt = current;
  const values = accepted && prompt.kind === 'input' ? [...$('inputfields').querySelectorAll('input')].map(input => input.value) : undefined;
  const reply = { promptId: prompt.id, accepted, ...(values ? { values } : {}) };
  pending = true;
  // Clear before crossing the bridge, including when the request fails or its
  // frame is revoked. JavaScript/Chromium memory copies cannot be zeroed here.
  erase();
  try { await window.walletPrompt.respond(reply); }
  catch { failed(); }
  finally { if (values) values.fill(''); }
}
function render(prompt) {
  if (abandoned) return;
  if (!valid(prompt)) { failed(); return; }
  $('promptform').dataset.kind = prompt.kind;
  current = { id: prompt.id, kind: prompt.kind };
  $('prompttitle').textContent = prompt.title;
  $('accept').disabled = false; $('cancel').disabled = false;
  $('accept').textContent = { input: 'Continue', confirm: 'Confirm', notice: 'Close', receive: 'Done', recovery: 'I wrote it down', recoveryBackup: 'Done' }[prompt.kind];
  $('cancel').hidden = prompt.kind === 'notice';
  if (prompt.kind === 'input') {
    $('description').textContent = 'Enter the requested information to continue.';
    $('inputfields').hidden = false;
    for (const [index, label] of prompt.labels.entries()) {
      const wrapper = document.createElement('label'), input = document.createElement('input');
      wrapper.textContent = label; input.id = 'promptinput' + index; input.type = 'password';
      input.autocomplete = 'off'; input.spellcheck = false; input.setAttribute('autocorrect', 'off');
      input.setAttribute('autocapitalize', 'none'); input.setAttribute('data-lpignore', 'true');
      wrapper.append(input); $('inputfields').append(wrapper);
    }
    $('promptinput0').focus();
  } else if (recoveryKinds.has(prompt.kind)) {
    $('description').textContent = 'Your recovery phrase restores your wallets on another device.';
    $('recovery').hidden = false;
    $('recoverynote').textContent = (prompt.kind === 'recovery' ? 'Next, confirm a few words from your written copy. ' : 'This is a manual backup of the same phrase for all your wallets. ') + 'Never share your phrase or enter it on a website. If you used an additional BIP39 passphrase, keep that separately too.';
    const words = prompt.phrase.split(' ');
    for (const word of words) { const item = document.createElement('li'); item.textContent = word; $('recoverywords').append(item); }
    words.fill(''); $('cancel').focus();
  } else {
    $('description').textContent = prompt.kind === 'confirm' ? 'Review these details before you confirm.' : prompt.kind === 'receive' ? 'Check the network and address before receiving funds.' : 'Review this wallet notice.';
    $('summary').textContent = prompt.summary; $('summary').hidden = false;
    if (prompt.kind === 'receive') {
      $('receivedomain').textContent = prompt.summary.split('\n')[0];
      $('receive').hidden = false; $('receiveaddress').textContent = prompt.address;
      $('receiveqr').src = 'data:image/png;base64,' + prompt.png;
    }
    (prompt.kind === 'notice' ? $('accept') : $('cancel')).focus();
  }
}
$('promptform').onsubmit = event => { event.preventDefault(); void finish(true); };
$('cancel').onclick = () => { void finish(false); };
$('copyaddress').onclick = async () => {
  if (!current || current.kind !== 'receive' || pending || $('copyaddress').disabled) return;
  const id = current.id;
  $('copyaddress').disabled = true;
  try { await window.walletPrompt.copyAddress(); if (current?.id === id) $('copystatus').textContent = 'Address copied'; }
  catch { if (current?.id === id) $('copystatus').textContent = 'Address could not be copied. Try again.'; }
  finally { if (current?.id === id) $('copyaddress').disabled = false; }
};
document.addEventListener('keydown', event => {
  if (event.key === 'Escape') { event.preventDefault(); void finish(false); return; }
  if (event.key === 'Tab') {
    const focusable = [...document.querySelectorAll('input,button,code[tabindex]')].filter(element => !element.disabled && !element.closest('[hidden]'));
    const first = focusable[0], last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
  if (recoveryKinds.has(current?.kind) && ((event.ctrlKey || event.metaKey) && ['c', 'x', 'p', 's'].includes(event.key.toLowerCase()))) event.preventDefault();
});
for (const name of ['copy', 'cut', 'contextmenu', 'dragstart']) document.addEventListener(name, event => { if (recoveryKinds.has(current?.kind)) event.preventDefault(); });
function abandon() { abandoned = true; void finish(false); erase(); }
document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') abandon(); });
window.addEventListener('pagehide', abandon);
window.addEventListener('beforeunload', abandon);
window.walletPrompt.getPrompt().then(render).catch(failed);
