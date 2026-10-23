'use strict';
const $ = id => document.getElementById(id);
let accounts = [], networks = [], operations = [], balances = [], walletTabs = [], activeWalletID = '', activeIndex = 0;
let prepared, viewEpoch = 0, sessionEpoch = 0, vaultMode = 'checking', mainnetBackupRequired = false;
let journalEpoch = 0, receiveIntent, balanceRunning = false, nativeActionsPending = 0;
const rowBalances = new Map(), balanceQueue = [];
const nativeResultMethods = new Set(['send', 'scanQR']);
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
  viewEpoch++; sessionEpoch++; journalEpoch++;
  rowBalances.clear(); balanceQueue.length = 0; closeReceiveBackup();
  if (!retainError) clearOperationError();
  if (vaultMode === 'ready') vaultMode = 'present';
  accounts = []; networks = []; operations = []; balances = []; walletTabs = []; activeWalletID = ''; activeIndex = 0; prepared = undefined;
  $('account').replaceChildren(); $('asset').replaceChildren(); $('operations').replaceChildren(); $('accountgroups').replaceChildren(); $('viewtabs').replaceChildren();
  $('journalaccount').replaceChildren(); $('journalasset').replaceChildren();
  $('walletpage').hidden = true; $('accountoverview').hidden = false; $('sendpanel').hidden = true; closeSettings();
  for (const id of ['wallettitle', 'walletsubtitle', 'gasbalance', 'connectionstatus']) show(id, '');
  $('address').removeAttribute('title');
  $('backupcallout').hidden = true; show('backupstatus', 'Locked');
  $('send').disabled = true;
  for (const id of ['recipient', 'amount', 'memo', 'associateaddress', 'birthday', 'gap', 'limit', 'blockbudget']) $(id).value = '';
  for (const id of ['address', 'diagnostics', 'preview', 'balance', 'chainhistoryresult', 'journalcontext', 'journalresult']) show(id, 'Locked');
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
  const value = await (nativeResultMethods.has(method) ? nativeAction(() => window.wallet[method](body)) : window.wallet[method](body));
  if (epoch === viewEpoch) await apply(value);
}
async function committedMetadata(method, body) {
  const epoch = sessionEpoch;
  return nativeAction(async () => {
    await window.wallet[method](body);
    // An association or discovery change is committed in Go even if its tab
    // closes. Update descriptors before resuming any account balance queries.
    if (epoch === sessionEpoch) await refresh();
  });
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
function closeWallet(id) {
  if (!walletTabs.includes(id)) return;
  const restoreFocus = activeWalletID === id || $('viewtabs').contains(document.activeElement);
  walletTabs = walletTabs.filter(item => item !== id);
  if (activeWalletID === id) {
    activeWalletID = '';
    const first = visibleAccounts().find(account => account.index === activeIndex);
    $('account').value = first.id;
    select(); renderAccountGroups(); renderView();
  }
  renderTabs();
  if (restoreFocus) $('viewtabs').querySelector('[aria-selected="true"]').focus();
}
function renderTabs() {
  const visible = visibleAccounts();
  $('viewtabs').replaceChildren();
  function tab(text, selected, page, activate, walletID = '') {
    const button = document.createElement('button');
    button.textContent = text; button.setAttribute('role', 'tab'); button.setAttribute('aria-selected', String(selected));
    button.setAttribute('aria-controls', page); button.tabIndex = selected ? 0 : -1;
    button.dataset.walletId = walletID;
    if (walletID) button.title = 'Middle-click or press Delete to close this wallet tab';
    button.onclick = event => { if (!event || event.button === 0) return activate(); };
    button.onpointerdown = event => { if (event.button === 1) event.preventDefault(); };
    button.onauxclick = event => {
      if (event.button !== 1) return;
      event.preventDefault(); event.stopPropagation();
      if (walletID) closeWallet(walletID);
    };
    button.onkeydown = event => {
      if (event.key === 'Delete' && walletID) { event.preventDefault(); closeWallet(walletID); return; }
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
    tab(`${network?.name || account.network} · ${accountName(account.index)}`, activeWalletID === id, 'walletpage', () => openWallet(id), id);
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
  const group = document.createElement('section'), heading = document.createElement('h2'), table = document.createElement('table');
  group.className = 'accountgroup'; table.className = 'wallettable';
  heading.textContent = accountName(activeIndex);
  const detail = document.createElement('p'); detail.textContent = `Derivation index ${activeIndex} · Wallets from your shared BIP39 root`;
  const head = table.createTHead().insertRow();
  for (const label of ['Network', 'Assets', 'Native balance', 'Receive address']) { const cell = document.createElement('th'); cell.scope = 'col'; cell.textContent = label; head.append(cell); }
  const body = table.createTBody();
  group.append(heading, detail, table);
  for (const account of visible) {
    const network = networks.find(network => network.id === account.network);
    const row = body.insertRow(), name = row.insertCell(), assets = row.insertCell(), balanceCell = row.insertCell(), addressCell = row.insertCell();
    balanceCell.className = 'walletbalance'; balanceCell.dataset.accountId = account.id;
    renderRowBalance(balanceCell, account);
    const address = document.createElement('span'), open = document.createElement('button'), copy = document.createElement('button');
    row.className = 'walletrow'; row.dataset.accountId = account.id; row.tabIndex = 0;
    row.setAttribute('aria-label', `Open ${network?.name || account.network} wallet, ${accountName(account.index)}`);
    open.textContent = network?.name || account.network; open.className = 'openwallet';
    open.onclick = event => { event?.stopPropagation(); return openWallet(account.id); };
    row.onclick = event => { if (event.button === 0) return openWallet(account.id); };
    row.onkeydown = event => {
      if (event.target !== row || !['Enter', ' '].includes(event.key)) return;
      event.preventDefault(); return openWallet(account.id);
    };
    name.append(open);
    address.textContent = account.address; address.title = account.address; address.className = 'walletaddress';
    const addressRow = document.createElement('div'); addressRow.className = 'addressrow';
    copy.className = 'copyaddress'; copy.title = 'Copy verified address';
    copy.setAttribute('aria-label', `Copy verified address for ${network?.name || account.network}, ${accountName(account.index)}`);
    copy.append(copyIcon()); copy.disabled = mainnetBackupRequired;
    if (mainnetBackupRequired) copy.title = 'Verify encrypted backup first';
    // Capture only the account ID. Go derives and confirms the copied address.
    copy.onclick = event => { event?.stopPropagation(); return run(() => addressAction('copyReceive', account.id), true); };
    addressRow.append(address, copy); addressCell.append(addressRow);
    assets.className = 'walletassets'; assets.textContent = (network?.assets || []).map(asset => asset.symbol).join(' · ') || 'Assets unavailable';
  }
  $('accountgroups').append(group);
  queueVisibleBalances();
}
function balanceScope(account) {
  return JSON.stringify([account, networks.find(network => network.id === account.network)]);
}
function nativeAsset(network) {
  return network?.family === 'evm' || network?.assets?.some(asset => asset.id.endsWith(':native'))
    ? network.assets.find(asset => asset.id.endsWith(':native')) : network?.assets?.length === 1 ? network.assets[0] : undefined;
}
function renderRowBalance(cell, account) {
  const network = networks.find(network => network.id === account.network), asset = nativeAsset(network), entry = rowBalances.get(account.id);
  const matches = entry?.value.filter(balance => balance?.asset === asset?.id) || [];
  cell.textContent = !asset ? 'Balance unknown — asset unavailable.' : entry?.scope !== balanceScope(account) || entry.status === 'loading' ? `Loading ${asset.symbol}…` : entry.status === 'failed' ? `${asset.symbol} unavailable — connection query failed.` : balanceText(asset, matches.length === 1 ? matches[0] : undefined);
  cell.title = `${cell.textContent} · ${network?.trust || 'Provider verification required.'}`;
}
function updateRowBalance(account) {
  for (const cell of $('accountgroups').querySelectorAll('.walletbalance')) if (cell.dataset.accountId === account.id) renderRowBalance(cell, account);
}
function queueVisibleBalances(force = false) {
  if (vaultMode !== 'ready' || activeWalletID) return;
  for (const account of visibleAccounts().filter(account => account.index === activeIndex)) {
    const scope = balanceScope(account), previous = rowBalances.get(account.id);
    if (!force && previous?.scope === scope) continue;
    for (let index = balanceQueue.length - 1; index >= 0; index--) if (balanceQueue[index].account.id === account.id) balanceQueue.splice(index, 1);
    const entry = { scope, status: 'loading', value: [] };
    rowBalances.set(account.id, entry); balanceQueue.push({ account, entry, epoch: sessionEpoch }); updateRowBalance(account);
  }
  void drainBalanceQueue();
}
async function drainBalanceQueue() {
  // Go serializes ordinary requests. One background query bounds how long a
  // deliberate native action can wait and avoids filling its request queue.
  if (balanceRunning || credentialPending || nativeActionsPending || receiveIntent) return;
  balanceRunning = true;
  try {
    while (balanceQueue.length && !credentialPending && !nativeActionsPending && !receiveIntent) {
      const { account, entry, epoch } = balanceQueue.shift();
      if (epoch !== sessionEpoch || vaultMode !== 'ready' || activeWalletID || account.index !== activeIndex || rowBalances.get(account.id) !== entry) {
        if (rowBalances.get(account.id) === entry) rowBalances.delete(account.id);
        continue;
      }
      try { const value = await window.wallet.balances({ accountId: account.id }); entry.value = Array.isArray(value) ? value : []; entry.status = 'loaded'; }
      catch { entry.status = 'failed'; }
      const fresh = accounts.find(item => item.id === account.id);
      if (epoch === sessionEpoch && fresh && balanceScope(fresh) === entry.scope && rowBalances.get(account.id) === entry) updateRowBalance(fresh);
      // Give explicit user actions a turn before dispatching the next RPC.
      await new Promise(resolve => setTimeout(resolve, 0));
    }
  } finally { balanceRunning = false; }
}
$('refreshbalances').onclick = () => queueVisibleBalances(true);
function openWallet(id) {
  const account = visibleAccounts().find(account => account.id === id);
  if (!account) return;
  activeWalletID = id; activeIndex = account.index; $('account').value = id;
  if (!walletTabs.includes(id)) walletTabs.push(id);
  select(); renderTabs(); renderView(); $('wallettitle').focus();
  return run(async () => { await Promise.all([loadBalance(), history()]); }, true);
}
async function refresh(queryActive = true) {
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
  $('receive').disabled = false; $('sendview').disabled = false;
  for (const id of ['copy', 'saveqr', 'prepare', 'max']) $(id).disabled = mainnetBackupRequired;
  $('sendbackupnotice').hidden = !mainnetBackupRequired;
  $('backupcallout').hidden = state.backupVerified === true;
  show('backupstatus', state.backupVerified === true ? 'Encrypted backup verified' : 'Encrypted backup verification required');
  populateAccounts($('account').value);
  if (activeWalletID && queryActive) await Promise.all([loadBalance(), history()]);
}
function current() { return accounts.find(account => account.id === $('account').value); }
function select() {
  viewEpoch++;
  closeReceiveBackup();
  $('sendpanel').hidden = true;
  const account = current();
  $('asset').replaceChildren();
  show('address', account ? account.address : 'Locked or no account');
  $('address').title = account?.address || '';
  balances = []; operations = [];
  show('balance', 'Balance unknown — query required.'); $('gasbalance').hidden = true;
  show('preview', 'No payment prepared.');
  show('chainhistoryresult', 'History has not been queried.'); $('operations').replaceChildren();
  for (const id of ['recipient', 'amount', 'memo', 'associateaddress']) $(id).value = '';
  $('hideaccount').textContent = account?.hidden ? 'Unhide selected' : 'Hide selected';
  prepared = undefined; $('send').disabled = true;
  const network = networks.find(network => network.id === account?.network);
  show('wallettitle', network?.name || account?.network || 'Network wallet');
  show('walletsubtitle', account ? `${accountName(account.index)} · ${account.network}` : 'No account selected');
  show('connectionstatus', network?.family === 'decred' && !network.rpc ? 'Connection required — configure an authenticated TLS dcrd node before querying or sending.' : network ? `${network.rpc ? 'RPC: ' + network.rpc + ' · ' : ''}${network.trust || 'Provider verification required.'}` : 'Connection details unavailable.');
  loadDiscovery(network?.family === 'decred' ? account : null);
  $('decrednote').hidden = network?.family !== 'decred'; $('dcrdiscoverycontrols').hidden = network?.family !== 'decred';
  $('associationcontrols').hidden = !['near', 'aptos'].includes(network?.family);
  $('associate').hidden = $('associationcontrols').hidden;
  show('diagnostics', { account: account || null, network: network || null });
  for (const asset of network?.assets || []) {
    const option = document.createElement('option');
    option.value = asset.id; option.textContent = `${asset.symbol} · ${asset.id}`; $('asset').append(option);
  }
  populateJournal(account?.id, $('asset').value);
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
  if (!balance || typeof balance.atomic !== 'string' || !/^(0|[1-9][0-9]*)$/.test(balance.atomic) || typeof balance.state !== 'string' || !balance.state || /unknown|unavailable|synchroniz|stale|pending|error|failed|incomplete/i.test(balance.state) || typeof balance.at !== 'string' || !balance.at) return 'Balance unknown — no complete network observation.';
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
  const epoch = viewEpoch, session = sessionEpoch, account = current();
  const entry = account && { scope: balanceScope(account), status: 'loading', value: [] };
  if (entry) rowBalances.set(account.id, entry);
  balances = []; show('balance', 'Balance unknown — querying the selected network…'); $('gasbalance').hidden = true;
  try {
    const value = await window.wallet.balances({ accountId: account?.id || '' }), rows = Array.isArray(value) ? value : [];
    const fresh = accounts.find(item => item.id === account?.id);
    if (session === sessionEpoch && entry && fresh && balanceScope(fresh) === entry.scope && rowBalances.get(account.id) === entry) { entry.value = rows; entry.status = 'loaded'; updateRowBalance(fresh); }
    if (epoch === viewEpoch) { balances = rows; renderBalance(); }
  }
  catch (error) { if (session === sessionEpoch && entry && rowBalances.get(account.id) === entry) { entry.status = 'failed'; updateRowBalance(account); } if (epoch === viewEpoch) show('balance', 'Balance unavailable — RPC query failed. No balance has been verified.'); throw error; }
}
function journalAccount() { return accounts.find(account => account.id === $('journalaccount').value); }
function populateJournal(selected = $('journalaccount').value, assetID = $('journalasset').value) {
  journalEpoch++; $('journalaccount').replaceChildren();
  for (const account of visibleAccounts()) {
    const option = document.createElement('option'); option.value = account.id;
    option.textContent = `${networks.find(network => network.id === account.network)?.name || account.network} · ${accountName(account.index)}`;
    $('journalaccount').append(option);
  }
  if (visibleAccounts().some(account => account.id === selected)) $('journalaccount').value = selected;
  populateJournalAssets(assetID);
}
function populateJournalAssets(selected) {
  journalEpoch++; $('journalasset').replaceChildren();
  const account = journalAccount(), network = networks.find(network => network.id === account?.network);
  for (const asset of network?.assets || []) { const option = document.createElement('option'); option.value = asset.id; option.textContent = `${asset.symbol} · ${asset.id}`; $('journalasset').append(option); }
  if (network?.assets.some(asset => asset.id === selected)) $('journalasset').value = selected;
  show('journalcontext', account ? `${accountName(account.index)} · ${account.network} · ${$('journalasset').value}` : 'Choose a wallet to inspect its payments.');
  $('history').disabled = !account; $('chainhistory').disabled = !account;
  show('chainhistoryresult', 'History has not been queried for this selection.'); renderHistory();
  show('journalresult', 'No journal action requested.');
}
async function journalResult(method, body, apply) {
  const epoch = journalEpoch, session = sessionEpoch;
  let value;
  try { value = await window.wallet[method](body); }
  catch (error) { if (epoch !== journalEpoch || session !== sessionEpoch) return; throw error; }
  if (epoch === journalEpoch && session === sessionEpoch) await apply(value);
}
async function history() {
  await journalResult('history', {}, value => { operations = value || []; renderHistory(); });
}
function renderHistory() {
  $('operations').replaceChildren();
  const account = journalAccount();
  if (!account) return;
  // A journal can include every derived account. Never show or reconcile an
  // operation from another wallet or another selected asset on this page.
  for (const operation of operations.filter(operation => operation?.plan?.account?.id === account.id && operation.plan.account.network === account.network && operation.plan.asset === $('journalasset').value)) {
    const row = document.createElement('div'), text = document.createElement('pre');
    text.textContent = JSON.stringify(operation, null, 2); row.append(text);
    if (['UNKNOWN', 'SUBMITTED', 'SUBMITTING', 'SIGNED_NOT_SUBMITTED', 'REORGED'].includes(operation.state)) {
      const button = document.createElement('button'); button.textContent = 'Reconcile original payment';
      button.onclick = () => run(() => journalResult('reconcile', { id: operation.plan.id }, async value => { show('journalresult', value); await history(); }), true); row.append(button);
    }
    if (operation.state === 'PREPARED') {
      const button = document.createElement('button'); button.textContent = 'Cancel unsigned preparation';
      button.onclick = () => run(() => journalResult('cancelPrepared', { id: operation.plan.id }, async () => { if (prepared?.id === operation.plan.id) { prepared = undefined; $('send').disabled = true; show('preview', 'Unsigned preparation cancelled.'); } await history(); }), true); row.append(button);
    }
    $('operations').append(row);
  }
}
function closeSettings() {
  const wasOpen = !$('settingsdialog').hidden;
  $('settingsdialog').hidden = true; $('settings').setAttribute('aria-expanded', 'false');
  document.querySelector('header').inert = false; document.querySelector('main').inert = false;
  if (wasOpen) $('settings').focus();
}
function settingsFocusables() {
  return [...$('settingsdialog').querySelectorAll('button, input, select, summary')].filter(element => {
    const closed = element.closest('details:not([open])');
    return !element.disabled && !element.closest('[hidden]') && (!closed || closed.querySelector('summary') === element);
  });
}
$('settings').onclick = () => {
  closeReceiveBackup();
  $('settingsdialog').hidden = false; $('settings').setAttribute('aria-expanded', 'true');
  document.querySelector('header').inert = true; document.querySelector('main').inert = true;
  $('closesettings').focus();
};
$('closesettings').onclick = closeSettings;
$('settingsdialog').onclick = event => { if (event.target === $('settingsdialog')) closeSettings(); };
$('settingsdialog').onkeydown = event => {
  if (event.key === 'Escape') { event.preventDefault(); closeSettings(); return; }
  if (event.key !== 'Tab') return;
  const focusable = settingsFocusables(), first = focusable[0], last = focusable[focusable.length - 1];
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
};
const credentialActions = [
  ['unlock', 'unlock', {}], ['create', 'create', { words: 12 }], ['create24', 'create', { words: 24 }],
  ['import', 'import', { words: 12 }], ['backup', 'backup', {}], ['restore', 'restore', {}], ['password', 'password', {}],
  ['onboardcreate', 'create', { words: 12 }], ['onboardunlock', 'unlock', {}], ['backupnow', 'backup', {}]
];
let credentialPending = false;
for (const [id, method, body] of credentialActions) $(id).onclick = async () => {
  if (credentialPending) return;
  closeSettings();
  credentialPending = true;
  for (const [button] of credentialActions) $(button).disabled = true;
  try { await run(async () => { const epoch = sessionEpoch; await window.wallet[method](body); if (epoch === sessionEpoch) await refresh(); }); }
  finally { credentialPending = false; for (const [button] of credentialActions) $(button).disabled = false; void drainBalanceQueue(); }
};
$('release-link').onclick = event => { event.preventDefault(); $('release-status').open = true; $('release-status').querySelector('summary').focus(); };
$('onboardretry').onclick = () => run(refresh);
$('lock').onclick = () => { clearLocked(); return run(() => window.wallet.lock()); };
$('add').onclick = () => { closeSettings(); return run(() => committedMetadata('addAccount', { index: Number($('index').value) })); };
$('hideaccount').onclick = () => run(() => committedMetadata('hideAccount', { accountId: current()?.id || '', hidden: !current()?.hidden }));
$('showhidden').onchange = () => populateAccounts($('account').value);
$('discoveraccounts').onclick = () => { closeSettings(); return run(() => committedMetadata('discoverAccounts', { start: Number($('recoverystart').value), end: Number($('recoveryend').value) })); };
$('account').onchange = () => openWallet($('account').value);
$('refresh').onclick = () => run(loadBalance, true);
async function receiveWallet(id) {
  return nativeAction(async () => {
  const epoch = sessionEpoch;
  const value = await window.wallet.receive({ accountId: id });
  // The Go window owns the verified QR/address and clipboard. A committed
  // Decred receive index must still refresh the overview after a tab switch.
  if (epoch !== sessionEpoch || !value.account || value.account.id !== id || !accounts.some(account => account.id === id)) return;
  accounts = accounts.map(account => account.id === id ? value.account : account); renderAccountGroups();
  if (current()?.id === id) { show('address', value.account.address); $('address').title = value.account.address; show('diagnostics', { account: value.account, network: networks.find(network => network.id === value.account.network) }); }
  });
}
async function nativeAction(action) {
  nativeActionsPending++;
  try { return await action(); }
  finally { nativeActionsPending--; void drainBalanceQueue(); }
}
function closeReceiveBackup() {
  const wasOpen = !$('receivebackupdialog').hidden;
  receiveIntent = undefined; $('receivebackupdialog').hidden = true;
  if (wasOpen) { document.querySelector('header').inert = false; document.querySelector('main').inert = false; $('receive').focus(); }
}
$('receive').onclick = () => {
  if (mainnetBackupRequired) {
    closeSettings(); receiveIntent = { id: current()?.id || '', view: viewEpoch, session: sessionEpoch };
    show('receivebackupcontext', `${$('wallettitle').textContent} · ${accountName(activeIndex)}`);
    $('receivebackupdialog').hidden = false; document.querySelector('header').inert = true; document.querySelector('main').inert = true; $('receivebackup').focus();
    return;
  }
  return run(() => receiveWallet(current()?.id || ''), true);
};
$('closereceivebackup').onclick = () => { closeReceiveBackup(); void drainBalanceQueue(); };
$('receivebackupdialog').onclick = event => { if (event.target === $('receivebackupdialog')) $('closereceivebackup').click(); };
$('receivebackupdialog').onkeydown = event => {
  if (event.key === 'Escape') { event.preventDefault(); $('closereceivebackup').click(); return; }
  if (event.key !== 'Tab') return;
  const first = $('closereceivebackup'), last = $('receivebackup');
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
};
async function backupForWorkflow(receive = false) {
  if (credentialPending || vaultMode !== 'ready') return;
  const intent = receive ? receiveIntent : { id: current()?.id || '', view: viewEpoch, session: sessionEpoch };
  if (!intent) return;
  const draft = ['recipient', 'amount', 'memo'].map(id => $(id).value), assetID = $('asset').value, sendOpen = !$('sendpanel').hidden;
  closeReceiveBackup(); credentialPending = true;
  $('sendbackup').disabled = true; $('receivebackup').disabled = true;
  try {
    await run(async () => {
      await window.wallet.backup({});
      if (intent.session !== sessionEpoch) return;
      const unchanged = intent.view === viewEpoch && activeWalletID === intent.id;
      await refresh(false);
      if (!unchanged || viewEpoch !== intent.view + 1 || intent.session !== sessionEpoch || activeWalletID !== intent.id || vaultMode !== 'ready') return;
      if (receive && !mainnetBackupRequired) await receiveWallet(intent.id);
      else if (!receive && sendOpen) { $('sendpanel').hidden = false; if ([...$('asset').options].some(option => option.value === assetID)) $('asset').value = assetID; renderBalance(); populateJournal(intent.id, assetID); ['recipient', 'amount', 'memo'].forEach((id, index) => { $(id).value = draft[index]; }); $('sendheading').focus(); }
    });
  } finally { credentialPending = false; $('sendbackup').disabled = false; $('receivebackup').disabled = false; void drainBalanceQueue(); }
}
$('receivebackup').onclick = () => backupForWorkflow(true);
$('sendbackup').onclick = () => backupForWorkflow();
$('sendview').onclick = () => { if (!activeWalletID) return; $('sendpanel').hidden = false; $('sendheading').focus(); };
$('closesend').onclick = () => {
  viewEpoch++; $('sendpanel').hidden = true; prepared = undefined; $('send').disabled = true;
  for (const id of ['recipient', 'amount', 'memo']) $(id).value = '';
  show('preview', 'No payment prepared. Earlier unsigned reservations remain in the journal until cancelled.');
  $('sendview').focus();
};
function transfer() { return { accountId: current()?.id || '', asset: $('asset').value, recipient: $('recipient').value, amount: $('amount').value, memo: $('memo').value }; }
async function prepare(method) {
  const body = transfer(); if (method === 'max') body.amount = '0';
  prepared = undefined; $('send').disabled = true;
  await result(method, body, async value => { prepared = value; show('preview', prepared); $('send').disabled = false; await history(); });
}
function startPrepare(method) { if ($('sendpanel').hidden || mainnetBackupRequired) return; viewEpoch++; return run(() => prepare(method), true); }
$('prepare').onclick = () => startPrepare('prepare');
$('max').onclick = () => startPrepare('max');
$('send').onclick = () => run(async () => { if ($('sendpanel').hidden || !prepared) return; const id = prepared.id; prepared = undefined; $('send').disabled = true; await result('send', { id }, async value => { show('preview', value); await history(); }); }, true);
$('history').onclick = () => run(history, true);
$('journalaccount').onchange = () => { populateJournalAssets(); return run(history, true); };
$('journalasset').onchange = () => { journalEpoch++; show('journalcontext', `${accountName(journalAccount().index)} · ${journalAccount().network} · ${$('journalasset').value}`); show('journalresult', 'No journal action requested.'); show('chainhistoryresult', 'History has not been queried for this selection.'); renderHistory(); };
for (const id of ['recipient', 'amount', 'memo', 'asset']) $(id).addEventListener('input', () => { $('send').disabled = true; prepared = undefined; viewEpoch++; show('preview', 'No payment prepared. Earlier unsigned reservations remain in the journal until cancelled.'); if (id === 'asset') { renderBalance(); populateJournal(current()?.id, $('asset').value); } });
async function addressAction(method, id) {
  return nativeAction(async () => {
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
  if (current()?.id === id) { show('address', fresh.address); $('address').title = fresh.address; show('diagnostics', { account: fresh, network: networks.find(network => network.id === fresh.network) }); }
  });
}
$('copy').onclick = () => run(() => addressAction('copyReceive', current()?.id || ''), true);
$('saveqr').onclick = () => run(() => addressAction('saveQR', current()?.id || ''), true);
$('scan').onclick = () => { if ($('sendpanel').hidden) return; return run(() => result('scanQR', {}, value => { viewEpoch++; $('recipient').value = value; prepared = undefined; $('send').disabled = true; }), true); };
$('configure').onclick = () => run(() => nativeAction(async () => {
  const epoch = sessionEpoch, network = current()?.network || '';
  await window.wallet.configure({ network });
  if (epoch !== sessionEpoch) return;
  // Public RPC metadata intentionally omits provider paths/credentials. Even a
  // change with the same public origin invalidates that network's observations.
  for (const account of accounts) if (account.network === network) rowBalances.delete(account.id);
  await refresh();
}), true);
$('associate').onclick = () => run(() => committedMetadata('associate', { accountId: current()?.id || '', address: $('associateaddress').value }));
$('chainhistory').onclick = () => run(() => journalResult('chainHistory', { accountId: journalAccount()?.id || '' }, value => show('chainhistoryresult', value)), true);
$('discovery').onclick = () => run(() => committedMetadata('discovery', { accountId: current()?.id || '', birthday: Number($('birthday').value), gap: Number($('gap').value), limit: Number($('limit').value), blockBudget: Number($('blockbudget').value) }));
renderGate();
run(refresh);
