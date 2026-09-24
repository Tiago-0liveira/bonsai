const {test} = require('node:test');
const assert = require('node:assert/strict');
const {findRelease, prepareRelease, publishRelease} = require('./release-publish.cjs');

function client(releases) {
  const calls = [];
  const listReleases = () => { throw new Error('Must use pagination'); };
  const github = {
    rest: {repos: {
      listReleases,
      getReleaseByTag: () => { throw new Error('Drafts are not available by tag'); },
      deleteRelease: async args => calls.push({operation: 'delete', ...args}),
      updateRelease: async args => calls.push({operation: 'update', ...args}),
    }},
    paginate: async (endpoint, args) => {
      assert.equal(endpoint, listReleases);
      assert.deepEqual(args, {owner: 'owner', repo: 'repo', per_page: 100});
      return releases;
    },
  };
  return {github, calls};
}

const draft = {id: 123, tag_name: 'v0.1.1', draft: true};
test('finds an exact draft tag beyond the first page', async () => {
  const {github} = client([...Array.from({length: 100}, (_, i) => ({tag_name: `v9.0.${i}`})), draft]);
  assert.equal(await findRelease(github, 'owner', 'repo', 'v0.1.1'), draft);
});
test('retry deletes only the matching draft', async () => {
  const {github, calls} = client([{id: 999, tag_name: 'v0.1.2', draft: true}, draft]);
  assert.equal(await prepareRelease(github, 'owner', 'repo', 'v0.1.1'), false);
  assert.deepEqual(calls, [{operation: 'delete', owner: 'owner', repo: 'repo', release_id: 123}]);
});
test('retry skips an already published release without mutations', async () => {
  const {github, calls} = client([{...draft, draft: false}]);
  assert.equal(await prepareRelease(github, 'owner', 'repo', 'v0.1.1'), true);
  await publishRelease(github, 'owner', 'repo', 'v0.1.1');
  assert.deepEqual(calls, []);
});
test('a new tag needs no cleanup, but publication requires an existing release', async () => {
  const {github, calls} = client([]);
  assert.equal(await prepareRelease(github, 'owner', 'repo', 'v0.1.1'), false);
  await assert.rejects(publishRelease(github, 'owner', 'repo', 'v0.1.1'), /No release found/);
  assert.deepEqual(calls, []);
});
test('publishes the matching draft by ID with semantic latest ordering', async () => {
  const {github, calls} = client([draft]);
  await publishRelease(github, 'owner', 'repo', 'v0.1.1');
  assert.deepEqual(calls, [{operation: 'update', owner: 'owner', repo: 'repo', release_id: 123, draft: false, make_latest: 'legacy'}]);
});
test('API failures propagate rather than being treated as missing releases', async () => {
  const {github, calls} = client([]);
  github.paginate = async () => { throw new Error('Forbidden'); };
  await assert.rejects(prepareRelease(github, 'owner', 'repo', 'v0.1.1'), /Forbidden/);
  await assert.rejects(publishRelease(github, 'owner', 'repo', 'v0.1.1'), /Forbidden/);
  assert.deepEqual(calls, []);
});
