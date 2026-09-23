// Pure version selection is shared by the workflow and its tests.
function selectVersion(tags, labels) {
  const bumps = labels.filter(x => /^release:(patch|minor|major|none)$/.test(x));
  if (bumps.length > 1) throw new Error('Use at most one release label');
  if (bumps[0] === 'release:none') return null;
  const versions = tags.filter(x => /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(x))
    .map(x => x.slice(1).split('.').map(BigInt));
  versions.sort((a,b) => { for (let i=0;i<3;i++) { if(a[i]!==b[i]) return a[i]>b[i] ? -1 : 1; } return 0; });
  const v = versions[0] || [0n,0n,0n];
  const index = bumps[0] === 'release:major' ? 0 : bumps[0] === 'release:minor' ? 1 : 2;
  v[index]++; for(let i=index+1;i<3;i++) v[i]=0n;
  return 'v'+v.join('.');
}
module.exports = {selectVersion};
