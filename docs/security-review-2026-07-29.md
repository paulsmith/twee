# Security and reliability review, 2026-07-29

## Status

This document records the findings from an adversarial review of `twee` at
commit `0fdd281f2a4ef44dbd13c59d9a41d69abf394fcd`.

The review was test-only: no product source was changed. Each substantive
finding below was reproduced against a binary built from that checkpoint, and
the highest-risk lifecycle findings were reproduced independently in fresh
state directories. Temporary sessions, processes, and artifacts were removed
afterward.

The findings are not remote-network vulnerabilities. The daemon listens on a
private Unix socket in the normal configuration, so daemon-protocol findings
generally require the ability to run commands as the same account. Bundle
findings apply when opening untrusted trace bundles. Trace and state-directory
findings matter most on multi-user systems or when paths are placed in shared
directories.

## Summary

| Severity | Finding | Primary impact |
| --- | --- | --- |
| High | A stopped daemon generation can corrupt a replacement session | Availability and state integrity |
| High | The TUI child can forge daemon readiness output | Result integrity and hidden processes |
| High | Invalid `region` arguments panic the daemon | Availability and trace loss |
| High | Unbounded terminal dimensions permit multi-gigabyte allocation | Availability |
| Medium | Trace archives are commonly created world-readable | Confidentiality |
| Medium | An insecure preexisting state directory enables symlink overwrites | Local file integrity |
| Medium | ZIP inflation is read into memory without a size limit | Availability |
| Medium | Detached descendants survive successful session shutdown | Resource and lifecycle integrity |

Two lower-severity hardening issues were also confirmed:

- screenshot output follows a preexisting destination symlink; and
- some bundle error paths print attacker-controlled filenames, including
  terminal control bytes, directly to the terminal.

## Findings

### 1. Stale daemon generation corrupts a replacement session

**Severity:** High

**Affected code:**

- `internal/daemon/server.go:47-85`
- `cmd/twee/cmd_stop.go:77-102`
- `cmd/twee/cmd_start.go:73-80`
- `cmd/twee/daemonize.go:164-181`
- `cmd/twee/daemonize.go:298-310`

#### Behavior

A client can connect to the daemon and send only part of an RPC frame. The
connection handler blocks indefinitely in `ReadMessage` because it has no read
deadline. The server waits for connection handlers during shutdown.

A second client can successfully issue `stop`. The stop command removes the
session socket and lock immediately, although the old daemon remains alive
waiting for the incomplete request. A new daemon can consequently start with
the same session name.

When the old connection finally closes, the old daemon finishes teardown and
blindly removes the socket and lock belonging to the replacement generation. It
also writes its own tombstone over the name shared with the replacement.

#### Reproduction result

The sequence was reproduced three times with fresh state:

1. start a named session;
2. hold an incomplete RPC frame open on its socket;
3. stop the session through a second connection;
4. start a replacement with the same name; and
5. release the incomplete connection.

The replacement daemon and child remained alive, but the socket and lock
disappeared. `twee status` then reported the old tombstone and treated the
replacement as stopped. The replacement could no longer be controlled through
the CLI.

#### Impact

A malformed or interrupted same-account client can wedge shutdown and make a
later session unreachable. The surviving process may continue consuming
resources, and active trace state can be lost or misreported.

#### Recommended remediation

- Apply a bounded deadline while reading each RPC frame.
- Stop accepting requests and close existing connections during shutdown.
- Do not remove the socket or lock until the daemon generation has actually
  exited.
- Associate cleanup artifacts with a generation identifier and remove them
  only if they still belong to that generation.
- Treat failure to reap the daemon within the shutdown timeout as an error
  instead of reporting a completed stop.

### 2. TUI child can forge daemon readiness output

**Severity:** High

**Affected code:**

- `cmd/twee/daemonize.go:73-153`
- `cmd/twee/daemonize.go:349-375`
- `internal/engine/config.go:45-56`
- `main.go:20`

#### Behavior

The daemon starts the TUI child before it closes its readiness channel. The
child inherits the readiness file descriptor and the daemon's internal control
environment. Observed variables included:

