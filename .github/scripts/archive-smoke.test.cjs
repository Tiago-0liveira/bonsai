const {test} = require('node:test');
const assert = require('node:assert/strict');
const {createHash} = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {expectedArchives, verifyChecksums} = require('./archive-smoke.cjs');

function fixture(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bonsai-archive-check-'));
  t.after(() => fs.rmSync(dir, {recursive: true, force: true}));
  const sums = expectedArchives.map(name => {
    const data = Buffer.from(`archive ${name}`);
    fs.writeFileSync(path.join(dir, name), data);
    return `${createHash('sha256').update(data).digest('hex')}  ${name}`;
  });
  fs.writeFileSync(path.join(dir, 'checksums.txt'), sums.join('\n') + '\n');
  return dir;
}

test('all six archives verify, and a tampered archive fails before extraction', t => {
  const dir = fixture(t);
  assert.doesNotThrow(() => verifyChecksums(dir));
  fs.appendFileSync(path.join(dir, expectedArchives[0]), 'tampered');
  assert.throws(() => verifyChecksums(dir), /Checksum mismatch/);
});

test('a missing platform checksum cannot pass as a complete release', t => {
  const dir = fixture(t);
  const sums = fs.readFileSync(path.join(dir, 'checksums.txt'), 'utf8').trim().split('\n');
  fs.writeFileSync(path.join(dir, 'checksums.txt'), sums.slice(1).join('\n'));
  assert.throws(() => verifyChecksums(dir), /Missing platform archive/);
});
