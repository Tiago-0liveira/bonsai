const {test} = require('node:test');
const assert = require('node:assert/strict');
const {selectVersion} = require('./release-version.cjs');
test('patch is default, tags are compared numerically', () => {
 assert.equal(selectVersion(['v0.9.9','v0.10.1','v1.0.0-rc.1','unrelated'],[]),'v0.10.2');
});
test('initial version and explicit bumps', () => {
 assert.equal(selectVersion([],[]),'v0.0.1');
 assert.equal(selectVersion(['v1.2.3'],['release:patch']),'v1.2.4');
 assert.equal(selectVersion(['v1.2.3'],['release:minor']),'v1.3.0');
 assert.equal(selectVersion(['v1.2.3'],['release:major']),'v2.0.0');
 assert.equal(selectVersion(['v1.2.3'],['release:none']),null);
});
test('conflicting release labels fail before creating a tag', () => {
 assert.throws(() => selectVersion([],['release:none','release:patch']));
});
