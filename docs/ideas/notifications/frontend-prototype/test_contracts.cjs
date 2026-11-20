/* Optional dependency-free tests for the autonomous notification prototype.
 * Run: node test_contracts.cjs
 */
'use strict';
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
const vm = require('node:vm');
const root = __dirname;
const read = name => readFileSync(join(root,name),'utf8');
const html = read('index.html');
const css = read('styles.css');
const app = read('app.js');
const fixtureCode = read('fixtures.js');
const sandbox = {Date,console};
sandbox.globalThis = sandbox;
vm.runInNewContext(fixtureCode,sandbox);
const fixtures = sandbox.AstraNoticeFixtures;
assert.ok(fixtures);
assert.equal(Object.keys(fixtures.kinds).length,5);
const initial=fixtures.createItems(16,Date.UTC(2026,10,12,9));
assert.equal(initial.length,16);
assert.equal(initial.filter(x=>!x.read).length,7);
assert.equal(new Set(initial.map(x=>x.id)).size,16);
assert.deepEqual(new Set(initial.map(x=>x.kind)),new Set(['system','economy','research','corporation','gameplay']));
const longer=fixtures.createItems(40,Date.UTC(2026,10,12,9));
assert.equal(longer.length,40);
assert.equal(new Set(longer.map(x=>x.id)).size,40);
assert.ok(longer.every(item => item.text.length>40));
assert.equal(fixtures.newItem(1).read,false);
assert.match(html,/src="\.\/fixtures\.js" defer/);
assert.match(html,/src="\.\/app\.js" defer/);
assert.match(html,/href="\.\/styles\.css"/);
assert.match(html,/data-variant="a"/);
for(const name of ['variant-a','variant-b','variant-c','filter-all','filter-unread','mark-all','notice-detail','empty-data','long-data'])
  assert.ok(html.includes('id="'+name+'"'),'missing UI control: '+name);
assert.ok(css.includes('.notice-window{'),'missing base variant A');
for(const v of ['b','c'])assert.ok(css.includes('data-variant='+v),'missing CSS variant '+v);
assert.ok(css.includes('overscroll-behavior:contain'),'scroll confinement missing');
for(const body of [html,css,app,fixtureCode]) {
  assert.ok(!/https?:\/\//.test(body.replaceAll('http://www.w3.org/2000/svg','')),'network URL in self-contained UI');
}
for(const disallowed of ['fetch(', 'WebSocket(', 'XMLHttpRequest', 'navigator.sendBeacon', 'Session.data', 'Modules.create', "api('notifications'"])
  assert.ok(!app.includes(disallowed),'unexpected external dependency: '+disallowed);
console.log('PASS: mock fixture integrity, variants, controls, scoped scroll, no remote dependencies');
