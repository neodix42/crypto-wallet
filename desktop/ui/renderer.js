'use strict';
const $ = id => document.getElementById(id);
let accounts = [], networks = [], prepared, viewEpoch = 0, sessionEpoch = 0;
const show = (id, value) => { $(id).textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2); };
function clearLocked() {
  viewEpoch++; sessionEpoch++;
  accounts = []; networks = []; prepared = undefined;
  $('account').replaceChildren(); $('asset').replaceChildren(); $('operations').replaceChildren();
  $('qr').hidden = true; $('qr').removeAttribute('src'); $('send').disabled = true;
  for (const id of ['recipient', 'amount', 'memo']) $(id).value = '';
  for (const id of ['address', 'diagnostics', 'preview', 'balance', 'chainhistoryresult']) show(id, 'Locked');
  $('message').textContent = 'Wallet locked. Unlock through the native Go window.';
}
const removeLockListener = window.wallet.onLocked(clearLocked);
window.addEventListener('beforeunload', removeLockListener, { once: true });
window.wallet.getVersion().then(version => { $('version').textContent = 'Version ' + version; }).catch(() => { $('version').textContent = 'Version unavailable'; });
async function run(fn) {
  const epoch = sessionEpoch;
  try {
    $('message').textContent = 'Waiting for Go wallet…';
    await fn();
    if (epoch === sessionEpoch) $('message').textContent = 'Request completed. Check the returned state and any limitations.';
  } catch {
    if (epoch === sessionEpoch) $('message').textContent = 'Request failed, unavailable or cancelled. Balance is unknown until verified; reconcile any pending payment before retrying.';
  }
}
// A native/automatic lock invalidates outstanding public replies as well as the
// visible state. Delayed queries cannot put addresses or a plan back on screen.
async function result(method, body, apply) {
  const epoch = viewEpoch;
  const value = await window.wallet[method](body);
  if (epoch === viewEpoch) await apply(value);
}
function populateAccounts(selected) {
  const visible = accounts.filter(account => !account.hidden || $('showhidden').checked);
  $('account').replaceChildren();
  for (const account of visible) {
    const option = document.createElement('option');
    option.value = account.id;
    option.textContent = `${account.network} · Account ${account.index + 1} · ${account.address}${account.hidden ? ' (hidden)' : ''}`;
    $('account').append(option);
  }
  if (visible.some(account => account.id === selected)) $('account').value = selected;
  select();
}
async function refresh() {
  const epoch = viewEpoch, selected = $('account').value;
  const state = await window.wallet.accounts();
  if (epoch !== viewEpoch) return;
  if (state.locked) { clearLocked(); return; }
  const availableNetworks = await window.wallet.networks();
  if (epoch !== viewEpoch) return;
  accounts = state.accounts || [];
  networks = availableNetworks;
  populateAccounts(selected);
  show('diagnostics', state);
  await history();
}
function current() { return accounts.find(account => account.id === $('account').value); }
function select() {
  viewEpoch++;
  const account = current();
  $('asset').replaceChildren();
  show('address', account ? account.address : 'Locked or no account');
  show('balance', 'Balance unknown — query required.');
  show('preview', 'No payment prepared.');
  show('chainhistoryresult', 'History has not been queried.');
  $('qr').hidden = true; $('qr').removeAttribute('src');
  $('hideaccount').textContent = account?.hidden ? 'Unhide selected' : 'Hide selected';
  prepared = undefined; $('send').disabled = true;
  const network = networks.find(network => network.id === account?.network);
  for (const asset of network?.assets || []) {
    const option = document.createElement('option');
    option.value = asset.id;
    option.textContent = `${asset.symbol} · ${asset.id}`;
    $('asset').append(option);
  }
}
async function history() {
  await result('history', {}, operations => {
    $('operations').replaceChildren();
    for (const operation of operations || []) {
      const row = document.createElement('div'), text = document.createElement('pre');
      text.textContent = JSON.stringify(operation, null, 2);
      row.append(text);
      if (['UNKNOWN', 'SUBMITTED', 'SUBMITTING', 'SIGNED_NOT_SUBMITTED', 'REORGED'].includes(operation.state)) {
        const button = document.createElement('button');
        button.textContent = 'Reconcile original payment';
        button.onclick = () => run(() => result('reconcile', { id: operation.plan.id }, async value => { show('preview', value); await history(); }));
        row.append(button);
      }
      if (operation.state === 'PREPARED') {
        const button = document.createElement('button');
        button.textContent = 'Cancel unsigned preparation';
        button.onclick = () => run(() => result('cancelPrepared', { id: operation.plan.id }, async () => { prepared = undefined; $('send').disabled = true; show('preview', 'Unsigned preparation cancelled.'); await history(); }));
        row.append(button);
      }
      $('operations').append(row);
    }
  });
}
for (const [id, method, body] of [
  ['unlock', 'unlock', {}], ['create', 'create', { words: 12 }], ['create24', 'create', { words: 24 }],
  ['import', 'import', { words: 12 }], ['backup', 'backup', {}], ['restore', 'restore', {}], ['password', 'password', {}]
]) $(id).onclick = () => run(() => result(method, body, refresh));
$('lock').onclick = () => { clearLocked(); return run(() => window.wallet.lock()); };
$('add').onclick = () => run(() => result('addAccount', { index: Number($('index').value) }, refresh));
$('hideaccount').onclick = () => run(() => result('hideAccount', { accountId: current()?.id || '', hidden: !current()?.hidden }, refresh));
$('showhidden').onchange = () => populateAccounts($('account').value);
$('discoveraccounts').onclick = () => run(() => result('discoverAccounts', { start: Number($('recoverystart').value), end: Number($('recoveryend').value) }, refresh));
$('account').onchange = select;
$('refresh').onclick = () => run(() => result('balances', { accountId: current()?.id || '' }, value => show('balance', value)));
$('receive').onclick = () => run(() => result('receive', { accountId: current()?.id || '' }, value => {
  if (value.account) { const i = accounts.findIndex(account => account.id === value.account.id); if (i >= 0) accounts[i] = value.account; show('address', value.account.address); }
  $('qr').src = 'data:image/png;base64,' + value.png; $('qr').hidden = false;
}));
function transfer() { return { accountId: current()?.id || '', asset: $('asset').value, recipient: $('recipient').value, amount: $('amount').value, memo: $('memo').value }; }
async function prepare(method) {
  viewEpoch++;
  const body = transfer(); if (method === 'max') body.amount = '0';
  prepared = undefined; $('send').disabled = true;
  await result(method, body, async value => { prepared = value; show('preview', prepared); $('send').disabled = false; await history(); });
}
$('prepare').onclick = () => run(() => prepare('prepare'));
$('max').onclick = () => run(() => prepare('max'));
$('send').onclick = () => run(async () => { if (!prepared) return; const id = prepared.id; prepared = undefined; $('send').disabled = true; await result('send', { id }, async value => { show('preview', value); await history(); }); });
$('history').onclick = () => run(history);
for (const id of ['recipient', 'amount', 'memo', 'asset']) $(id).addEventListener('input', () => { $('send').disabled = true; prepared = undefined; viewEpoch++; });
$('copy').onclick = () => run(() => window.wallet.copyReceive({ accountId: current()?.id || '' }));
$('saveqr').onclick = () => run(() => window.wallet.saveQR({ accountId: current()?.id || '' }));
$('scan').onclick = () => run(() => result('scanQR', {}, value => { viewEpoch++; $('recipient').value = value; prepared = undefined; $('send').disabled = true; }));
$('configure').onclick = () => run(() => window.wallet.configure({ network: current()?.network || '' }));
$('associate').onclick = () => run(() => result('associate', { accountId: current()?.id || '', address: $('recipient').value }, refresh));
$('chainhistory').onclick = () => run(() => result('chainHistory', { accountId: current()?.id || '' }, value => show('chainhistoryresult', value)));
$('discovery').onclick = () => run(() => result('discovery', { accountId: current()?.id || '', birthday: Number($('birthday').value), gap: Number($('gap').value), limit: Number($('limit').value), blockBudget: Number($('blockbudget').value) }, refresh));
run(refresh);