```text
READY_FD=3
LOCK_FD=4
TWEE_DAEMON_CMD=...
TWEE_DAEMON_MODE=1
NAME=...
```

The engine builds the child environment from `os.Environ`, so internal daemon
controls are not separated from the environment intended for the application.

#### Reproduction result

A child that wrote this value to descriptor 3:

```json
{"name":"spoofed","socket":"/tmp/fake.sock","pid":4242}
```

caused the outer `twee start` command to return:

```json
{"ok":true,"data":{"name":"spoofed","socket":"/tmp/fake.sock","pid":4242}}
```

The actual daemon had a different PID and socket and remained running.

#### Impact

An untrusted or compromised child can corrupt the result consumed by shell
scripts and orchestration. A caller may believe a nonexistent daemon was
started while the real daemon continues in the background. Inheriting
`TWEE_DAEMON_MODE` can also alter the behavior of nested `twee` invocations.

#### Recommended remediation

- Mark internal descriptors close-on-exec, or explicitly close them before
  starting the TUI process.
- Complete the readiness handshake before launching the TUI where practical.
- Construct the child environment from an allowlisted application environment
  and remove all daemon-only variables.
- Validate that a readiness response corresponds to the expected name, socket,
  and daemon process before returning it to the caller.

### 3. Invalid `region` arguments panic the daemon

**Severity:** High

**Affected code:** `internal/daemon/handlers_query.go:52-78`

#### Behavior

`handleRegion` checks only that width and height are positive. It does not
reject negative coordinates or unreasonable dimensions before allocating and
indexing the snapshot.

#### Reproduction result

A normal `1x1` region request succeeded. A request with `y = -1` caused the
client to receive an unexpected EOF and terminated the entire daemon.
Subsequent status calls failed because the socket no longer had a listener.

An extreme positive height reached the allocation path before coordinate
clamping and could also panic.

#### Impact

Any process able to access the session socket can terminate the daemon through
an otherwise valid RPC operation. This can interrupt the managed program and
lose in-progress trace or lifecycle state.

#### Recommended remediation

- Validate `x >= 0`, `y >= 0`, and bounded positive width and height before
  allocation.
- Reject integer overflow in `x + w` and `y + h`.
- Allocate only the clamped number of rows and cells.
- Recover from unexpected handler panics at the connection boundary so a
  single request cannot terminate the server.

### 4. Unbounded dimensions permit multi-gigabyte allocation

**Severity:** High

**Affected code:**

- `internal/play/bundle.go:45-93`
- `internal/export/export.go:37-52`
- `internal/export/replay.go:82-91`
- `internal/export/canvas.go:23-37`
- `internal/vt/ghostty.go:21-24`
- `internal/vt/ghostty.go:64-69`
- `internal/daemon/handlers_input.go:76-87`
- `internal/ptyrunner/runner.go:98-103`

#### Behavior

Bundle validation accepts arbitrary positive terminal dimensions. Export then
uses those dimensions for replay and rendering allocations. Some paths convert
the values to `uint16`, causing values greater than 65,535 to wrap.

The live `resize` operation has the same missing upper bound.

#### Reproduction result

A 314-byte ZIP containing a version 1 manifest with `cols` and `rows` set to
the maximum signed integer passed `twee bundle validate`:

```json
{"events":0,"valid":true}
```

Exporting that bundle was stopped after ten seconds. It reached approximately
4.5 GB maximum resident memory and spent most of the interval in system time.

Separately:

- a bundle width of 65,536 reached libghostty as an invalid zero value and
  panicked during export; and
- `resize --cols 65537` returned success but produced a one-column terminal
  because of `uint16` wrapping.

#### Impact

A tiny untrusted bundle can exhaust memory or crash an export process.
Validation currently provides false assurance that such a bundle is safe to
process. Live resize values can corrupt terminal state.

#### Recommended remediation

- Define one explicit maximum for rows, columns, and total cells.
- Enforce the limits in CLI decoding, RPC handlers, bundle validation, replay,
  export, and the libghostty boundary.
