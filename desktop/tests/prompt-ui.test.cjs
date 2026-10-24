'use strict';
// Synthetic secrets only. These component checks exercise the actual isolated
// prompt document; packaged tests establish the Electron/display boundary.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {JSDOM} = require('jsdom');
const root = path.resolve(__dirname, '..');
const id = '0123456789abcdef'.repeat(2);
const fixturePhrase = 'abandon '.repeat(11) + 'about';
const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return {promise, resolve, reject}; }
async function harness(prompt, options = {}) {
  const dom = new JSDOM(fs.readFileSync(path.join(root, 'ui/prompt.html'), 'utf8'), {url:'wallet://prompt/', runScripts:'outside-only', pretendToBeVisual:true});
  const calls = [], copyCalls = [];
  const $ = name => dom.window.document.getElementById(name);
  const bridge = {
    getPrompt: () => options.get ? options.get() : Promise.resolve(prompt),
    respond(reply) { calls.push(structuredClone(reply)); if (options.respond) return options.respond(reply, $); return Promise.resolve(); },
    copyAddress(...args) { copyCalls.push(args); return options.copy ? options.copy() : Promise.resolve(); }
  };
  dom.window.walletPrompt = Object.freeze(bridge);
  new vm.Script(fs.readFileSync(path.join(root, 'ui/prompt.js'), 'utf8'), {filename:path.join(root, 'ui/prompt.js')}).runInContext(dom.getInternalVMContext());
  await tick();
  const event = (type, init = {}) => new dom.window.Event(type, {bubbles:true, cancelable:true, ...init});
  return {dom, $, calls, copyCalls, async submit() { $('promptform').dispatchEvent(event('submit')); await tick(); }, async cancel() { $('cancel').click(); await tick(); }, key(key, init = {}) { const value = new dom.window.KeyboardEvent('keydown', {key, bubbles:true, cancelable:true, ...init}); dom.window.document.dispatchEvent(value); return value; }, event, close() { dom.window.close(); }};
}
function cleared(h, retainedInputs = []) {
  assert.equal(h.$('inputfields').children.length, 0);
  assert.equal(h.$('recoverywords').children.length, 0);
  assert.equal(h.$('summary').textContent, '');
  assert.equal(h.$('receivedomain').textContent, '');
  assert.equal(h.$('receiveaddress').textContent, '');
  assert.equal(h.$('receiveqr').hasAttribute('src'), false);
  assert.equal(h.$('accept').disabled, true);
  assert.equal(h.$('cancel').disabled, true);
  for (const input of retainedInputs) assert.equal(input.value, '');
}
test('TP033 dedicated prompt is local-only, has no wallet API, storage, reveal or secret export actions', async t => {
  const h = await harness({id, kind:'recovery', title:'Record recovery phrase', phrase:fixturePhrase}); t.after(h.close);
  assert.equal(h.dom.window.wallet, undefined);
  assert.equal(h.dom.window.require, undefined);
  assert.equal(h.dom.window.process, undefined);
  const source = fs.readFileSync(path.join(root, 'ui/prompt.js'), 'utf8');
  assert.ok(!/localStorage|sessionStorage|indexedDB|fetch\(|XMLHttpRequest|console\./.test(source));
  assert.equal(h.dom.window.document.querySelectorAll('input,a,iframe,webview').length, 0);
  assert.equal(h.$('receive').hidden, true);
  assert.equal(h.$('copyaddress').closest('[hidden]'), h.$('receive'));
  assert.equal(h.$('receiveqr').hasAttribute('src'), false);
  const csp = h.dom.window.document.querySelector('meta[http-equiv="Content-Security-Policy"]').content;
  for (const rule of ["default-src 'none'", "connect-src 'none'", "form-action 'none'", "frame-src 'none'", "worker-src 'none'"]) assert.ok(csp.includes(rule));
  assert.ok(!csp.includes('unsafe-inline'));
  const css = fs.readFileSync(path.join(root, 'ui/prompt.css'), 'utf8');
  assert.match(css, /recovery-words[^}]*user-select:none/);
  assert.match(css, /@media print\{body\{display:none!important/);
});
test('TP033 masked inputs preserve exact text and are cleared before the scoped response crosses the bridge', async t => {
  let beforeResponse = false;
  const h = await harness({id, kind:'input', title:'Create local wallet — set app password', labels:['App password', 'Repeat app password']}, {respond(reply, $) {
    beforeResponse = true;
    assert.equal($('inputfields').children.length, 0);
    assert.equal($('accept').disabled, true);
    assert.deepEqual(structuredClone(reply), {promptId:id, accepted:true, values:['  synthetic-password-✓  ', '  synthetic-password-✓  ']});
    return Promise.resolve();
  }}); t.after(h.close);
  const inputs = [...h.$('inputfields').querySelectorAll('input')];
  assert.equal(inputs.length, 2);
  assert.equal(h.dom.window.document.activeElement, inputs[0]);
  for (const input of inputs) {
    assert.equal(input.type, 'password'); assert.equal(input.autocomplete, 'off'); assert.equal(input.spellcheck, false);
    assert.equal(input.getAttribute('autocorrect'), 'off'); assert.equal(input.getAttribute('autocapitalize'), 'none');
    input.value = '  synthetic-password-✓  ';
  }
  await h.submit(); assert.equal(beforeResponse, true); cleared(h, inputs);
  await h.submit(); await h.cancel(); assert.equal(h.calls.length, 1);
});
test('TP033 empty input remains a Go policy decision and all eight permitted fields stay masked', async t => {
  const labels = Array.from({length:8}, (_, i) => 'Input ' + i);
  const h = await harness({id, kind:'input', title:'Wallet input', labels}); t.after(h.close);
  assert.equal(h.$('inputfields').querySelectorAll('input[type=password]').length, 8);
  await h.submit(); assert.deepEqual(h.calls, [{promptId:id, accepted:true, values:Array(8).fill('')}]);
});
test('TP033 cancelling or abandoning any input clears retained nodes and never returns their values', async t => {
  for (const action of ['cancel', 'escape', 'hidden', 'pagehide', 'beforeunload']) {
    const h = await harness({id, kind:'input', title:'Unlock local wallet', labels:['App password']}); t.after(h.close);
    const input = h.$('promptinput0'); input.value = 'synthetic-do-not-return';
    if (action === 'cancel') await h.cancel();
    else if (action === 'escape') { assert.equal(h.key('Escape').defaultPrevented, true); await tick(); }
    else if (action === 'hidden') { Object.defineProperty(h.dom.window.document, 'visibilityState', {value:'hidden'}); h.dom.window.document.dispatchEvent(h.event('visibilitychange')); await tick(); }
    else { h.dom.window.dispatchEvent(h.event(action)); await tick(); }
    cleared(h, [input]); assert.deepEqual(h.calls, [{promptId:id, accepted:false}], action);
    h.dom.window.dispatchEvent(h.event('pagehide')); await tick(); assert.equal(h.calls.length, 1);
  }
});
test('TP033 phrase acknowledgement has no phrase values and clears all displayed words before replying', async t => {
  for (const words of [12, 15, 18, 21, 24]) {
    const h = await harness({id, kind:'recovery', title:'Record recovery phrase', phrase:Array(words).fill('abandon').join(' ')}, {respond(_reply, $) { assert.equal($('recoverywords').children.length, 0); return Promise.resolve(); }}); t.after(h.close);
    assert.equal(h.$('recoverywords').children.length, words);
    assert.deepEqual([...h.$('recoverywords').children].map(item => item.textContent), Array(words).fill('abandon'));
    assert.equal(h.$('accept').textContent, 'I wrote it down');
    assert.equal(h.dom.window.document.activeElement, h.$('cancel'));
    await h.submit(); cleared(h); assert.deepEqual(h.calls, [{promptId:id, accepted:true}]);
  }
});
test('TP033 recording phase blocks copy, cut, drag, context menu, print and save shortcuts without affecting other prompts', async t => {
  const h = await harness({id, kind:'recovery', title:'Record recovery phrase', phrase:fixturePhrase}); t.after(h.close);
  for (const type of ['copy', 'cut', 'contextmenu', 'dragstart']) { const event = h.event(type); h.$('recoverywords').dispatchEvent(event); assert.equal(event.defaultPrevented, true); }
  for (const key of ['c', 'C', 'x', 'p', 's']) for (const modifier of ['ctrlKey', 'metaKey']) assert.equal(h.key(key, {[modifier]:true}).defaultPrevented, true);
  assert.equal(h.key('a', {ctrlKey:true}).defaultPrevented, false); assert.equal(h.key('c').defaultPrevented, false);
  await h.cancel(); assert.deepEqual(h.calls, [{promptId:id, accepted:false}]); cleared(h);
  const notice = await harness({id, kind:'notice', title:'Wallet notice', summary:'Public information'}); t.after(notice.close);
  for (const type of ['copy', 'cut', 'contextmenu', 'dragstart']) { const event = notice.event(type); notice.dom.window.document.dispatchEvent(event); assert.equal(event.defaultPrevented, false); }
  assert.equal(notice.key('p', {ctrlKey:true}).defaultPrevented, false);
});
test('TP042 transaction confirmation displays only inert backend details and never signs automatically', async t => {
  const dangerous = '<img src=x onerror="globalThis.compromised=true">';
  const h = await harness({id, kind:'confirm', title:dangerous, summary:'Network: Ethereum\nRecipient: public\nMaximum fee: 0.01 ETH\n' + dangerous}); t.after(h.close);
  assert.equal(h.$('prompttitle').textContent, dangerous);
  assert.ok(h.$('summary').textContent.includes(dangerous)); assert.equal(h.$('summary').children.length, 0);
  assert.equal(h.dom.window.document.querySelectorAll('img').length, 1); assert.equal(h.dom.window.compromised, undefined);
  assert.equal(h.calls.length, 0); assert.equal(h.$('accept').textContent, 'Confirm');
  await h.submit(); assert.deepEqual(h.calls, [{promptId:id, accepted:true}]); cleared(h);
  const input = await harness({id, kind:'input', title:'Wallet input', labels:[dangerous]}); t.after(input.close);
  assert.equal(input.$('inputfields').querySelector('label').childNodes[0].textContent, dangerous);
  assert.equal(input.$('inputfields').querySelector('img'), null);
});
test('TP041 notice has one Close action and a fixed sanitized unavailable state on failed dispatch', async t => {
  const h = await harness({id, kind:'notice', title:'Wallet notice', summary:'Recovery phrase not confirmed'}, {respond:() => Promise.reject(Error('CANARY-PRIVATE-ERROR'))}); t.after(h.close);
  assert.equal(h.$('cancel').hidden, true); assert.equal(h.$('accept').textContent, 'Close');
  assert.equal(h.dom.window.document.activeElement, h.$('accept'));
  await h.submit(); cleared(h); assert.equal(h.$('prompterror').hidden, false);
  assert.match(h.$('prompterror').textContent, /no longer available/); assert.ok(!h.dom.window.document.body.textContent.includes('CANARY'));
  assert.equal(h.dom.window.document.activeElement, h.$('prompterror'));
});
test('TP061 receive uses only the backend address and QR with a scoped argument-free copy action', async t => {
  const h = await harness({id, kind:'receive', title:'Receive ETH', summary:'Ethereum mainnet · ETH', address:'0x' + 'a'.repeat(40), png:'AAAA'}); t.after(h.close);
  assert.equal(h.$('receiveaddress').textContent, '0x' + 'a'.repeat(40)); assert.equal(h.$('receiveqr').src, 'data:image/png;base64,AAAA');
  assert.equal(h.$('accept').textContent, 'Done'); assert.equal(h.$('summary').textContent, 'Ethereum mainnet · ETH');
  assert.equal(h.$('receivedomain').textContent, 'Ethereum mainnet · ETH');
  assert.ok(h.$('receive').compareDocumentPosition(h.$('summary')) & h.dom.window.Node.DOCUMENT_POSITION_FOLLOWING);
  h.$('receiveaddress').textContent = 'renderer-tampered-public-address';
  await h.$('copyaddress').onclick(); assert.deepEqual(h.copyCalls, [[]]); assert.equal(h.$('copystatus').textContent, 'Address copied');
  assert.equal(h.calls.length, 0); assert.equal(h.$('copyaddress').disabled, false);
  const copy = h.event('copy'); h.$('receiveaddress').dispatchEvent(copy); assert.equal(copy.defaultPrevented, false);
  await h.submit(); cleared(h); assert.deepEqual(h.calls, [{promptId:id, accepted:true}]);
});
test('TP061 receive copy errors are sanitized, duplicate work is disabled, and late completion cannot repopulate a closed prompt', async t => {
  for (const outcome of ['success', 'error']) {
    const pending = deferred();
    const h = await harness({id, kind:'receive', title:'Receive TON', summary:'TON mainnet', address:'PUBLIC-ADDRESS', png:'AAAA'}, {copy:() => pending.promise}); t.after(h.close);
    const work = h.$('copyaddress').onclick(); await tick(); assert.equal(h.$('copyaddress').disabled, true);
    await h.$('copyaddress').onclick(); assert.equal(h.copyCalls.length, 1);
    await h.cancel(); if (outcome === 'success') pending.resolve(); else pending.reject(Error('CANARY-PRIVATE-COPY'));
    await work; cleared(h); assert.equal(h.$('copystatus').textContent, ''); assert.ok(!h.dom.window.document.body.textContent.includes('CANARY'));
  }
  const failed = await harness({id, kind:'receive', title:'Receive DCR', summary:'Decred mainnet', address:'PUBLIC-ADDRESS', png:'AAAA'}, {copy:() => Promise.reject(Error('CANARY-COPY-FAILURE'))}); t.after(failed.close);
  await failed.$('copyaddress').onclick(); assert.match(failed.$('copystatus').textContent, /could not be copied/); assert.equal(failed.$('copyaddress').disabled, false);
  assert.ok(!failed.dom.window.document.body.textContent.includes('CANARY'));
  const input = await harness({id, kind:'input', title:'Wallet input', labels:['Password']}); t.after(input.close);
  await input.$('copyaddress').onclick(); assert.equal(input.copyCalls.length, 0);
});
test('TP043 prompt focus stays in the current modal and visible-state events do not cancel', async t => {
  const h = await harness({id, kind:'input', title:'Change app password', labels:['Current', 'New', 'Repeat new']}); t.after(h.close);
  h.$('promptinput0').focus(); assert.equal(h.key('Tab', {shiftKey:true}).defaultPrevented, true); assert.equal(h.dom.window.document.activeElement, h.$('accept'));
  assert.equal(h.key('Tab').defaultPrevented, true); assert.equal(h.dom.window.document.activeElement, h.$('promptinput0'));
  h.$('promptinput1').focus(); assert.equal(h.key('Tab').defaultPrevented, false); assert.equal(h.key('Tab', {shiftKey:true}).defaultPrevented, false);
  h.dom.window.document.dispatchEvent(h.event('visibilitychange')); assert.equal(h.calls.length, 0);
});
test('TP043 a hidden or closed window cannot render a delayed phrase response', async t => {
  const delayed = deferred(); const h = await harness(null, {get:() => delayed.promise}); t.after(h.close);
  h.dom.window.dispatchEvent(h.event('pagehide'));
  delayed.resolve({id, kind:'recovery', title:'Record recovery phrase', phrase:fixturePhrase}); await tick();
  cleared(h); assert.equal(h.calls.length, 0); assert.ok(!h.dom.window.document.body.textContent.includes(fixturePhrase));
});
test('TP041 malformed prompt data fails closed without reflecting supplied data or sending a response', async t => {
  const input = {id, kind:'input', title:'Input', labels:['Password']};
  const invalid = [null, [], 1, {}, {...input,id:123}, {...input,id:'A'.repeat(32)}, {...input,id:'a'.repeat(31)}, {...input,kind:'eval'}, {...input,title:''}, {...input,title:'x'.repeat(257)}, {...input,labels:[]}, {...input,labels:null}, {...input,labels:Array(9).fill('Password')}, {...input,labels:['']}, {...input,labels:[123]}, {...input,labels:['x'.repeat(257)]},
    ...['', 'abandon '.repeat(11), 'Abandon ' + fixturePhrase, fixturePhrase + ' extra', fixturePhrase.replace(' ', '  '), fixturePhrase.replace('abandon', '<img>'), 'a'.repeat(513)].map(phrase => ({id,kind:'recovery',title:'Phrase',phrase})),
    ...['x'.repeat(65537), 12].map(summary => ({id,kind:'confirm',title:'Confirm',summary})),
    ...['', 'x'.repeat(513), null].map(address => ({id,kind:'receive',title:'Receive',summary:'Network',address,png:'AAAA'})),
    ...['', '<svg>', 'data:image/png;base64,AAAA', 'A', 'A===', 'x'.repeat(350001), null].map(png => ({id,kind:'receive',title:'Receive',summary:'Network',address:'PUBLIC-ADDRESS',png})),
    {id,kind:'receive',title:'Receive',summary:null,address:'PUBLIC-ADDRESS',png:'AAAA'}];
  for (const prompt of invalid) {
    const h = await harness(prompt); t.after(h.close); cleared(h); assert.equal(h.$('prompterror').hidden, false);
    assert.equal(h.calls.length, 0); assert.ok(!h.dom.window.document.body.textContent.includes('CANARY'));
  }
  const failed = await harness(null, {get:() => Promise.reject(Error('CANARY-SECRET-FETCH'))}); t.after(failed.close);
  cleared(failed); assert.equal(failed.$('prompterror').hidden, false); assert.ok(!failed.dom.window.document.body.textContent.includes('CANARY'));
});
test('TP041 submitting twice while a response is outstanding never sends duplicate approval or secrets', async t => {
  const pending = deferred(); const h = await harness({id,kind:'input',title:'Wallet input',labels:['App password']}, {respond:() => pending.promise}); t.after(h.close);
  const node = h.$('promptinput0'); node.value = 'synthetic-pending-password';
  await h.submit(); cleared(h, [node]); await h.submit(); await h.cancel(); assert.equal(h.calls.length, 1);
  await h.$('copyaddress').onclick(); assert.equal(h.copyCalls.length, 0);
  pending.resolve(); await tick(); cleared(h);
});
test('TP041 an empty backend summary remains a usable title-only notice, confirmation or receive prompt', async t => {
  for (const kind of ['notice', 'confirm', 'receive']) {
    const h = await harness({id, kind, title:'Wallet request', summary:'', ...(kind === 'receive' ? {address:'PUBLIC-ADDRESS', png:'AAAA'} : {})}); t.after(h.close);
    assert.equal(h.$('prompterror').hidden, true); assert.equal(h.$('accept').disabled, false);
    assert.equal(h.$('summary').textContent, ''); await h.submit(); cleared(h);
    assert.deepEqual(h.calls, [{promptId:id, accepted:true}]);
  }
});
