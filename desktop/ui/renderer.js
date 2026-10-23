'use strict';
const $ = id => document.getElementById(id);
let accounts = [], networks = [], operations = [], balances = [], walletTabs = [], activeWalletID = '', activeIndex = 0;
let prepared, viewEpoch = 0, sessionEpoch = 0, vaultMode = 'checking', mainnetBackupRequired = false;
const show = (id, value) => { $(id).textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2); };
const operationErrors = Object.freeze({
  PASSWORD_POLICY: 'Use an app password of 12–4096 UTF-8 bytes. Enter and repeat it in the native Go window, then try again.',
  INPUT_TOO_LONG: 'An input exceeds 4,096 UTF-8 bytes. Shorten it in the native window.',
  PASSWORD_MISMATCH: 'The passwords did not match. Enter the same app password in both native fields and try again.',
  INVALID_MNEMONIC: 'Enter a valid English BIP39 recovery phrase (12, 15, 18, 21 or 24 words) in the native Go window. A new wallet generates its phrase automatically.',
  BACKUP_DESTINATION_UNSAFE: 'The backup destination is unavailable or unsafe. Choose a writable folder and a new file name. The wallet will not follow a link or overwrite an existing file.',
  BACKUP_EXISTS: 'A file already exists at that backup destination. Choose a new file name; the wallet will not overwrite it.',
  BACKUP_WRITE_FAILED: 'The encrypted backup could not be saved or verified. Check folder permissions and free space, then choose a new file name. Backup verification is still required.',
  BACKUP_VERIFY_FAILED: 'The encrypted backup could not be verified. Keep the wallet open and choose a new file name to create and verify another backup. This backup is not verified for recovery.',
  RESTORE_SOURCE_UNSAFE: 'The selected backup could not be read safely. Choose a regular encrypted backup file you can read; links and unsafe files are refused.',
  REQUEST_CANCELLED: 'Request cancelled. Check the wallet state before retrying.',
  REQUEST_FAILED_STATE_UNKNOWN: 'Request failed or is unavailable. Balance is unknown until verified; reconcile any pending payment before retrying.'
});
function clearOperationError() { $('operationerror').hidden = true; show('operationerror', ''); }
function showOperationError(error) {
  // Electron copies Error.message but drops custom Error properties at the bridge.
  const code = typeof error?.message === 'string' && Object.hasOwn(operationErrors, error.message) ? error.message : 'REQUEST_FAILED_STATE_UNKNOWN';
  const message = operationErrors[code];
  show('message', message); show('operationerror', message); $('operationerror').hidden = false;
  $('operationerror').focus();
}
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
  const retainError = vaultMode !== 'ready' && !$('operationerror').hidden;
  viewEpoch++; sessionEpoch++;
  if (!retainError) clearOperationError();
  if (vaultMode === 'ready') vaultMode = 'present';
  accounts = []; networks = []; operations = []; balances = []; walletTabs = []; activeWalletID = ''; activeIndex = 0; prepared = undefined;
  $('account').replaceChildren(); $('asset').replaceChildren(); $('operations').replaceChildren(); $('accountgroups').replaceChildren(); $('viewtabs').replaceChildren();
  $('walletpage').hidden = true; $('accountoverview').hidden = false;
  for (const id of ['wallettitle', 'walletsubtitle', 'gasbalance', 'connectionstatus']) show(id, '');
  $('address').removeAttribute('title');
  $('backupcallout').hidden = true; show('backupstatus', 'Locked');
  $('qr').hidden = true; $('qr').removeAttribute('src'); $('send').disabled = true;
  for (const id of ['recipient', 'amount', 'memo', 'birthday', 'gap', 'limit', 'blockbudget']) $(id).value = '';
  for (const id of ['address', 'diagnostics', 'preview', 'balance', 'chainhistoryresult']) show(id, 'Locked');
  if (!retainError) $('message').textContent = vaultMode === 'missing' ? 'Start by choosing your app password in the native Go window.' : vaultMode === 'unavailable' ? 'Wallet storage unavailable. Retry the storage check before continuing.' : 'Wallet locked. Unlock through the native Go window.';
  renderGate();
}
const removeLockListener = window.wallet.onLocked(clearLocked);
window.addEventListener('beforeunload', removeLockListener, { once: true });
window.wallet.getVersion().then(version => { $('version').textContent = 'Version ' + version; }).catch(() => { $('version').textContent = 'Version unavailable'; });
async function run(fn, scoped = false) {
  const epoch = sessionEpoch, view = viewEpoch;
  try {
    clearOperationError();
    $('message').textContent = 'Waiting for Go wallet…';
    await fn();
    if (epoch === sessionEpoch && (!scoped || view === viewEpoch)) $('message').textContent = 'Request completed. Check the returned state and any limitations.';
  } catch (error) {
    if (vaultMode === 'checking') { vaultMode = 'unavailable'; renderGate(); }
    if (epoch === sessionEpoch && (!scoped || view === viewEpoch)) showOperationError(error);
  }
}
// A native/automatic lock invalidates outstanding public replies as well as the
// visible state. Delayed queries cannot put addresses or a plan back on screen.
async function result(method, body, apply) {
  const epoch = viewEpoch;
  const value = await window.wallet[method](body);
  if (epoch === viewEpoch) await apply(value);
}
function visibleAccounts() { return accounts.filter(account => !account.hidden || $('showhidden').checked); }
function accountName(index) { return index === 0 ? 'Main account' : `Account ${index + 1}`; }
function populateAccounts(selected) {
  const visible = visibleAccounts();
  $('account').replaceChildren();
  for (const account of visible) {
    const option = document.createElement('option');
    option.value = account.id;
    option.textContent = `${account.network} · Account ${account.index + 1} · ${account.address}${account.hidden ? ' (hidden)' : ''}`;
    $('account').append(option);
  }
  if (visible.some(account => account.id === selected)) $('account').value = selected;
  walletTabs = walletTabs.filter(id => visible.some(account => account.id === id));
  if (!walletTabs.includes(activeWalletID)) activeWalletID = '';
  if (!visible.some(account => account.index === activeIndex)) activeIndex = visible[0]?.index || 0;
  select(); renderAccountGroups(); renderTabs(); renderView();
}
function renderView() {
  $('accountoverview').hidden = !!activeWalletID;
  $('walletpage').hidden = !activeWalletID;
}
function renderTabs() {
  const visible = visibleAccounts();
  $('viewtabs').replaceChildren();
  function tab(text, selected, page, activate) {
    const button = document.createElement('button');
    button.textContent = text; button.setAttribute('role', 'tab'); button.setAttribute('aria-selected', String(selected));
    button.setAttribute('aria-controls', page); button.tabIndex = selected ? 0 : -1;
    button.onclick = activate;
    button.onkeydown = event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      const tabs = [...$('viewtabs').children], index = tabs.indexOf(button);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length;
      tabs[next].click(); $('viewtabs').children[next].focus();
    };
    $('viewtabs').append(button);
  }
  for (const index of [...new Set(visible.map(account => account.index))].sort((a, b) => a - b)) {
    tab(accountName(index), !activeWalletID && activeIndex === index, 'accountoverview', () => {
      activeWalletID = ''; activeIndex = index;
      const first = visible.find(account => account.index === index); $('account').value = first.id;
      select(); renderAccountGroups(); renderTabs(); renderView();
    });
  }
  for (const id of walletTabs) {
    const account = visible.find(account => account.id === id), network = networks.find(network => network.id === account.network);
    tab(`${network?.name || account.network} · ${accountName(account.index)}`, activeWalletID === id, 'walletpage', () => openWallet(id));
  }
}
function copyIcon() {
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  icon.setAttribute('viewBox', '0 0 24 24'); icon.setAttribute('aria-hidden', 'true');
  for (const [x, y] of [[8, 8], [3, 3]]) {
    const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    for (const [key, value] of Object.entries({x, y, width: 13, height: 13, rx: 2})) rect.setAttribute(key, String(value));
    icon.append(rect);
  }
  return icon;
}
function renderAccountGroups() {
  $('accountgroups').replaceChildren();
  const visible = visibleAccounts().filter(account => account.index === activeIndex);
  if (!visible.length) return;
  const group = document.createElement('section'), heading = document.createElement('h2'), grid = document.createElement('div');
  group.className = 'accountgroup'; grid.className = 'walletgrid';
  heading.textContent = accountName(activeIndex);
  const detail = document.createElement('p'); detail.textContent = `Derivation index ${activeIndex} · Wallets from your shared BIP39 root`;
  group.append(heading, detail, grid);
  for (const account of visible) {
    const network = networks.find(network => network.id === account.network);
    const card = document.createElement('article'), name = document.createElement('h3'), address = document.createElement('p'), assets = document.createElement('p'), open = document.createElement('button');
    const addressRow = document.createElement('div'), copy = document.createElement('button');
    card.className = 'walletcard'; card.dataset.accountId = account.id;
    name.textContent = network?.name || account.network;
    address.textContent = account.address; address.title = account.address; address.className = 'walletaddress';
    addressRow.className = 'addressrow'; copy.className = 'copyaddress'; copy.title = 'Copy verified address';
    copy.setAttribute('aria-label', `Copy verified address for ${network?.name || account.network}, ${accountName(account.index)}`);
    copy.append(copyIcon()); copy.disabled = mainnetBackupRequired;
    if (mainnetBackupRequired) copy.title = 'Verify encrypted backup first';
    // Only the account ID crosses IPC. Go independently derives and confirms the
    // address; neither DOM text nor a renderer-supplied address reaches clipboard.
    copy.onclick = () => run(() => addressAction('copyReceive', account.id), true);
    addressRow.append(address, copy);
    assets.textContent = (network?.assets || []).map(asset => asset.symbol).join(' · ') || 'Assets unavailable';
    open.textContent = account.hidden ? 'Open hidden wallet' : 'Open wallet'; open.className = 'openwallet';
    open.onclick = () => openWallet(account.id);
    card.append(name, assets, addressRow, open); grid.append(card);
  }
  $('accountgroups').append(group);
}
function openWallet(id) {
  const account = visibleAccounts().find(account => account.id === id);
  if (!account) return;
  activeWalletID = id; activeIndex = account.index; $('account').value = id;
  if (!walletTabs.includes(id)) walletTabs.push(id);
  select(); renderTabs(); renderView(); $('wallettitle').focus();
  return run(async () => { await Promise.all([loadBalance(), history()]); }, true);
}
async function refresh() {
  const epoch = sessionEpoch;
  const state = await window.wallet.accounts();
  if (epoch !== sessionEpoch) return;
  if (state.environment === 'testnet' || state.environment === 'mainnet') {
    $('profile').textContent = state.environment === 'testnet' ? 'Testnet profile' : 'Mainnet profile';
    $('profile').dataset.environment = state.environment;
  }
  vaultMode = state.vaultState === 'missing' && state.walletExists === false ? 'missing' : state.vaultState === 'present' && state.walletExists === true ? 'present' : 'unavailable';
  if (state.locked !== false || vaultMode !== 'present') { clearLocked(); return; }
  const availableNetworks = await window.wallet.networks();
  if (epoch !== sessionEpoch) return;
  accounts = state.accounts || [];
  networks = availableNetworks;
  vaultMode = 'ready'; mainnetBackupRequired = state.environment === 'mainnet' && state.backupVerified !== true; renderGate();
  $('walletbackupnotice').hidden = !mainnetBackupRequired;
  for (const id of ['receive', 'copy', 'saveqr', 'sendview', 'prepare', 'max']) $(id).disabled = mainnetBackupRequired;
  $('backupcallout').hidden = state.backupVerified === true;
  show('backupstatus', state.backupVerified === true ? 'Encrypted backup verified' : 'Encrypted backup verification required');
  populateAccounts($('account').value);
  if (activeWalletID) await Promise.all([loadBalance(), history()]);
}
function current() { return accounts.find(account => account.id === $('account').value); }
function select() {
  viewEpoch++;
  const account = current();
  $('asset').replaceChildren();
  show('address', account ? account.address : 'Locked or no account');
  $('address').title = account?.address || '';
  balances = []; operations = [];
  show('balance', 'Balance unknown — query required.'); $('gasbalance').hidden = true;
  show('preview', 'No payment prepared.');
  show('chainhistoryresult', 'History has not been queried.'); $('operations').replaceChildren();
  for (const id of ['recipient', 'amount', 'memo']) $(id).value = '';
  $('qr').hidden = true; $('qr').removeAttribute('src');
  $('hideaccount').textContent = account?.hidden ? 'Unhide selected' : 'Hide selected';
  prepared = undefined; $('send').disabled = true;
  const network = networks.find(network => network.id === account?.network);
  show('wallettitle', network?.name || account?.network || 'Network wallet');
  show('walletsubtitle', account ? `${accountName(account.index)} · ${account.network}` : 'No account selected');
  show('connectionstatus', network?.family === 'decred' && !network.rpc ? 'Connection required — configure an authenticated TLS dcrd node before querying or sending.' : network ? `${network.rpc ? 'RPC: ' + network.rpc + ' · ' : ''}${network.trust || 'Provider verification required.'}` : 'Connection details unavailable.');
  loadDiscovery(network?.family === 'decred' ? account : null);
  $('decrednote').hidden = network?.family !== 'decred'; $('dcrdiscoverycontrols').hidden = network?.family !== 'decred';
  $('associate').hidden = !['near', 'aptos'].includes(network?.family);
  show('diagnostics', { account: account || null, network: network || null });
  for (const asset of network?.assets || []) {
    const option = document.createElement('option');
    option.value = asset.id; option.textContent = `${asset.symbol} · ${asset.id}`; $('asset').append(option);
  }
}
function loadDiscovery(account) {
  const parameters = account?.parameters;
  for (const [id, key, minimum, maximum, fallback] of [
    ['birthday', 'birthdayHeight', 0, 4294967295, 0],
    ['gap', 'gapLimit', 1, 100, 20],
    ['limit', 'scanLimit', 1, 10000, 1000],
    ['blockbudget', 'blockBudget', 0, 2000000, 0]
  ]) {
    const value = parameters?.[key];
    $(id).value = Number.isInteger(value) && value >= minimum && value <= maximum ? String(value) : String(fallback);
  }
}
function balanceText(asset, balance) {
  if (!balance || typeof balance.atomic !== 'string' || !/^(0|[1-9][0-9]*)$/.test(balance.atomic)) return 'Balance unknown — not returned by the network.';
  const padded = balance.atomic.padStart(asset.decimals + 1, '0');
  const value = asset.decimals ? padded.slice(0, -asset.decimals) + '.' + padded.slice(-asset.decimals) : padded;
  return `${value.replace(/(\.[0-9]*?)0+$/, '$1').replace(/\.$/, '')} ${asset.symbol} · ${balance.state} · ${balance.at}`;
}
function renderBalance() {
  const network = networks.find(network => network.id === current()?.network), asset = network?.assets.find(asset => asset.id === $('asset').value);
  show('balance', asset ? balanceText(asset, balances.find(balance => balance.asset === asset.id)) : 'Balance unknown — no asset selected.');
  const gas = network?.assets.find(item => item.id.endsWith(':native'));
  $('gasbalance').hidden = !asset?.contract || !gas;
  if (asset?.contract && gas) show('gasbalance', `Gas balance: ${balanceText(gas, balances.find(balance => balance.asset === gas.id))}`);
}
async function loadBalance() {
  const epoch = viewEpoch;
  balances = []; show('balance', 'Balance unknown — querying the selected network…'); $('gasbalance').hidden = true;
  try { await result('balances', { accountId: current()?.id || '' }, value => { balances = Array.isArray(value) ? value : []; renderBalance(); }); }
  catch (error) { if (epoch === viewEpoch) show('balance', 'Balance unavailable — RPC query failed. No balance has been verified.'); throw error; }
}
async function history() {
  await result('history', {}, value => { operations = value || []; renderHistory(); });
}
function renderHistory() {
  $('operations').replaceChildren();
  const account = current();
  if (!account) return;
  // A journal can include every derived account. Never show or reconcile an
  // operation from another wallet or another selected asset on this page.
  for (const operation of operations.filter(operation => operation?.plan?.account?.id === account.id && operation.plan.account.network === account.network && operation.plan.asset === $('asset').value)) {
    const row = document.createElement('div'), text = document.createElement('pre');
    text.textContent = JSON.stringify(operation, null, 2); row.append(text);
    if (['UNKNOWN', 'SUBMITTED', 'SUBMITTING', 'SIGNED_NOT_SUBMITTED', 'REORGED'].includes(operation.state)) {
      const button = document.createElement('button'); button.textContent = 'Reconcile original payment';
      button.onclick = () => run(() => result('reconcile', { id: operation.plan.id }, async value => { show('preview', value); await history(); }), true); row.append(button);
    }
    if (operation.state === 'PREPARED') {
      const button = document.createElement('button'); button.textContent = 'Cancel unsigned preparation';
      button.onclick = () => run(() => result('cancelPrepared', { id: operation.plan.id }, async () => { prepared = undefined; $('send').disabled = true; show('preview', 'Unsigned preparation cancelled.'); await history(); }), true); row.append(button);
    }
    $('operations').append(row);
  }
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
  try { await run(async () => { const epoch = sessionEpoch; await window.wallet[method](body); if (epoch === sessionEpoch) await refresh(); }); }
  finally { credentialPending = false; for (const [button] of credentialActions) $(button).disabled = false; }
};
$('release-link').onclick = event => { event.preventDefault(); $('release-status').open = true; $('release-status').querySelector('summary').focus(); };
$('onboardretry').onclick = () => run(refresh);
$('lock').onclick = () => { clearLocked(); return run(() => window.wallet.lock()); };
$('add').onclick = () => run(() => result('addAccount', { index: Number($('index').value) }, refresh));
$('hideaccount').onclick = () => run(() => result('hideAccount', { accountId: current()?.id || '', hidden: !current()?.hidden }, refresh));
$('showhidden').onchange = () => populateAccounts($('account').value);
$('discoveraccounts').onclick = () => run(() => result('discoverAccounts', { start: Number($('recoverystart').value), end: Number($('recoveryend').value) }, refresh));
$('account').onchange = () => openWallet($('account').value);
$('refresh').onclick = () => run(loadBalance, true);
$('receive').onclick = () => run(() => result('receive', { accountId: current()?.id || '' }, value => {
  if (value.account) { const i = accounts.findIndex(account => account.id === value.account.id); if (i >= 0) accounts[i] = value.account; show('address', value.account.address); $('address').title = value.account.address; renderAccountGroups(); }
  $('qr').src = 'data:image/png;base64,' + value.png; $('qr').hidden = false;
}), true);
$('sendview').onclick = () => $('sendheading').focus();
function transfer() { return { accountId: current()?.id || '', asset: $('asset').value, recipient: $('recipient').value, amount: $('amount').value, memo: $('memo').value }; }
async function prepare(method) {
  const body = transfer(); if (method === 'max') body.amount = '0';
  prepared = undefined; $('send').disabled = true;
  await result(method, body, async value => { prepared = value; show('preview', prepared); $('send').disabled = false; await history(); });
}
function startPrepare(method) { viewEpoch++; return run(() => prepare(method), true); }
$('prepare').onclick = () => startPrepare('prepare');
$('max').onclick = () => startPrepare('max');
$('send').onclick = () => run(async () => { if (!prepared) return; const id = prepared.id; prepared = undefined; $('send').disabled = true; await result('send', { id }, async value => { show('preview', value); await history(); }); }, true);
$('history').onclick = () => run(history, true);
for (const id of ['recipient', 'amount', 'memo', 'asset']) $(id).addEventListener('input', () => { $('send').disabled = true; prepared = undefined; viewEpoch++; show('preview', 'No payment prepared. Earlier unsigned reservations remain in the journal until cancelled.'); if (id === 'asset') { renderBalance(); renderHistory(); $('qr').hidden = true; $('qr').removeAttribute('src'); show('chainhistoryresult', 'History has not been queried for this selection.'); } });
async function addressAction(method, id) {
  const epoch = sessionEpoch, account = accounts.find(account => account.id === id);
  await window.wallet[method]({ accountId: id });
  // Decred commits a new receive index before copying/saving. Refresh only that
  // descriptor without clearing a payment form or selecting another wallet.
  if (epoch !== sessionEpoch || networks.find(network => network.id === account?.network)?.family !== 'decred') return;
  const state = await window.wallet.accounts();
  if (epoch !== sessionEpoch) return;
  if (state.locked !== false) { clearLocked(); return; }
  const fresh = state.accounts?.find(item => item.id === id);
  if (!fresh) return;
  accounts = accounts.map(item => item.id === id ? fresh : item); renderAccountGroups();
  if (current()?.id === id) { show('address', fresh.address); $('address').title = fresh.address; $('qr').hidden = true; $('qr').removeAttribute('src'); show('diagnostics', { account: fresh, network: networks.find(network => network.id === fresh.network) }); }
}
$('copy').onclick = () => run(() => addressAction('copyReceive', current()?.id || ''), true);
$('saveqr').onclick = () => run(() => addressAction('saveQR', current()?.id || ''), true);
$('scan').onclick = () => run(() => result('scanQR', {}, value => { viewEpoch++; $('recipient').value = value; prepared = undefined; $('send').disabled = true; }), true);
$('configure').onclick = () => run(() => result('configure', { network: current()?.network || '' }, refresh), true);
$('associate').onclick = () => run(() => result('associate', { accountId: current()?.id || '', address: $('recipient').value }, refresh));
$('chainhistory').onclick = () => run(() => result('chainHistory', { accountId: current()?.id || '' }, value => show('chainhistoryresult', value)), true);
$('discovery').onclick = () => run(() => result('discovery', { accountId: current()?.id || '', birthday: Number($('birthday').value), gap: Number($('gap').value), limit: Number($('limit').value), blockBudget: Number($('blockbudget').value) }, refresh));
renderGate();
run(refresh);