- Check conversions before narrowing to `uint16`.
- Make `bundle validate` reject any bundle that downstream commands cannot
  safely process.

### 5. Trace archives are commonly world-readable

**Severity:** Medium

**Affected code:**

- `internal/trace/trace.go:204-264`
- `internal/engine/input.go:10-21`

#### Behavior

The final trace ZIP is created with `os.Create`, whose requested mode is `0666`.
Under the common `0022` umask, the resulting archive has mode `0644`.

Trace archives contain application output, typed input, terminal metadata, and
explicitly configured environment variables.

#### Reproduction result

With umask `0022`, a trace written to a shared temporary directory had mode
`0644`. Its `events.jsonl` contained the base64-encoded bytes typed during the
session. A separate trace manifest contained an explicitly supplied test secret
from the child environment. A control run under umask `0077` produced mode
`0600`.

#### Impact

On a multi-user system, another account may read credentials, commands,
application data, or environment secrets from a trace placed in a traversable
shared directory. Documentation examples using `/tmp` make this a realistic
configuration.

#### Recommended remediation

- Create trace files with mode `0600`, independent of the caller's umask.
- Use a private temporary file in the destination directory, then atomically
  rename it after a successful close.
- Document that traces contain input and environment data.
- Consider making environment capture opt-in or supporting explicit redaction.

### 6. Insecure preexisting state directory enables symlink overwrites

**Severity:** Medium

**Affected code:**

- `cmd/twee/paths.go:17-57`
- `cmd/twee/daemonize.go:246-269`
- `cmd/twee/daemonize.go:323-325`
- `cmd/twee/tombstone.go:31-44`

#### Behavior

`MkdirAll(..., 0700)` does not repair or reject an existing directory with
unsafe permissions. The fallback state path is predictable from `$TMPDIR` and
`$USER`. Lock and tombstone creation follows symlinks at predictable names.

#### Reproduction result

The fallback state directory was precreated with mode `0777`, and the expected
lock path was made a symlink to another file writable by the victim account.
Starting the named session followed the symlink and replaced the target's
contents with the daemon PID.

The tombstone path was independently shown to overwrite a symlink target with
exit metadata. A control using a newly created state directory produced the
expected mode `0700`.

#### Impact

Another local user who can precreate or write the selected state directory can
overwrite an arbitrary file writable by the victim account. The attacker does
not gain additional filesystem permissions, but can corrupt configuration,
data, or shell-controlled files.

#### Recommended remediation

- Require the state directory to be owned by the effective user and inaccessible
  to group and other users.
- Reject unsafe existing state directories rather than silently using them.
- Open lock files with no-follow and exclusive-creation protections where
  supported, then verify the opened file with `fstat`.
- Create tombstones through a randomly named private temporary file and atomic
  rename.
- Prefer a trusted runtime directory over the predictable `/tmp` fallback.

### 7. ZIP inflation is read without a size limit

**Severity:** Medium

**Affected code:** `internal/bundle/validate.go:34-70,145-154`

#### Behavior

Bundle validation reads a ZIP entry into memory with an unbounded `io.ReadAll`
before the JSON-lines scanner applies its token limit. There is no maximum
compressed size, uncompressed size, or compression ratio.

#### Reproduction result

A roughly 65 KB ZIP expanding to 64 MB was accepted far enough to consume
170-203 MB maximum resident memory before validation rejected the oversized
event line. A malformed ten-byte control bundle used approximately 7 MB.

#### Impact

A small untrusted bundle can cause disproportionate memory and CPU use during
validation, information display, playback, or export.

#### Recommended remediation

- Reject ZIP entries whose declared uncompressed size exceeds a documented
  limit.
- Read through `io.LimitReader` and fail if the limit is exceeded.
- Stream `events.jsonl` rather than materializing it in memory.
- Apply limits to total archive expansion and individual event size.

### 8. Detached descendants survive successful shutdown

**Severity:** Medium

**Affected code:** `internal/ptyrunner/runner.go:157-179`

#### Behavior

