# Release builds and validation

GoReleaser **v2.18.2** builds six archives: Linux, macOS and Windows, each on
amd64 and arm64. Windows uses ZIP; other platforms use tar.gz. The checksum file
uses SHA256. Existing version, commit and build-date metadata, labels and rerun
behavior remain intact.

Node **22.12.0** and pnpm **12.9.1** are pinned in both release workflows.
GoReleaser global `before.hooks` perform a frozen frontend install, production
build, bundle-budget check and `go test -tags embedui ./web/`. The Bonsai build
uses `tags: [embedui]`. These follow the documented
[global hooks](https://goreleaser.com/customization/general/hooks/) and
[Go build tags](https://goreleaser.com/customization/builds/builders/go/).
Ordinary Go builds/tests remain Node independent and keep the `/app` placeholder.

The PR snapshot job uploads all six archives and checksums. Its native smoke
matrix runs on Linux, macOS and Windows and is included in **CI passes**.
After a merge into `main`, release labels select the version. GoReleaser builds
a draft; the native matrix downloads the actual draft assets, verifies all
checksums, extracts its native archive, and runs outside Git with isolated
config, state, cache, home and runtime directories. It checks production `/app`,
referenced JS/CSS, deep links, capability-authenticated requests, foreign-origin
rejection and supervised-child shutdown. The isolated idle supervisor is removed
by the harness after checking product stop behavior.

The draft download jobs retain contents write permission for draft visibility
([GitHub release API](https://docs.github.com/en/rest/releases/releases#list-releases));
they perform only reads.

The separate publish job depends on all three successful smoke jobs. A failed
job leaves the release in draft. Reruns reuse the merge's version tag and rebuild
an incomplete draft; published releases are preserved. The workflow operates
only on merged base-branch code, including for fork PRs.

```sh
goreleaser check
goreleaser release --snapshot --clean
node .github/scripts/archive-smoke.cjs dist
```

The integration-to-main release PR must carry **`release:minor`**. Phase 8
explicitly changes the original migration schedule: `bonsai serve` is removed
immediately, along with hosted access, the cloud relay, landing page and public
deployment inputs. No external hosting shutdown or DNS changes are included.
Product webhook tunnels and GitHub release publishing remain supported.

## First-run acceptance evidence

Automated Go CI exercises wizard models and setup backend application on Linux,
macOS and Windows. Archive smoke uses `--no-open --no-setup` and therefore does
not prove the interactive terminal experience or a graphical browser opening.
Record a native-terminal release-archive check on each OS before claiming the
original first-run exit criterion:

| Platform | Native wizard with release archive | Browser opens local `/app` |
| --- | --- | --- |
| Linux | Pending native-terminal check | Pending graphical opener check |
| macOS | Pending native-terminal check | Pending graphical opener check |
| Windows | Pending native-terminal check | Pending graphical opener check |

Use a fresh isolated user profile outside Git, run `bonsai web`, apply Standard
updates, confirm the opened URL and UI, edit settings through setup, run doctor,
and stop. Attach evidence with OS, architecture, archive checksum and revision.
A terminal automation or intercepted opener should be identified as such.

A confirmed real tunnel delivery is also required: record the receiver revision,
tunnel provider, signed delivery acceptance and evidence of local invalidation.
Do not substitute a localhost-only fixture for that check.
