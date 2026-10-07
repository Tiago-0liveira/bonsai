# Managed process output contract

Each process has a persistent daemon ID and a browser ID scoped to its project.
The daemon persists the initial record before launching. Its allocation counter
survives record removal, so daemon restarts do not reuse browser identities. A command that cannot
start returns a process record, including its error and exit code, and follows
its configured retry policy. Invalid launch arguments or policy overrides are
rejected before a record is created.

`GET /api/projects/{projectId}/processes/{id}/terminal` uses the same host, origin,
and first-frame session authentication as agent terminals. Send `authenticate`
followed by `attach` with `generation` and `offset`. The server emits `ready`,
optional `gap`, `output`, and `status` frames in order. Output is base64 encoded
raw bytes. Its offset is the exclusive end byte position. Lifecycle status is
sent after preceding saved output has been delivered. Subscriptions continue
through backoff and terminal states to support a later manual restart. Closing
the socket cancels the daemon subscription, including silent subscriptions.

Stream generations identify output history, independently of execution attempts.
The generation and absolute rotation offsets are persisted in `<id>.log.cursor`;
manual and automatic restarts append under the same generation. Reconnect uses
the last accepted byte offset. The terminal accepts UTF-8 and ANSI bytes across
chunk boundaries and formats lifecycle markers without buffering ordinary lines.

Retention remains one live log and one rotated backup, normally 10 MiB per file.
Earlier output can be evicted after rotation. An attachment with an evicted or
invalid cursor receives an explicit `gap` before retained output; the browser
appends a visible notice. Retained logs can be downloaded separately from the
terminal's 5,000-line scrollback. Pending browser output is bounded (512 KiB
before xterm attaches, 1 MiB while rendering). A slow renderer reconnects from
its last accepted cursor. Socket write deadlines prevent slow consumers from
holding daemon subscriptions indefinitely. Child output writes never wait for
network consumers.

Process lifecycle revisions prevent older snapshots or responses from replacing
newer live status. The start response immediately inserts and opens its process,
selects its project and worktree, and attaches before xterm loads. Closing a card
only detaches its view. Stop cancels a pending retry; restart retains the ID and
history. Attempts, cumulative restarts, and the current retry count are separate.

The start dialog offers inherited, never, on-failure, and always policies. An
inherited selection omits an override; the backend reports the resolved default
for the actual command. Explicit retry limits are integers from 0 to 100 and
count additional attempts. Zero disables retries. Explicit selections survive
command changes and discovery refreshes. Command preview and launch both use
`pkgmgr.Resolve`, retaining the one-argument-per-line convention.

Node providers use explicit `run` to avoid collisions with built-in tool commands.
They append arguments for pnpm, Yarn and Bun; npm inserts `--` when
arguments are present. Cargo retains its executable-argument separator. Discovery
schema version 2 invalidates earlier cached strategies. The forwarding contract
is covered by controlled argv fixtures against installed tools and by each
provider's resolver tests. See the official [pnpm run](https://pnpm.io/cli/run),
[npm run](https://docs.npmjs.com/cli/commands/npm-run/),
[Yarn run](https://yarnpkg.com/cli/run), and
[Bun runtime](https://bun.sh/docs/runtime) documentation.