Shutdown signals the direct child and relies on PTY closure or the terminal
session to clean up descendants. A descendant that creates a new session with
`setsid` escapes that lifecycle.

#### Reproduction result

A child launched a signal-resistant detached descendant. `twee stop --grace 0`
reported success and wrote the session tombstone, but the descendant survived,
reparented to PID 1. It required explicit cleanup. An ordinary background
process that remained in the original session was cleaned successfully.

#### Impact

Managed programs can intentionally or accidentally leave processes running
after `twee` reports the session stopped. These processes may retain resources,
files, sockets, or sensitive state.

#### Recommended remediation

- Define whether the lifecycle contract covers the process group, session, or
  a stronger containment boundary.
- At minimum, place managed processes in a dedicated process group and signal
  the group.
- Where available, use a platform containment mechanism such as a cgroup to
  cover descendants that create new sessions.
- Report incomplete cleanup rather than writing an unconditional successful
  tombstone.

## Lower-severity hardening

### Screenshot destination follows symlinks

`internal/render/render.go:79-80` creates screenshot output directly at the
requested path. In a shared writable directory, another user can precreate that
path as a symlink and cause `twee` to overwrite a different file writable by
the victim. Use private temporary creation and atomic rename, consistent with
the safer export path.

### Terminal escape injection through bundle filenames

Some errors in `cmd/twee/cmd_export.go` and `cmd/twee/cmd_play.go` interpolate
the bundle path directly into terminal output. A filename containing control
bytes can change terminal presentation. Structured JSON output escaped the
same filename correctly. Quote or sanitize untrusted paths before printing
them to a terminal.

## Toolchain vulnerability triage

The Nix development environment used Go 1.26.2. `govulncheck` reported three
symbol-level standard-library advisories:

- [GO-2026-4986](https://pkg.go.dev/vuln/GO-2026-4986), excessive resource
  consumption in `net/mail`;
- [GO-2026-4977](https://pkg.go.dev/vuln/GO-2026-4977), denial of service from
  pathological email input; and
- [GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971), a Windows-only panic in
  `net.Dial` and `net.LookupPort`.

The first two were reported through `go-arg` and `go-scalar`. Manual source
tracing showed that the affected `net/mail.ParseAddress` call is selected only
when parsing into a `mail.Address` field, and `twee` defines no such CLI field.
The third advisory affects Windows, while `twee` currently relies on Unix
facilities. These paths were therefore not treated as confirmed exploitable
findings for the reviewed scope.

The advisories are fixed in Go 1.26.3. The development environment should still
move to a current supported Go patch release as defense in depth.

## Validation performed

The following baseline checks passed:

```sh
nix develop -c go test -count=1 ./...
nix develop -c go test -race -count=1 ./...
nix develop -c go vet ./...
nix shell nixpkgs#shellcheck -c shellcheck scripts/*.sh
```

Additional controls found:

- fresh state directories had mode `0700`;
- fresh sockets and lock files had mode `0600`;
- session names rejected empty values, path separators, dot traversal, and NUL;
- RPC messages had a 4 MiB size cap;
- ZIP entry matching was exact and no ZIP extraction path was exposed;
- exported HTML embedded images as data under a restrictive content security
  policy;
- terminal footer rendering removed C0 and C1 controls;
- twelve concurrent starts produced exactly one success and eleven
  `ALREADY_RUNNING` responses;
- concurrent status and text queries completed successfully;
- waiters were released correctly during normal shutdown; and
- ordinary descendants in the managed terminal session were cleaned up.

`gopls` was not available in the host or Nix development environment. `go doc`
was used for the relevant standard-library lifecycle and deadline behavior.

## Suggested remediation order

1. Fix generation-owned shutdown and cleanup, then add a regression test using
   an incomplete RPC frame.
2. Close internal descriptors and scrub daemon-only environment variables
   before starting the TUI.
3. Add centralized coordinate, dimension, allocation, and conversion limits;
   add a connection-level panic boundary.
4. Make trace and state artifact creation private, atomic, and symlink-safe.
5. Stream and bound ZIP processing.
6. Define and enforce the intended descendant-process lifecycle.

