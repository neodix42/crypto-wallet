'use strict';
const $ = id => document.getElementById(id);
let accounts = [], networks = [], prepared, viewEpoch = 0, sessionEpoch = 0, vaultMode = 'checking';
const show = (id, value) => { $(id).textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2); };
const gates = {
  checking: ['Checking your local wallet', 'Reading the local vault status. No password dialog opens until you choose an action.'],
  missing: ['Create your app password', 'Choose your password in the native Go window. The signer generates one BIP39 root and derives Main account (index 0) across every supported network. Your generated phrase stays inside the signer.'],
  present: ['Welcome back', 'Your encrypted wallet is already on this device. Enter its app password in the native Go window to unlock your accounts.'],
  unavailable: ['Wallet storage unavailable', 'The local vault could not be inspected safely. Check storage access and retry. Creation and restoration remain unavailable while its state is uncertain.']
};
function renderGate() {
  const ready = vaultMode === 'ready', missing = vaultMode === 'missing', present = vaultMode === 'present';
  $('onboarding').hidden = ready; $('walletcontent').hidden = !ready;
  $('emptyactions').hidden = !missing; $('walletactions').hidden = !ready;
  $('unlock').hidden = !present; $('lock').disabled = !ready;
  $('onboardcreate').hidden = !missing; $('onboardunlock').hidden = !present;
  $('onboardretry').hidden = vaultMode !== 'unavailable';
  if (ready) return;
  const [title, description] = gates[vaultMode];
  show('onboardtitle', title); show('onboarddescription', description);
}
function clearLocked() {
  viewEpoch++; sessionEpoch++;
  if (vaultMode === 'ready') vaultMode = 'present';
  accounts = []; networks = []; prepared = undefined;
  $('account').replaceChildren(); $('asset').replaceChildren(); $('operations').replaceChildren(); $('accountgroups').replaceChildren();
  $('backupcallout').hidden = true; show('backupstatus', 'Locked');
  $('qr').hidden = true; $('qr').removeAttribute('src'); $('send').disabled = true;
  for (const id of ['recipient', 'amount', 'memo']) $(id).value = '';
  for (const id of ['address', 'diagnostics', 'preview', 'balance', 'chainhistoryresult']) show(id, 'Locked');
  $('message').textContent = vaultMode === 'missing' ? 'Start by choosing your app password in the native Go window.' : vaultMode === 'unavailable' ? 'Wallet storage unavailable. Retry the storage check before continuing.' : 'Wallet locked. Unlock through the native Go window.';
  renderGate();
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
    if (vaultMode === 'checking') { vaultMode = 'unavailable'; renderGate(); }
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
  renderAccountGroups(visible);
  select();
}
function renderAccountGroups(visible) {
  $('accountgroups').replaceChildren();
  const indices = [...new Set(visible.map(account => account.index))].sort((a, b) => a - b);
  for (const index of indices) {
    const group = document.createElement('section'), heading = document.createElement('h2'), grid = document.createElement('div');
    group.className = 'accountgroup'; grid.className = 'walletgrid';
    heading.textContent = index === 0 ? 'Main account' : `Account ${index + 1}`;
    const detail = document.createElement('p'); detail.textContent = `Derivation index ${index} · Wallets from your shared BIP39 root`;
    group.append(heading, detail, grid);
    for (const account of visible.filter(account => account.index === index)) {
      const network = networks.find(network => network.id === account.network);
      const card = document.createElement('article'), name = document.createElement('h3'), address = document.createElement('p'), assets = document.createElement('p'), open = document.createElement('button');
      card.className = 'walletcard'; card.dataset.accountId = account.id;
      name.textContent = network?.name || account.network;
      address.textContent = account.address; address.className = 'walletaddress';
      assets.textContent = (network?.assets || []).map(asset => asset.symbol).join(' · ') || 'Assets unavailable';
      open.textContent = account.hidden ? 'Open hidden wallet' : 'Open wallet';
      open.onclick = () => { $('account').value = account.id; select(); $('account').focus(); };
      card.append(name, assets, address, open); grid.append(card);
    }
    $('accountgroups').append(group);
  }
}
async function refresh() {
  const epoch = viewEpoch, selected = $('account').value;
  const state = await window.wallet.accounts();
  if (epoch !== viewEpoch) return;
  if (state.environment === 'testnet' || state.environment === 'mainnet') {
    $('profile').textContent = state.environment === 'testnet' ? 'Testnet profile' : 'Mainnet profile';
    $('profile').dataset.environment = state.environment;
  }
  vaultMode = state.vaultState === 'missing' && state.walletExists === false ? 'missing' : state.vaultState === 'present' && state.walletExists === true ? 'present' : 'unavailable';
  if (state.locked !== false || vaultMode !== 'present') { clearLocked(); return; }
  const availableNetworks = await window.wallet.networks();
  if (epoch !== viewEpoch) return;
  accounts = state.accounts || [];
  networks = availableNetworks;
  vaultMode = 'ready'; renderGate();
  $('backupcallout').hidden = state.backupVerified === true;
  show('backupstatus', state.backupVerified === true ? 'Encrypted backup verified' : 'Encrypted backup verification required');
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
const credentialActions = [
  ['unlock', 'unlock', {}], ['create', 'create', { words: 12 }], ['create24', 'create', { words: 24 }],
  ['import', 'import', { words: 12 }], ['backup', 'backup', {}], ['restore', 'restore', {}], ['password', 'password', {}],
  ['onboardcreate', 'create', { words: 12 }], ['onboardunlock', 'unlock', {}], ['backupnow', 'backup', {}]
];
let credentialPending = false;
for (const [id, method, body] of credentialActions) $(id).onclick = async () => {
  if (credentialPending) return;
  credentialPending = true;
  for (const [button] of credentialActions) $(button).disabled = true;
  try { await run(() => result(method, body, refresh)); }
  finally { credentialPending = false; for (const [button] of credentialActions) $(button).disabled = false; }
};
$('release-link').onclick = event => { event.preventDefault(); $('release-status').open = true; $('release-status').querySelector('summary').focus(); };
$('onboardretry').onclick = () => run(refresh);
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
renderGate();
run(refresh);
