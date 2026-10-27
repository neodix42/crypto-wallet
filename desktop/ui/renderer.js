'use strict';
const $ = id => document.getElementById(id);
let accounts = [], networks = [], operations = [], balances = [], walletTabs = [], activeWalletID = '', activeIndex = 0;
let prepared, viewEpoch = 0, sessionEpoch = 0, vaultMode = 'checking', recoveryRequired = false;
let chainHistoryCursor = '', chainHistoryEntries = [], chainHistoryBytes = 0;
let journalCursor = '', journalBytes = 0, journalRequestEpoch = 0, chainRequestEpoch = 0;
let journalEpoch = 0, receiveIntent, balanceRunning = false, nativeActionsPending = 0;
let exchangeOpen = false, exchangeEpoch = 0, exchangeQuoteEpoch = 0, exchangeWithdrawEpoch = 0, exchangeStateEpoch = 0;
let exchangeBusy = false, exchangeConnected = false, exchangeMarket, exchangeWithdrawal, exchangeOperations = [];
let settingsOpen = false, settingsSection = 'wallet', notificationReturnFocus, notificationTimer, notificationGeneration = 0, lastLockedMode;
const rowBalances = new Map(), balanceQueue = [];
const walletAssets = new Map();
const decredConnectionRequired = 'Connection required — configure an authenticated TLS dcrd node. Balance unknown.';
const nativeResultMethods = new Set(['send']);
const show = (id, value) => { $(id).textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2); };
function showStatus(message) {
  cancelNotificationTimer();
  show('message', message); $('message').hidden = false; $('notifications').hidden = false;
  const generation = notificationGeneration;
  notificationTimer = window.setTimeout(() => { if (generation === notificationGeneration) dismissNotification(); }, 7000);
}
function cancelNotificationTimer() {
  window.clearTimeout(notificationTimer); notificationTimer = undefined; notificationGeneration++;
}
function dismissNotification() {
  cancelNotificationTimer();
  const restoreFocus = $('notifications').contains(document.activeElement);
  $('notifications').hidden = true; clearOperationError(); show('message', ''); $('message').hidden = true;
  if (restoreFocus) {
    const target = notificationReturnFocus;
    if (target?.isConnected && !target.disabled && !target.closest('[hidden]')) target.focus();
    else $('viewtabs').querySelector('[aria-selected="true"]')?.focus();
  }
  notificationReturnFocus = undefined;
}
$('notifications').onkeydown = event => { if (event.key === 'Escape') { event.preventDefault(); dismissNotification(); } };
const operationErrors = Object.freeze({
  PASSWORD_POLICY: 'Use an app password of 12–4096 UTF-8 bytes. Enter and repeat it in the wallet dialog, then try again.',
  INPUT_TOO_LONG: 'An input exceeds 4,096 UTF-8 bytes. Shorten it in the wallet dialog.',
  PASSWORD_MISMATCH: 'The passwords did not match. Enter the same app password in both password fields and try again.',
  INVALID_MNEMONIC: 'Enter a valid English BIP39 recovery phrase (12, 15, 18, 21 or 24 words) in the wallet dialog. A new wallet generates its phrase automatically.',
  RECOVERY_WORDS_MISMATCH: 'The recorded recovery words did not match. Record the complete phrase in order, then try again. Your wallet remains unavailable for payments until this is complete.',
  REQUEST_CANCELLED: 'Request cancelled. Check the wallet state before retrying.',
  REQUEST_FAILED_STATE_UNKNOWN: 'Request failed or is unavailable. Balance is unknown until verified; reconcile any pending payment before retrying.'
});
function clearOperationError() { $('operationerror').hidden = true; show('operationerror', ''); }
function showOperationError(error) {
  // Electron copies Error.message but drops custom Error properties at the bridge.
  const code = typeof error?.message === 'string' && Object.hasOwn(operationErrors, error.message) ? error.message : 'REQUEST_FAILED_STATE_UNKNOWN';
  const message = operationErrors[code];
  if (!$('notifications').contains(document.activeElement)) notificationReturnFocus = document.activeElement;
  showStatus(message); $('message').hidden = true; show('operationerror', message); $('operationerror').hidden = false;
  $('operationerror').focus();
}
const gates = {
  checking: ['Checking your local wallet', 'Reading the local vault status. No password dialog opens until you choose an action.'],
  missing: ['Create your app password', 'Choose your password in the wallet dialog. The signer generates one BIP39 root and derives Main account (index 0) across every supported network. Write down your generated phrase in the wallet dialog, then confirm the requested words to finish setup. Already have a secret phrase? Choose Restore from recovery phrase to import it instead.'],
  present: ['Welcome back', 'Your encrypted wallet is already on this device. Enter its app password in the wallet dialog to unlock your accounts.'],
  unavailable: ['Wallet storage unavailable', 'The local vault could not be inspected safely. Check storage access and retry. Creation and restoration remain unavailable while its state is uncertain.']
};
function renderGate() {
  const ready = vaultMode === 'ready', missing = vaultMode === 'missing', present = vaultMode === 'present';
  $('onboarding').hidden = ready || settingsOpen; $('walletcontent').hidden = !ready || settingsOpen;
  $('emptyactions').hidden = !missing; $('walletactions').hidden = !ready;
  $('backupphrase').hidden = !ready || recoveryRequired;
  $('unlock').hidden = !present; $('lock').disabled = !ready;
  $('onboardcreate').hidden = !missing; $('onboardrestore').hidden = !missing; $('onboardunlock').hidden = !present;
  $('onboardretry').hidden = vaultMode !== 'unavailable';
  $('journalpanel').hidden = !ready; $('journallocked').hidden = ready;
  renderView();
  if (ready) return;
  const [title, description] = gates[vaultMode];
  show('onboardtitle', title); show('onboarddescription', description);
}
function clearLocked() {
  const retainError = vaultMode !== 'ready' && !$('operationerror').hidden;
  viewEpoch++; sessionEpoch++; journalEpoch++; resetChainHistory(); resetJournalPages();
  rowBalances.clear(); balanceQueue.length = 0; closeReceiveRecovery();
  walletAssets.clear();
  if (!retainError) clearOperationError();
  if (vaultMode === 'ready') vaultMode = 'present';
  accounts = []; networks = []; operations = []; balances = []; walletTabs = []; activeWalletID = ''; activeIndex = 0; prepared = undefined;
  resetExchange();
  $('account').replaceChildren(); $('asset').replaceChildren(); $('operations').replaceChildren(); $('accountgroups').replaceChildren(); $('viewtabs').replaceChildren();
  $('journalaccount').replaceChildren(); $('journalasset').replaceChildren();
  settingsOpen = false; settingsSection = 'wallet';
  $('walletpage').hidden = true; $('accountoverview').hidden = false; $('sendpanel').hidden = true;
  for (const id of ['wallettitle', 'walletsubtitle', 'walletaddress', 'amountunit', 'gasbalance', 'connectionstatus']) show(id, '');
  $('walletassets').replaceChildren(); $('copywalletaddress').disabled = true;
  for (const id of ['walletaddress', 'balance', 'gasbalance']) $(id).removeAttribute('title');
  $('address').removeAttribute('title');
  $('recoverycallout').hidden = true; show('recoverystatus', 'Locked');
  $('send').disabled = true;
  for (const id of ['recipient', 'amount', 'memo', 'associateaddress', 'birthday', 'gap', 'limit', 'blockbudget']) $(id).value = '';
  for (const id of ['address', 'diagnostics', 'preview', 'balance', 'chainhistoryresult', 'journalcontext', 'journalresult']) show(id, 'Locked');
  if (!retainError && lastLockedMode !== vaultMode) showStatus(vaultMode === 'missing' ? 'Start by choosing your app password in the wallet dialog.' : vaultMode === 'unavailable' ? 'Wallet storage unavailable. Retry the storage check before continuing.' : 'Wallet locked. Unlock through the wallet dialog.');
  lastLockedMode = vaultMode;
  renderGate(); renderTabs();
}
const removeLockListener = window.wallet.onLocked(clearLocked);
window.addEventListener('beforeunload', () => { cancelNotificationTimer(); removeLockListener(); }, { once: true });
window.wallet.getVersion().then(version => { $('version').textContent = 'Version ' + version; }).catch(() => { $('version').textContent = 'Version unavailable'; });
async function run(fn, scoped = false, completion = 'Request completed. Check the returned state and any limitations.') {
  const epoch = sessionEpoch, view = viewEpoch;
  try {
    clearOperationError();
    showStatus('Waiting for Go wallet…');
    await fn();
    if (epoch === sessionEpoch && (!scoped || view === viewEpoch)) showStatus(completion);
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
  select(); populateExchangeAccounts(); renderAccountGroups(); renderTabs(); renderView();
}
function renderView() {
  $('accountoverview').hidden = settingsOpen || exchangeOpen || !!activeWalletID;
  $('walletpage').hidden = settingsOpen || exchangeOpen || !activeWalletID;
  $('exchangepage').hidden = settingsOpen || !exchangeOpen;
  $('settingspage').hidden = !settingsOpen;
  $('walletcontent').hidden = vaultMode !== 'ready' || settingsOpen;
  $('onboarding').hidden = vaultMode === 'ready' || settingsOpen;
}
function resetExchange() {
  exchangeOpen = false; exchangeEpoch++; exchangeQuoteEpoch++; exchangeWithdrawEpoch++; exchangeStateEpoch++;
  exchangeBusy = false; exchangeConnected = false; exchangeMarket = undefined; exchangeWithdrawal = undefined; exchangeOperations = [];
  for (const id of ['exchangeaccount', 'exchangeasset', 'exchangewithdrawaccount', 'exchangewithdrawasset', 'exchangeoperations']) $(id).replaceChildren();
  for (const id of ['exchangedepositamount', 'exchangeswapamount', 'exchangewithdrawamount']) $(id).value = '';
  $('exchangeside').value = 'Sell'; $('exchangeslippage').value = '0.5';
  for (const id of ['exchangeconnection', 'exchangeroute', 'exchangeprice', 'exchangewithdrawroute', 'exchangebalances', 'exchangeresult']) show(id, 'Locked');
  show('exchangesettingsconnection', 'Locked');
  for (const id of ['exchangeaddress', 'exchangeassetinfo', 'exchangewithdrawaddress', 'exchangewithdrawassetinfo']) show(id, 'Locked');
  show('exchangedetails', 'Locked'); $('exchangedetails').hidden = true;
  $('exchangepage').hidden = true; renderExchangeControls();
}
function leaveExchange() {
  if (!exchangeOpen) return;
  exchangeOpen = false; exchangeEpoch++; exchangeQuoteEpoch++; exchangeWithdrawEpoch++; exchangeStateEpoch++;
}
function openExchange() {
  if (vaultMode !== 'ready') return;
  leaveSettings();
  exchangeOpen = true; exchangeEpoch++; activeWalletID = '';
  if ($('exchangeresult').textContent === 'Locked') show('exchangeresult', 'No exchange action requested.');
  select(); renderTabs(); renderView(); focusActiveTab(); renderExchangeControls();
  return run(refreshExchangeState, true);
}
function exchangeSelection(withdrawal = false) {
  const account = accounts.find(item => item.id === $(withdrawal ? 'exchangewithdrawaccount' : 'exchangeaccount').value);
  const asset = networks.find(item => item.id === account?.network)?.assets.find(item => item.id === $(withdrawal ? 'exchangewithdrawasset' : 'exchangeasset').value);
  return account && asset ? { accountId: account.id, assetId: asset.id } : undefined;
}
function populateExchangeAccounts() {
  for (const withdrawal of [false, true]) {
    const selector = $(withdrawal ? 'exchangewithdrawaccount' : 'exchangeaccount'), selected = selector.value;
    selector.replaceChildren();
    for (const account of visibleAccounts()) {
      const option = document.createElement('option'); option.value = account.id;
      option.textContent = `${networks.find(network => network.id === account.network)?.name || account.network} · ${accountName(account.index)}`;
      selector.append(option);
    }
    if (visibleAccounts().some(account => account.id === selected)) selector.value = selected;
    populateExchangeAssets(withdrawal);
  }
}
function populateExchangeAssets(withdrawal) {
  const account = accounts.find(item => item.id === $(withdrawal ? 'exchangewithdrawaccount' : 'exchangeaccount').value);
  const selector = $(withdrawal ? 'exchangewithdrawasset' : 'exchangeasset'), selected = selector.value;
  const network = networks.find(item => item.id === account?.network), assets = network?.assets || [];
  selector.replaceChildren();
  for (const asset of assets) {
    const option = document.createElement('option'); option.value = asset.id; option.textContent = assetLabel(asset, network); selector.append(option);
  }
  if (assets.some(asset => asset.id === selected)) selector.value = selected;
  invalidateExchangeSelection(withdrawal);
}
function assetLabel(asset, network) { return `${asset.symbol} (${asset.contract ? network.family === 'evm' ? 'ERC-20' : 'token' : 'native'})`; }
function renderExchangeAssetIdentity(withdrawal) {
  const selection = exchangeSelection(withdrawal);
  const account = accounts.find(item => item.id === selection?.accountId);
  const network = networks.find(item => item.id === account?.network);
  const asset = network?.assets.find(item => item.id === selection.assetId);
  show(withdrawal ? 'exchangewithdrawaddress' : 'exchangeaddress', account?.address || 'No wallet selected');
  show(withdrawal ? 'exchangewithdrawassetinfo' : 'exchangeassetinfo', asset?.contract ? `${network.family === 'evm' ? 'Token contract' : 'Token identifier'}: ${asset.contract}. This identifies the token, not your wallet address.` : asset ? `Native asset on ${network.name || network.id}.` : 'Choose an asset.');
}
function invalidateExchangeSelection(withdrawal) {
  renderExchangeAssetIdentity(withdrawal);
  if (exchangeOpen) viewEpoch++;
  if (withdrawal) { exchangeWithdrawEpoch++; exchangeWithdrawal = undefined; show('exchangewithdrawroute', 'Check the selected withdrawal network and current fee.'); }
  else { exchangeQuoteEpoch++; exchangeMarket = undefined; show('exchangeroute', 'Refresh the selected asset’s market and deposit network.'); show('exchangeprice', 'Price unknown — refresh the selected market.'); }
  renderExchangeControls();
}
function renderExchangeControls() {
  const ready = vaultMode === 'ready' && exchangeOpen && !exchangeBusy;
  const settingsReady = vaultMode === 'ready' && exchangeSettingsVisible() && !exchangeBusy;
  for (const id of ['exchangeconnect', 'exchangeendpoint', 'exchangedisconnect', 'exchangesettingsrefresh']) $(id).disabled = !settingsReady;
  for (const id of ['exchangeaccount', 'exchangeasset', 'exchangeside', 'exchangewithdrawaccount', 'exchangewithdrawasset', 'exchangedepositamount', 'exchangeswapamount', 'exchangewithdrawamount', 'exchangeslippage']) $(id).disabled = !ready;
  $('exchangeconnect').textContent = exchangeConnected ? 'Change Bybit API key' : 'Connect Bybit API key';
  $('exchangedisconnect').hidden = !exchangeConnected;
  $('exchangerefresh').disabled = !ready;
  $('exchangequote').disabled = !ready || !exchangeConnected || !exchangeSelection();
  $('exchangewithdrawquote').disabled = !ready || !exchangeConnected || !exchangeSelection(true);
  const authorize = ready && exchangeConnected && !recoveryRequired;
  $('exchangedeposit').disabled = !authorize || exchangeMarket?.route?.supported !== true || exchangeMarket.route.depositEnabled !== true;
  $('exchangeswap').disabled = !authorize || !exchangePriceKnown(exchangeMarket) || exchangeMarket.coin === 'USDT';
  $('exchangewithdraw').disabled = !authorize || exchangeWithdrawal?.route?.supported !== true || exchangeWithdrawal.route.withdrawEnabled !== true;
  show('exchangeswapunit', $('exchangeside').value === 'Buy' ? 'USDT amount to spend' : 'Amount of wallet asset to sell');
  for (const button of $('exchangeoperations').querySelectorAll('button')) button.disabled = !ready || !exchangeConnected;
}
function exchangePriceKnown(market) {
  return typeof market?.symbol === 'string' && /^[A-Z0-9]{1,24}USDT$/.test(market.symbol) && typeof market.lastPrice === 'string' && /^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(market.lastPrice) && /[1-9]/.test(market.lastPrice);
}
function publicExchangeDetails(value) {
  const detail = {};
  for (const field of ['id', 'kind', 'state', 'message', 'remoteId', 'transactionId', 'filledBase', 'filledQuote', 'reportedAmount', 'reportedFee', 'accountId', 'assetId', 'coin', 'symbol', 'amount', 'createdAt']) {
    if (typeof value?.[field] === 'string' && value[field].length <= 4096) detail[field] = value[field];
  }
  if (value?.fees && typeof value.fees === 'object' && !Array.isArray(value.fees)) {
    const fees = {};
    for (const [coin, fee] of Object.entries(value.fees).slice(0, 32)) if (/^[A-Z0-9]{1,16}$/.test(coin) && typeof fee === 'string' && fee.length <= 128 && /^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(fee)) fees[coin] = fee;
    if (Object.keys(fees).length) detail.fees = fees;
  }
  return detail;
}
function renderExchangeOperations(value) {
  exchangeOperations = Array.isArray(value) ? value.slice(0, 256) : [];
  $('exchangeoperations').replaceChildren();
  for (const operation of exchangeOperations) {
    if (!operation || typeof operation.id !== 'string' || typeof operation.state !== 'string') continue;
    const row = document.createElement('div'), detail = document.createElement('pre');
    detail.textContent = JSON.stringify(publicExchangeDetails(operation), null, 2); row.append(detail);
    if (['UNKNOWN', 'SUBMITTED', 'SUBMITTING', 'PENDING', 'PARTIALLY_FILLED', 'ACCEPTED'].includes(operation.state)) {
      const button = document.createElement('button'); button.textContent = 'Reconcile original exchange operation';
      button.onclick = () => exchangeMutation('exchangeReconcile', { id: operation.id }); row.append(button);
    }
    $('exchangeoperations').append(row);
  }
  renderExchangeControls();
}
async function refreshExchangeState(afterMutation = false) {
  if ((!exchangeOpen && !exchangeSettingsVisible()) || vaultMode !== 'ready' || exchangeBusy && !afterMutation) return;
  const epoch = exchangeEpoch, session = sessionEpoch, request = ++exchangeStateEpoch;
  exchangeConnected = false; exchangeMarket = undefined; exchangeWithdrawal = undefined; exchangeQuoteEpoch++; exchangeWithdrawEpoch++;
  show('exchangebalances', 'Balances unknown — querying Bybit.'); renderExchangeControls();
  let value;
  try { value = await window.wallet.exchangeStatus({}); }
  catch (error) { if (epoch !== exchangeEpoch || session !== sessionEpoch || request !== exchangeStateEpoch) return; exchangeConnected = false; exchangeMarket = undefined; exchangeWithdrawal = undefined; exchangeQuoteEpoch++; exchangeWithdrawEpoch++; renderExchangeControls(); show('exchangebalances', 'Balances unknown — exchange query failed.'); show('exchangeprice', 'Price unknown — exchange query failed.'); showExchangeConnection('Bybit connection unavailable. Operation state remains unknown; reconnect in Settings → Exchange and reconcile the original request.'); throw error; }
  if (epoch !== exchangeEpoch || session !== sessionEpoch || request !== exchangeStateEpoch) return;
  if (typeof value?.connected !== 'boolean') throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
  exchangeConnected = value.connected;
  const guidance = typeof value.message === 'string' ? ' · ' + value.message : '';
  showExchangeConnection((exchangeConnected ? `Connected to Bybit · ${value.endpoint || 'verified API endpoint'}${value.keySuffix ? ' · key ending ' + value.keySuffix : ''}` : 'Bybit is disconnected. Configure your connection in Settings → Exchange.') + guidance);
  if (!exchangeConnected) { exchangeMarket = undefined; exchangeWithdrawal = undefined; exchangeQuoteEpoch++; exchangeWithdrawEpoch++; show('exchangeprice', 'Price unknown — connect and refresh the market.'); }
  show('exchangebalances', Array.isArray(value.balances) ? value.balances : 'Balances unknown — refresh a verified market.');
  renderExchangeOperations(value.operations);
}
function exchangeRouteText(market, withdrawal) {
  const route = market.route;
  if (route?.supported !== true) return `Network unavailable: ${route?.reason || 'Bybit has no verified route for this exact wallet asset.'}`;
  return `${market.coin} · ${route.chainType || route.chain} · ${withdrawal ? route.withdrawEnabled === true ? 'Withdrawals enabled' : 'Withdrawals suspended' : route.depositEnabled === true ? 'Deposits enabled' : 'Deposits suspended'}${withdrawal ? ' · minimum ' + (route.minWithdraw || 'unknown') + ' · withdrawal fee ' + (route.withdrawFee || 'unknown') : ' · minimum deposit ' + (route.minDeposit || 'unknown')}${route.reason ? ' · ' + route.reason : ''}`;
}
async function refreshExchangeQuote(withdrawal = false) {
  const selection = exchangeSelection(withdrawal);
  if (!exchangeOpen || vaultMode !== 'ready' || exchangeBusy || !exchangeConnected || !selection) return;
  const epoch = exchangeEpoch, session = sessionEpoch;
  const request = withdrawal ? ++exchangeWithdrawEpoch : ++exchangeQuoteEpoch;
  if (withdrawal) exchangeWithdrawal = undefined; else exchangeMarket = undefined;
  show(withdrawal ? 'exchangewithdrawroute' : 'exchangeprice', 'Querying Bybit — current observation is unknown.');
  renderExchangeControls();
  let value;
  try { value = await window.wallet.exchangeQuote({ ...selection, side: withdrawal ? 'Sell' : $('exchangeside').value }); }
  catch (error) { if (epoch !== exchangeEpoch || session !== sessionEpoch || request !== (withdrawal ? exchangeWithdrawEpoch : exchangeQuoteEpoch)) return; show(withdrawal ? 'exchangewithdrawroute' : 'exchangeprice', 'Unavailable — no complete current exchange observation.'); throw error; }
  if (epoch !== exchangeEpoch || session !== sessionEpoch || request !== (withdrawal ? exchangeWithdrawEpoch : exchangeQuoteEpoch)) return;
  if (value?.accountId !== selection.accountId || value?.assetId !== selection.assetId || typeof value.coin !== 'string' || !value.route || value.side !== (withdrawal ? 'Sell' : $('exchangeside').value)) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
  if (withdrawal) { exchangeWithdrawal = value; show('exchangewithdrawroute', exchangeRouteText(value, true)); }
  else {
    exchangeMarket = value; show('exchangeroute', exchangeRouteText(value, false));
    show('exchangeprice', exchangePriceKnown(value) ? `${value.symbol} · last ${value.lastPrice} USDT · bid ${value.bidPrice || 'unknown'} · ask ${value.askPrice || 'unknown'}\nLast price is an observation; execution uses a relative slippage limit.` : 'No complete spot USDT price observation. Deposits and withdrawals depend on the exact verified network.');
  }
  if (Array.isArray(value.balances)) show('exchangebalances', value.balances);
  renderExchangeControls();
}
async function exchangeMutation(method, body) {
  const connection = method === 'exchangeConnect' || method === 'exchangeDisconnect';
  if ((connection ? !exchangeSettingsVisible() : !exchangeOpen) || vaultMode !== 'ready' || exchangeBusy) return;
  const epoch = exchangeEpoch, session = sessionEpoch;
  exchangeBusy = true; exchangeMarket = undefined; exchangeWithdrawal = undefined; exchangeQuoteEpoch++; exchangeWithdrawEpoch++; renderExchangeControls();
  show('exchangeresult', 'Awaiting review or operation result. An uncertain outcome must be reconciled before another request.');
  show('exchangedetails', ''); $('exchangedetails').hidden = true;
  try {
    await run(async () => {
      let value;
      try { value = await nativeAction(() => window.wallet[method](body)); }
      catch (error) { if (epoch !== exchangeEpoch || session !== sessionEpoch) return; throw error; }
      if (epoch !== exchangeEpoch || session !== sessionEpoch) return;
      const detail = publicExchangeDetails(value);
      show('exchangedetails', detail); $('exchangedetails').hidden = Object.keys(detail).length === 0;
      show('exchangeresult', method === 'exchangeConnect' ? 'Bybit connection checked. Review the current connection and permissions below.' : method === 'exchangeDisconnect' ? 'Bybit credentials disconnected.' : `Request returned ${typeof value?.state === 'string' ? value.state : 'an unverified state'}. Acceptance does not establish a fill, deposit credit or completed withdrawal. Reconcile the original operation.${method === 'exchangeDeposit' ? ' Track this wallet payment in Settings → Journal; Bybit credit remains unverified.' : ''}`);
      exchangeMarket = undefined; exchangeWithdrawal = undefined; exchangeQuoteEpoch++; exchangeWithdrawEpoch++;
      show('exchangeprice', 'Refresh the market before another operation.'); show('exchangewithdrawroute', 'Refresh the withdrawal network before another withdrawal.');
      await refreshExchangeState(true);
    }, true, method === 'exchangeConnect' ? 'Bybit connection checked. Review the connection status and permissions in Settings → Exchange.' : method === 'exchangeDisconnect' ? 'Bybit credentials disconnected.' : 'Request completed. Check the returned state and any limitations.');
  } finally { if (session === sessionEpoch) { exchangeBusy = false; renderExchangeControls(); } }
}
$('exchangeaccount').onchange = () => populateExchangeAssets(false);
$('exchangewithdrawaccount').onchange = () => populateExchangeAssets(true);
$('exchangeasset').onchange = () => invalidateExchangeSelection(false);
$('exchangewithdrawasset').onchange = () => invalidateExchangeSelection(true);
$('exchangeside').onchange = () => invalidateExchangeSelection(false);
$('exchangequote').onclick = () => run(() => refreshExchangeQuote(false), true);
$('exchangewithdrawquote').onclick = () => run(() => refreshExchangeQuote(true), true);
$('exchangerefresh').onclick = () => run(refreshExchangeState, true);
$('exchangesettingsrefresh').onclick = () => run(refreshExchangeState, true);
$('exchangeconnect').onclick = () => exchangeMutation('exchangeConnect', { endpoint: 'https://' + $('exchangeendpoint').value });
$('exchangedisconnect').onclick = () => exchangeConnected ? exchangeMutation('exchangeDisconnect', {}) : undefined;
for (const [button, method, input, withdrawal] of [
  ['exchangedeposit', 'exchangeDeposit', 'exchangedepositamount', false],
  ['exchangeswap', 'exchangeSwap', 'exchangeswapamount', false],
  ['exchangewithdraw', 'exchangeWithdraw', 'exchangewithdrawamount', true]
]) $(button).onclick = () => {
  const selection = exchangeSelection(withdrawal);
  if ($(button).disabled || !selection) return;
  return exchangeMutation(method, { ...selection, amount: $(input).value, ...(method === 'exchangeSwap' ? { side: $('exchangeside').value, slippage: $('exchangeslippage').value } : {}) });
};
function closeWallet(id) {
  if (!walletTabs.includes(id)) return;
  const restoreFocus = activeWalletID === id || $('viewtabs').contains(document.activeElement);
  walletTabs = walletTabs.filter(item => item !== id);
  walletAssets.delete(id);
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
      const tabs = [...$('viewtabs').children].filter(item => !item.disabled), index = tabs.indexOf(button);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length;
      const position = [...$('viewtabs').children].indexOf(tabs[next]);
      tabs[next].click(); $('viewtabs').children[position].focus();
    };
    $('viewtabs').append(button);
    return button;
  }
  if (vaultMode !== 'ready') tab('Home', !settingsOpen, 'onboarding', closeSettings).id = 'hometab';
  for (const index of [...new Set(visible.map(account => account.index))].sort((a, b) => a - b)) {
    tab(accountName(index), !settingsOpen && !exchangeOpen && !activeWalletID && activeIndex === index, 'accountoverview', () => {
      leaveSettings(); leaveExchange();
      activeWalletID = ''; activeIndex = index;
      const first = visible.find(account => account.index === index); $('account').value = first.id;
      select(); renderAccountGroups(); renderTabs(); renderView(); focusActiveTab();
    });
  }
  for (const id of walletTabs) {
    const account = visible.find(account => account.id === id), network = networks.find(network => network.id === account.network);
    const asset = network?.assets.find(asset => asset.id === walletAssets.get(id)) || nativeAsset(network) || network?.assets[0];
    tab(`${network?.name || account.network}${asset ? ' · ' + asset.symbol : ''} · ${accountName(account.index)}`, !settingsOpen && !exchangeOpen && activeWalletID === id, 'walletpage', () => openWallet(id), id);
  }
  const exchange = tab('Exchange', exchangeOpen, 'exchangepage', openExchange);
  exchange.id = 'exchangetab'; exchange.disabled = vaultMode !== 'ready';
  tab('Settings', settingsOpen, 'settingspage', () => openSettings()).id = 'settings';
}
function focusActiveTab() { $('viewtabs').querySelector('[aria-selected="true"]')?.focus(); }
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
    copy.append(copyIcon()); copy.disabled = recoveryRequired;
    if (recoveryRequired) copy.title = 'Finish recovery phrase setup first';
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
function needsDecredConnection(network) { return network?.family === 'decred' && !network.rpc; }
function renderRowBalance(cell, account) {
  const network = networks.find(network => network.id === account.network), asset = nativeAsset(network), entry = rowBalances.get(account.id);
  const matches = entry?.value.filter(balance => balance?.asset === asset?.id) || [];
  const observation = needsDecredConnection(network) ? decredConnectionRequired : !asset ? 'Balance unknown — asset unavailable.' : entry?.scope !== balanceScope(account) || entry.status === 'loading' ? `Loading ${asset.symbol}…` : entry.status === 'failed' ? `${asset.symbol} unavailable — connection query failed.` : balanceText(asset, matches.length === 1 ? matches[0] : undefined);
  cell.title = `${observation} · ${network?.trust || 'Provider verification required.'}`;
  cell.textContent = needsDecredConnection(network) ? 'Connection required' : !asset ? 'Unknown' : entry?.scope !== balanceScope(account) || entry.status === 'loading' ? 'Loading…' : entry.status === 'failed' ? 'Unavailable' : balanceAmount(asset, matches.length === 1 ? matches[0] : undefined) ?? 'Unknown';
}
function updateRowBalance(account) {
  for (const cell of $('accountgroups').querySelectorAll('.walletbalance')) if (cell.dataset.accountId === account.id) renderRowBalance(cell, account);
}
function queueVisibleBalances(force = false) {
  if (vaultMode !== 'ready' || activeWalletID || exchangeOpen || settingsOpen) return;
  for (const account of visibleAccounts().filter(account => account.index === activeIndex)) {
    if (needsDecredConnection(networks.find(network => network.id === account.network))) { rowBalances.delete(account.id); updateRowBalance(account); continue; }
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
      if (epoch !== sessionEpoch || vaultMode !== 'ready' || activeWalletID || exchangeOpen || settingsOpen || account.index !== activeIndex || rowBalances.get(account.id) !== entry || needsDecredConnection(networks.find(network => network.id === account.network))) {
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
  leaveSettings(); leaveExchange();
  activeWalletID = id; activeIndex = account.index; $('account').value = id;
  if (!walletTabs.includes(id)) walletTabs.push(id);
  select(); renderTabs(); renderView(); focusActiveTab();
  return run(async () => { await Promise.all([loadBalance(), history()]); }, true);
}
async function readAccountPages() {
  const epoch = sessionEpoch;
  let state = await window.wallet.accounts();
  if (epoch !== sessionEpoch || state.locked !== false) return state;
  if (!Array.isArray(state.accounts) || !Number.isInteger(state.totalAccounts) || state.totalAccounts < 0 || state.totalAccounts > 10000 || typeof state.revision !== 'string' || !state.revision || !state.accountsCursor && state.accounts.length !== state.totalAccounts) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
  if (!state.accountsCursor) return state;
  const first = state, collected = [...(state.accounts || [])], seen = new Set();
  let bytes = JSON.stringify(collected).length;
  while (state.accountsCursor) {
    if (typeof state.accountsCursor !== 'string' || !/^[0-9a-f]{32}$/.test(state.accountsCursor) || seen.has(state.accountsCursor) || seen.size >= 256) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
    seen.add(state.accountsCursor);
    state = await window.wallet.accounts({ cursor: state.accountsCursor });
    if (epoch !== sessionEpoch) return { locked: true };
    if (state.locked !== false || state.revision !== first.revision || state.totalAccounts !== first.totalAccounts || !Array.isArray(state.accounts)) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
    bytes += JSON.stringify(state.accounts).length;
    if (collected.length + state.accounts.length > 10000 || bytes > 16777216) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
    collected.push(...state.accounts);
  }
  if (collected.length !== first.totalAccounts || new Set(collected.map(account => account.id)).size !== collected.length) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
  return { ...first, accounts: collected, accountsCursor: '' };
}
async function refresh(queryActive = true) {
  const epoch = sessionEpoch;
  const state = await readAccountPages();
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
  vaultMode = 'ready'; lastLockedMode = undefined; recoveryRequired = state.recoveryConfirmed !== true; renderGate();
  $('walletrecoverynotice').hidden = !recoveryRequired;
  $('receive').disabled = false; $('sendview').disabled = false;
  for (const id of ['copy', 'copywalletaddress', 'saveqr', 'prepare', 'max']) $(id).disabled = recoveryRequired;
  $('sendrecoverynotice').hidden = !recoveryRequired;
  $('recoverycallout').hidden = !recoveryRequired; $('recoverysettings').hidden = !recoveryRequired;
  show('recoverystatus', recoveryRequired ? 'Recovery phrase setup required' : 'Recovery phrase recorded');
  populateAccounts($('account').value);
  if (activeWalletID && queryActive) await Promise.all([loadBalance(), history()]);
}
function current() { return accounts.find(account => account.id === $('account').value); }
function select() {
  viewEpoch++;
  closeReceiveRecovery();
  $('sendpanel').hidden = true;
  const account = current();
  $('asset').replaceChildren();
  show('address', account ? account.address : 'Locked or no account');
  $('address').title = account?.address || '';
  renderWalletAddress(account);
  balances = []; operations = [];
  show('balance', 'Balance unknown — query required.'); $('balance').removeAttribute('title'); $('gasbalance').hidden = true;
  show('preview', 'No payment prepared.');
  show('chainhistoryresult', 'History has not been queried.'); $('operations').replaceChildren();
  for (const id of ['recipient', 'amount', 'memo', 'associateaddress']) $(id).value = '';
  $('hideaccount').textContent = account?.hidden ? 'Unhide selected' : 'Hide selected';
  prepared = undefined; $('send').disabled = true;
  const network = networks.find(network => network.id === account?.network);
  show('wallettitle', network?.name || account?.network || 'Network wallet');
  show('walletsubtitle', account ? `${accountName(account.index)} · ${account.network}` : 'No account selected');
  show('connectionstatus', needsDecredConnection(network) ? decredConnectionRequired : network ? `${network.rpc ? 'RPC: ' + network.rpc + ' · ' : ''}${network.trust || 'Provider verification required.'}` : 'Connection details unavailable.');
  if (needsDecredConnection(network)) { show('balance', 'Connection required'); $('balance').title = decredConnectionRequired; }
  loadDiscovery(network?.family === 'decred' ? account : null);
  $('decrednote').hidden = network?.family !== 'decred'; $('dcrdiscoverycontrols').hidden = network?.family !== 'decred';
  $('associationcontrols').hidden = !['near', 'aptos'].includes(network?.family);
  $('associate').hidden = $('associationcontrols').hidden;
  show('diagnostics', { account: account || null, network: network || null });
  for (const asset of network?.assets || []) {
    const option = document.createElement('option');
    option.value = asset.id; option.textContent = assetLabel(asset, network); $('asset').append(option);
  }
  const remembered = walletAssets.get(account?.id), selected = network?.assets.find(asset => asset.id === remembered) || nativeAsset(network) || network?.assets[0];
  if (selected) { $('asset').value = selected.id; walletAssets.set(account.id, selected.id); }
  renderWalletAssets();
  populateJournal(account?.id, $('asset').value);
}
function renderWalletAddress(account) {
  show('walletaddress', account?.address || ''); $('walletaddress').title = account?.address || '';
  $('copywalletaddress').disabled = !account || recoveryRequired;
}
function renderWalletAssets() {
  const account = current(), network = networks.find(network => network.id === account?.network), assets = network?.assets || [];
  $('walletassets').replaceChildren(); $('walletassets').hidden = assets.length < 2;
  const asset = assets.find(item => item.id === $('asset').value);
  if (asset) walletAssets.set(account.id, asset.id);
  show('wallettitle', `${network?.name || account?.network || 'Network wallet'}${asset ? ' · ' + asset.symbol : ''}`);
  show('amountunit', asset ? `1 = 1 ${asset.symbol}` : 'Choose a wallet asset.');
  for (const item of assets) {
    const button = document.createElement('button');
    button.textContent = item.symbol; button.dataset.assetId = item.id; button.title = `${assetLabel(item, network)} · ${item.id}`;
    button.setAttribute('role', 'tab'); button.setAttribute('aria-selected', String(item.id === asset?.id)); button.setAttribute('aria-controls', 'walletassetpanel'); button.tabIndex = item.id === asset?.id ? 0 : -1;
    button.onclick = () => changeWalletAsset(item.id);
    button.onkeydown = event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      const index = assets.indexOf(item), next = event.key === 'Home' ? 0 : event.key === 'End' ? assets.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : assets.length - 1)) % assets.length;
      changeWalletAsset(assets[next].id);
    };
    $('walletassets').append(button);
  }
}
function changeWalletAsset(id) {
  if (!activeWalletID || $('asset').value === id) return;
  $('asset').value = id; $('asset').dispatchEvent(new Event('input'));
  [...$('walletassets').children].find(button => button.dataset.assetId === id)?.focus();
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
function balanceAmount(asset, balance) {
  if (!balance || typeof balance.atomic !== 'string' || !/^(0|[1-9][0-9]*)$/.test(balance.atomic) || typeof balance.state !== 'string' || !balance.state || /unknown|unavailable|synchroniz|stale|pending|error|failed|incomplete/i.test(balance.state) || typeof balance.at !== 'string' || !balance.at) return undefined;
  const padded = balance.atomic.padStart(asset.decimals + 1, '0');
  const value = asset.decimals ? padded.slice(0, -asset.decimals) + '.' + padded.slice(-asset.decimals) : padded;
  return value.replace(/(\.[0-9]*?)0+$/, '$1').replace(/\.$/, '');
}
function balanceText(asset, balance) {
  const value = balanceAmount(asset, balance);
  if (value === undefined) return 'Balance unknown — no complete network observation.';
  const state = balance.state === 'NOT_CREATED (final state)' ? 'Account not created (final state) — receive NEAR to activate' : balance.state;
  return `${value} ${asset.symbol} · ${state} · ${balance.at}`;
}
function renderBalance() {
  const network = networks.find(network => network.id === current()?.network), asset = network?.assets.find(asset => asset.id === $('asset').value);
  if (needsDecredConnection(network)) { show('balance', 'Connection required'); $('balance').title = decredConnectionRequired; $('gasbalance').hidden = true; return; }
  const balance = balances.find(balance => balance.asset === asset?.id), value = asset && balanceAmount(asset, balance);
  show('balance', value === undefined ? 'Balance unknown — no complete network observation.' : `${value} ${asset.symbol}`);
  $('balance').title = asset ? balanceText(asset, balance) : 'Balance unknown — no asset selected.';
  const gas = network?.assets.find(item => item.id.endsWith(':native'));
  $('gasbalance').hidden = !asset?.contract || !gas;
  if (asset?.contract && gas) {
    const balance = balances.find(balance => balance.asset === gas.id), value = balanceAmount(gas, balance);
    show('gasbalance', value === undefined ? 'Gas balance unknown' : `Gas balance: ${value} ${gas.symbol}`); $('gasbalance').title = balanceText(gas, balance);
  }
}
async function loadBalance() {
  const epoch = viewEpoch, session = sessionEpoch, account = current();
  if (needsDecredConnection(networks.find(network => network.id === account?.network))) {
    balances = []; rowBalances.delete(account.id); updateRowBalance(account);
    show('balance', 'Connection required'); $('balance').title = decredConnectionRequired; $('gasbalance').hidden = true; return;
  }
  const entry = account && { scope: balanceScope(account), status: 'loading', value: [] };
  if (entry) rowBalances.set(account.id, entry);
  balances = []; show('balance', 'Balance unknown — querying the selected network…'); $('balance').removeAttribute('title'); $('gasbalance').hidden = true;
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
  journalEpoch++; resetJournalPages(); $('journalasset').replaceChildren();
  const account = journalAccount(), network = networks.find(network => network.id === account?.network);
  for (const asset of network?.assets || []) { const option = document.createElement('option'); option.value = asset.id; option.textContent = `${asset.symbol} · ${asset.id}`; $('journalasset').append(option); }
  if (network?.assets.some(asset => asset.id === selected)) $('journalasset').value = selected;
  show('journalcontext', account ? `${accountName(account.index)} · ${account.network} · ${$('journalasset').value}` : 'Choose a wallet to inspect its payments.');
  $('history').disabled = !account; $('chainhistory').disabled = !account;
  resetChainHistory(); show('chainhistoryresult', 'History has not been queried for this selection.'); renderHistory();
  show('journalresult', 'No journal action requested.');
}
async function journalResult(method, body, apply) {
  const epoch = journalEpoch, session = sessionEpoch;
  let value;
  try { value = await window.wallet[method](body); }
  catch (error) { if (epoch !== journalEpoch || session !== sessionEpoch) return; throw error; }
  if (epoch === journalEpoch && session === sessionEpoch) await apply(value);
}
function resetJournalPages() {
  journalRequestEpoch++; journalCursor = ''; journalBytes = 0; operations = []; $('operations').replaceChildren();
  $('historymore').hidden = true; $('historymore').disabled = false; show('historystatus', '');
}
async function history(older = false) {
  if (!journalAccount()) { resetJournalPages(); renderHistory(); return; }
  const cursor = journalCursor;
  if (older && (!cursor || $('historymore').disabled)) return;
  if (!older) resetJournalPages();
  const requestEpoch = journalRequestEpoch;
  $('historymore').disabled = true;
  try {
    await journalResult('history', { accountId: journalAccount()?.id || '', asset: $('journalasset').value, ...(older ? { cursor } : {}) }, value => {
      if (requestEpoch !== journalRequestEpoch) return;
      if (!Array.isArray(value) && (!Array.isArray(value?.operations) || typeof value.complete !== 'boolean' || value.complete && value.nextCursor || !value.complete && !/^[0-9a-f]{32}$/.test(value.nextCursor))) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
      const rows = Array.isArray(value) ? value : value.operations;
      const bytes = JSON.stringify(rows).length;
      const seen = new Set(operations.map(operation => operation.plan.id));
      if (bytes > 262144 || journalBytes + bytes > 16777216 || operations.length + rows.length > 10000 || rows.some(operation => { const id = operation?.plan?.id; if (!id || seen.has(id)) return true; seen.add(id); return false; })) throw new Error('REQUEST_FAILED_STATE_UNKNOWN');
      operations.push(...rows); journalBytes += bytes;
      journalCursor = typeof value?.nextCursor === 'string' && /^[0-9a-f]{32}$/.test(value.nextCursor) ? value.nextCursor : '';
      $('historymore').hidden = !journalCursor; $('historymore').disabled = false;
      show('historystatus', journalCursor ? `${operations.length} local payments loaded. Older payments are available.` : `${operations.length} local payments loaded. Local journal scope complete; it does not reconstruct missing on-chain history.`);
      renderHistory();
    });
  } catch (error) { if (requestEpoch !== journalRequestEpoch) return; journalCursor = ''; $('historymore').hidden = true; show('historystatus', 'Journal query failed or generation changed. Refresh the journal; reservations remain in Go.'); throw error; }
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
function exchangeSettingsVisible() { return settingsOpen && settingsSection === 'exchange'; }
function showExchangeConnection(message) { show('exchangeconnection', message); show('exchangesettingsconnection', message); }
function selectSettingsTab(section) {
  const wasExchange = exchangeSettingsVisible();
  if (section !== settingsSection) viewEpoch++;
  settingsSection = section;
  for (const name of ['wallet', 'exchange', 'journal']) {
    const selected = section === name, tab = $('settings' + name + 'tab');
    tab.setAttribute('aria-selected', String(selected)); tab.tabIndex = selected ? 0 : -1;
    $('settings' + name + 'panel').hidden = !selected;
  }
  if (wasExchange && section !== 'exchange' && !exchangeOpen) { exchangeEpoch++; exchangeStateEpoch++; }
  renderExchangeControls();
  if (section === 'exchange') return run(refreshExchangeState, true);
  if (section === 'journal' && vaultMode === 'ready') return run(history, true);
}
function leaveSettings() {
  if (settingsOpen) viewEpoch++;
  if (exchangeSettingsVisible()) { exchangeEpoch++; exchangeStateEpoch++; }
  settingsOpen = false;
  renderExchangeControls();
}
function closeSettings() {
  leaveSettings(); renderTabs(); renderView();
  renderExchangeControls();
}
function openSettings(section = 'wallet') {
  closeReceiveRecovery();
  leaveExchange(); settingsOpen = true; viewEpoch++;
  prepared = undefined; $('send').disabled = true; $('sendpanel').hidden = true;
  renderTabs(); renderView(); focusActiveTab();
  return selectSettingsTab(section);
}
const settingsSections = ['wallet', 'exchange', 'journal'];
for (const [index, section] of settingsSections.entries()) {
  const tab = $('settings' + section + 'tab');
  tab.onclick = () => selectSettingsTab(section);
  tab.onkeydown = event => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const target = event.key === 'Home' ? 'wallet' : event.key === 'End' ? 'journal' : settingsSections[(index + (event.key === 'ArrowRight' ? 1 : 2)) % settingsSections.length];
    $('settings' + target + 'tab').focus();
    return selectSettingsTab(target);
  };
}
const credentialActions = [
  ['unlock', 'unlock', {}], ['create', 'create', { words: 12 }], ['create24', 'create', { words: 24 }],
  ['import', 'import', { words: 12 }], ['recoverysettings', 'confirmRecovery', {}], ['backupphrase', 'backupPhrase', {}], ['password', 'password', {}],
  ['onboardcreate', 'create', { words: 12 }], ['onboardrestore', 'import', { words: 12 }], ['onboardunlock', 'unlock', {}], ['recoverycontinue', 'confirmRecovery', {}]
];
let credentialPending = false;
for (const [id, method, body] of credentialActions) $(id).onclick = async () => {
  if (credentialPending || method === 'confirmRecovery' && (vaultMode !== 'ready' || !recoveryRequired) || method === 'backupPhrase' && (vaultMode !== 'ready' || recoveryRequired)) return;
  if (method !== 'backupPhrase') closeSettings();
  credentialPending = true;
  for (const [button] of credentialActions) $(button).disabled = true;
  try { await run(async () => {
    const epoch = sessionEpoch;
    try { await window.wallet[method](body); }
    catch (error) {
      // Go may have durably created a pending root before a wrong-word
      // challenge. Show that existing wallet before reporting the fixed error;
      // never offer replacement creation or mistake this for completed setup.
      if (error?.message === 'RECOVERY_WORDS_MISMATCH' && epoch === sessionEpoch) await refresh(false);
      throw error;
    }
    if (epoch === sessionEpoch) await refresh();
  }); }
  finally { credentialPending = false; for (const [button] of credentialActions) $(button).disabled = false; void drainBalanceQueue(); }
};
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
  // Go supplies the verified QR/address to the dedicated wallet dialog. A committed
  // Decred receive index must still refresh the overview after a tab switch.
  if (epoch !== sessionEpoch || !value.account || value.account.id !== id || !accounts.some(account => account.id === id)) return;
  accounts = accounts.map(account => account.id === id ? value.account : account); renderAccountGroups();
  if (current()?.id === id) { show('address', value.account.address); $('address').title = value.account.address; renderWalletAddress(value.account); show('diagnostics', { account: value.account, network: networks.find(network => network.id === value.account.network) }); }
  });
}
async function nativeAction(action) {
  nativeActionsPending++;
  try { return await action(); }
  finally { nativeActionsPending--; void drainBalanceQueue(); }
}
function closeReceiveRecovery() {
  const wasOpen = !$('receiverecoverydialog').hidden;
  receiveIntent = undefined; $('receiverecoverydialog').hidden = true;
  if (wasOpen) $('receive').focus();
}
$('receive').onclick = () => {
  if (recoveryRequired) {
    closeSettings(); receiveIntent = { id: current()?.id || '', view: viewEpoch, session: sessionEpoch };
    show('receiverecoverycontext', `${$('wallettitle').textContent} · ${accountName(activeIndex)}`);
    $('receiverecoverydialog').hidden = false; $('receiverecovery').focus();
    return;
  }
  return run(() => receiveWallet(current()?.id || ''), true);
};
$('closereceiverecovery').onclick = () => { closeReceiveRecovery(); void drainBalanceQueue(); };
$('receiverecoverydialog').onkeydown = event => {
  if (event.key === 'Escape') { event.preventDefault(); $('closereceiverecovery').click(); }
};
async function recoveryForWorkflow(receive = false) {
  if (credentialPending || vaultMode !== 'ready' || !recoveryRequired) return;
  const intent = receive ? receiveIntent : { id: current()?.id || '', view: viewEpoch, session: sessionEpoch };
  if (!intent) return;
  const draft = ['recipient', 'amount', 'memo'].map(id => $(id).value), assetID = $('asset').value, sendOpen = !$('sendpanel').hidden;
  closeReceiveRecovery(); credentialPending = true;
  $('sendrecovery').disabled = true; $('receiverecovery').disabled = true;
  try {
    await run(async () => {
      await window.wallet.confirmRecovery({});
      if (intent.session !== sessionEpoch) return;
      const unchanged = intent.view === viewEpoch && activeWalletID === intent.id;
      await refresh(false);
      if (!unchanged || viewEpoch !== intent.view + 1 || intent.session !== sessionEpoch || activeWalletID !== intent.id || vaultMode !== 'ready') return;
      if (receive && !recoveryRequired) await receiveWallet(intent.id);
      else if (!receive && sendOpen) { $('sendpanel').hidden = false; if ([...$('asset').options].some(option => option.value === assetID)) $('asset').value = assetID; renderWalletAssets(); renderTabs(); renderBalance(); populateJournal(intent.id, assetID); ['recipient', 'amount', 'memo'].forEach((id, index) => { $(id).value = draft[index]; }); $('recipient').focus(); }
    });
  } finally { credentialPending = false; $('sendrecovery').disabled = false; $('receiverecovery').disabled = false; void drainBalanceQueue(); }
}
$('receiverecovery').onclick = () => recoveryForWorkflow(true);
$('sendrecovery').onclick = () => recoveryForWorkflow();
$('sendview').onclick = () => { if (!activeWalletID) return; $('sendpanel').hidden = false; $('recipient').focus(); };
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
function startPrepare(method) { if ($('sendpanel').hidden || recoveryRequired) return; viewEpoch++; return run(() => prepare(method), true); }
$('prepare').onclick = () => startPrepare('prepare');
$('max').onclick = () => startPrepare('max');
$('send').onclick = () => run(async () => { if ($('sendpanel').hidden || !prepared) return; const id = prepared.id; prepared = undefined; $('send').disabled = true; await result('send', { id }, async value => { show('preview', value); await history(); }); }, true);
$('history').onclick = () => run(() => history(), true);
$('historymore').onclick = () => run(() => history(true), true);
$('journalaccount').onchange = () => { populateJournalAssets(); return run(history, true); };
$('journalasset').onchange = () => { journalEpoch++; resetJournalPages(); show('journalcontext', `${accountName(journalAccount().index)} · ${journalAccount().network} · ${$('journalasset').value}`); show('journalresult', 'No journal action requested.'); resetChainHistory(); show('chainhistoryresult', 'History has not been queried for this selection.'); renderHistory(); };
for (const id of ['recipient', 'amount', 'memo', 'asset']) $(id).addEventListener('input', () => { $('send').disabled = true; prepared = undefined; viewEpoch++; show('preview', 'No payment prepared. Earlier unsigned reservations remain in the journal until cancelled.'); if (id === 'asset') { renderWalletAssets(); renderTabs(); renderBalance(); populateJournal(current()?.id, $('asset').value); void run(() => history(), true); } });
async function addressAction(method, id) {
  return nativeAction(async () => {
  const epoch = sessionEpoch, account = accounts.find(account => account.id === id);
  await window.wallet[method]({ accountId: id });
  // Decred commits a new receive index before copying/saving. Refresh only that
  // descriptor without clearing a payment form or selecting another wallet.
  if (epoch !== sessionEpoch || networks.find(network => network.id === account?.network)?.family !== 'decred') return;
  const state = await readAccountPages();
  if (epoch !== sessionEpoch) return;
  if (state.locked !== false) { clearLocked(); return; }
  const fresh = state.accounts?.find(item => item.id === id);
  if (!fresh) return;
  accounts = accounts.map(item => item.id === id ? fresh : item); renderAccountGroups();
  if (current()?.id === id) { show('address', fresh.address); $('address').title = fresh.address; renderWalletAddress(fresh); show('diagnostics', { account: fresh, network: networks.find(network => network.id === fresh.network) }); }
  });
}
$('copy').onclick = () => run(() => addressAction('copyReceive', current()?.id || ''), true);
$('copywalletaddress').onclick = () => recoveryRequired || vaultMode !== 'ready' || !activeWalletID ? undefined : run(() => addressAction('copyReceive', current()?.id || ''), true);
$('saveqr').onclick = () => run(() => addressAction('saveQR', current()?.id || ''), true);
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
function resetChainHistory() {
  chainRequestEpoch++; chainHistoryCursor = ''; chainHistoryEntries = []; chainHistoryBytes = 0;
  $('chainhistorymore').hidden = true; $('chainhistorymore').disabled = false;
  show('chainhistorystatus', '');
}
async function chainHistory(older) {
  const cursor = chainHistoryCursor;
  if (older && (!cursor || $('chainhistorymore').disabled)) return;
  if (!older) resetChainHistory();
  const requestEpoch = chainRequestEpoch;
  $('chainhistorymore').disabled = true;
  try {
    await journalResult('chainHistory', { accountId: journalAccount()?.id || '', ...(older ? { cursor } : {}) }, value => {
      if (requestEpoch !== chainRequestEpoch) return;
      // Public responses are inert text. Never turn a provider token into a URL
      // or execute content; only a bounded coordinator capability can continue.
      const entries = Array.isArray(value?.entries) ? value.entries : [];
      const encoded = JSON.stringify(value);
      if (encoded.length > 262144 || chainHistoryBytes + encoded.length > 4194304 || chainHistoryEntries.length + entries.length > 10000) {
        resetChainHistory(); show('chainhistoryresult', 'History display reached its bound. Refresh to start again; older history remains unresolved.'); return;
      }
      chainHistoryEntries.push(...entries); chainHistoryBytes += encoded.length;
      chainHistoryCursor = typeof value?.nextCursor === 'string' && /^[0-9a-f]{32}$/.test(value.nextCursor) ? value.nextCursor : '';
      show('chainhistoryresult', { ...(value?.report ? { report: value.report } : { entries: chainHistoryEntries }), scope: value?.scope || 'Provider history scope unavailable; completeness is unknown.' });
      show('chainhistorystatus', chainHistoryCursor ? `${chainHistoryEntries.length} observations loaded. Older history is available.` : value?.complete === true ? `${chainHistoryEntries.length} observations loaded. End of the disclosed provider scope; this does not prove payment finality or zero funds.` : `${chainHistoryEntries.length} observations loaded. Bounded snapshot; older history remains unresolved.`);
      $('chainhistorymore').hidden = !chainHistoryCursor; $('chainhistorymore').disabled = false;
    });
  } catch (error) {
    if (requestEpoch !== chainRequestEpoch) return;
    chainHistoryCursor = ''; $('chainhistorymore').hidden = true;
    show('chainhistorystatus', 'History query failed or continuation expired. Refresh history; no missing observation has been treated as zero or failed payment.');
    throw error;
  }
}
$('chainhistory').onclick = () => run(() => chainHistory(false), true);
$('chainhistorymore').onclick = () => run(() => chainHistory(true), true);
$('discovery').onclick = () => run(() => committedMetadata('discovery', { accountId: current()?.id || '', birthday: Number($('birthday').value), gap: Number($('gap').value), limit: Number($('limit').value), blockBudget: Number($('blockbudget').value) }));
renderGate();
run(refresh);
