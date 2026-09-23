// The tag endpoint only finds published releases. The authenticated release
// listing also includes drafts; paginate so retries can find older releases.
async function findRelease(github, owner, repo, tag) {
  const releases = await github.paginate(github.rest.repos.listReleases, {
    owner, repo, per_page: 100,
  });
  return releases.find(release => release.tag_name === tag);
}

// Preserve published releases; remove incomplete drafts before rebuilding.
async function prepareRelease(github, owner, repo, tag) {
  const release = await findRelease(github, owner, repo, tag);
  if (!release) return false;
  if (!release.draft) return true;
  await github.rest.repos.deleteRelease({owner, repo, release_id: release.id});
  return false;
}

async function publishRelease(github, owner, repo, tag) {
  const release = await findRelease(github, owner, repo, tag);
  if (!release) throw new Error(`No release found for ${tag} after GoReleaser completed`);
  if (!release.draft) return;
  // Semantic ordering keeps a retry of an older release from becoming latest.
  await github.rest.repos.updateRelease({
    owner, repo, release_id: release.id, draft: false, make_latest: 'legacy',
  });
}

module.exports = {findRelease, prepareRelease, publishRelease};
