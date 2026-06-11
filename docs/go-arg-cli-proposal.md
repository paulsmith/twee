# Proposal: adopt go-arg and clean up CLI grammar

## Status

Proposal. `twee` is pre-release experimental software, so this proposal assumes
the CLI and API may change without a compatibility bridge.

## Goal

Adopt `github.com/alexflint/go-arg` where it fits while fixing the CLI grammar
that currently makes flag placement fragile. This is a breaking cleanup, not a
compatibility-preserving refactor.

The important policy changes are:

- Use long options only: `--name`, `--env`, `--pattern`, `--timeout`,
  `--regex`, `--out`, `--dir`, `--cols`, and `--rows`.
- Treat short-style option aliases as usage errors.
- Use `--help` for help. Do not accept a short help alias.
- Keep `--` as the explicit boundary before child argv or literal payload.

## Parser Architecture

Keep a small manual root parser for now. Do not migrate the whole root command
tree to `go-arg` in the first pass.

The root parser owns:

- Daemon-mode detection.
- Global client options before the verb, such as `twee --name SESSION status`.
- Verb selection.
- `help`, `version`, and `completion`.
- Unknown-verb errors.
- Dispatch to concrete verb handlers.

Add `TWEE_SESSION` as a documented client-facing default session selector.
Session precedence for name-aware daemon commands is:

1. Per-command `--name`.
2. Global `--name`.
3. `TWEE_SESSION`.
4. `default`.

Session resolution must be presence-aware. Do not use string fields defaulted to
`default` for local or global names. Use `*string` or an explicit option type
such as:

```go
type nameOpt struct {
	Value   string
	Present bool
}
```

This lets `sessionName(local, global nameOpt) (string, error)` distinguish an
absent option from an explicitly provided empty value. Empty explicit names are
errors.

Tighten `validateName` during this breaking cleanup so session names must not
begin with `-`, in addition to the existing restrictions against empty names,
path separators, NUL, `.`, and `..`.

Only name-aware daemon commands call session resolution. Local, ephemeral, and
top-level commands do not read `TWEE_SESSION`.

Explicit global `--name` is accepted only for commands that target or create a
named daemon: `start`, `status`, `stop`, `diff`, query commands, input commands,
state-changing daemon commands, `wait`, and `trace`.

Explicit global `--name` is a usage error for `help`, `version`, `completion`,
`sleep`, `play`, `run`, `codegen`, and `ls`. `ls` currently lists all sessions;
unless a future design adds filtering, it should not accept `--name`.

## Help and Error Handling

Keep static `registerUsage` help authoritative in the first pass. Use `go-arg`
for parsing, not generated user-visible help.

The parser wrapper should:

- Scan only the pre-`--` portion of argv for `--help`.
- Route `--help` to existing static help for the relevant verb path.
- Treat post-boundary `--help` as payload or child argv.
- Reject short help aliases.
- Map parse errors to the existing usage exit code `2`.
- Preserve current stdout/stderr conventions.

This means these are payload or child argv:

```sh
twee type -- --help
twee paste -- --help
twee key -- --help
twee signal -- --help
twee start -- child --help
twee run -- child --help
twee codegen -- child --help
```

But this prints command help:

```sh
twee type --help
```

## Command Classes

### Ordinary Name-Aware Daemon Leaves

Use `go-arg` per handler for flags, positionals, stricter validation, and
interspersed option parsing.

This includes:

- `status`
- `stop`
- `diff`
- `text`
- `lines`
- `cursor`
- `size`
- `title`
- `mode`
- `scrollback`
- `snapshot`
- `cell`
- `region`
- `find`
- `resize`
- `screenshot`
- `signal`
- `key`
- `keys`
- Concrete leaves under `wait` and `trace`

### Ordinary Local Leaves

Use `go-arg` where useful, but do not apply session resolution.

This includes:

- `sleep`
- `play`

### `ls`

Keep `ls` as a local listing command that scans all session sockets. It does not
consume `TWEE_SESSION` or `--name` unless a future design adds filtering.

### Subverb Containers

Keep manual subverb dispatch for `wait` and `trace`, then parse each
concrete leaf with `go-arg`.

Static help should continue to support:

```sh
twee help wait text
twee help trace stop
```

### Child-Argv Commands

`start`, `run`, and `codegen` are special. They require an explicit `--`
boundary before the child command:

```sh
twee start [daemon options] -- <cmd> [args...]
twee run [run options] -- <cmd> [args...]
twee codegen [codegen options] -- <cmd> [args...]
```

Before `--`, parse only `twee` options with `go-arg`. After `--`, preserve child
argv tokens exactly, including dash-prefixed options and literal `--`.

Examples:

```sh
twee start --cols 80 -- vim file
twee start -- vim file --cols 80
twee run --script ops.json -- /bin/echo ok
twee codegen --out ops.json -- /bin/cat --literal-child-flag
```

Old bare forms fail as usage errors before launching children:

```sh
twee start vim file --cols 80
twee run /bin/echo ok --script ops.json
twee codegen /bin/cat --out ops.json
```

### Literal Text Payload Commands

`type` and `paste` require an explicit `--` before payload, unconditionally:

```sh
twee type [client options] -- <text...>
twee paste [client options] -- <text...>
```

The payload remains joined with single spaces, matching current behavior, but
dash-prefixed text is now unambiguous:

```sh
twee type -- --name
twee type -- --
twee paste -- --literal
```

`key` and `keys` are not literal text payload commands. They remain
parser-managed key-name positionals:

```sh
twee key Enter --name s
twee keys Escape Enter --name s
```

`--` may still be accepted by normal parser semantics before a positional key
name:

```sh
twee key -- --help
```

In that form, `--help` is treated as the key-name positional, not help.

## Text Search Grammar

Replace fragile text positionals for text-search style commands with named
values:

```sh
twee wait text --pattern TEXT [--regex] [--timeout DUR]
twee wait no-text --pattern TEXT [--timeout DUR]
twee find --pattern TEXT [--regex]
```

Dash-leading string values use equals form because stock `go-arg` does not
accept a separate dash-leading token as a string flag value:

```sh
twee wait text --pattern=-- INSERT -- --timeout 3s
twee find --pattern=--literal
```

The same rule applies to any string option whose value may begin with `-`, such
as `--out=-file`, `--dir=-workdir`, or `--against=-snapshot`.

Session names beginning with `-` are rejected instead of requiring equals form.

## Repeated `--env`

Repeated env overrides on `start` and `codegen` must consume exactly one value
per `--env` occurrence.

Use `go-arg`'s `separate` tag for a `[]string` field or a custom repeated
`KEY=VALUE` option type. Do not use a plain slice flag that greedily consumes
multiple following values.

Preserve existing semantics where later duplicate keys overwrite earlier ones
after validation with `KEY=VALUE` splitting.

Examples:

```sh
twee start --env A=B --env C=D -- cmd ARG
twee codegen --env A=B --out ops.json -- /bin/cat
```

In the first example, `ARG` is child argv, not an env value.

## `start` Immediate-Exit Contract

Make quick child-exit reporting deterministic by extending the daemon readiness
protocol, not by relying on a best-effort parent-side status query after the
socket may be gone.

In daemon mode:

1. Start the child with `engine.Start`.
2. Create and chmod the listener/socket.
3. Observe the child for a short quick-exit window before sending a success
   ready message.
4. If the child exits during that window, write a ready error with structured
   quick-exit fields instead of success.

Use a 100ms observation window initially, matching the daemon's current delay
before stopping after child exit. This adds up to 100ms to successful `start`,
which is acceptable for clearer pre-release semantics.

On quick exit, `twee start` returns a normal JSON error envelope on stdout,
exits non-zero, and includes structured details:

- Session name.
- Child argv.
- Exit code if available from `engine.Term.ExitCode`.
- Whether the socket was created.

The quick-exit daemon path must close the listener, close the engine term,
remove the socket, close the ready pipe, release the lock by exiting, and exit
cleanly. A failed `start` must not leave a reachable socket or block restarting
the same session name.

Expected behavior:

- `/bin/false` deterministically returns the immediate-exit error with exit
  code/details.
- `sleep 0` returns the immediate-exit error if it exits during the observation
  window.
- Long-running children return `ok:true`.

The misplaced-daemon-option hint is only for rejected missing-boundary
invocations or quick exits from a command line that lacked an explicit boundary.
It must not fire merely because explicit post-boundary child argv contains
`--cols` or `--name`.

## Implementation Sequence

1. Add parser helpers in `cmd/twee`:
   - Root global option parsing.
   - Presence-aware session-name resolution for name-aware daemon commands.
   - Static-help interception.
   - Explicit-boundary parsing helpers.
   - Long-option-only validation.
   - Global string-option documentation/tests for dash-leading values.
   - Repeated `--env` parsing.
   - A wrapper around `go-arg` parse errors.
2. Update static help and README command examples for the new grammar,
   especially `start`, `run`, `codegen`, `type`, `paste`, `wait text`,
   `wait no-text`, and `find`.
3. Convert ordinary name-aware daemon leaves, ordinary local leaves, `ls`
   handling, and subverb leaves to `go-arg` or the classified manual helpers.
4. Convert `start`, `run`, `codegen`, `type`, and `paste` with explicit
   boundary parsing and missing-boundary usage errors.
5. Extend the daemon ready protocol for deterministic quick-exit reporting and
   cleanup.
6. Run:

```sh
go test ./cmd/twee
go test ./...
```

Use `go doc` and `gopls` while implementing, per `AGENTS.md`.

## Acceptance Tests

- Session precedence for name-aware daemon commands: per-command `--name` beats
  global `--name`, global beats `TWEE_SESSION`, and `TWEE_SESSION` beats
  `default`.
- Explicit empty `--name ''` is rejected.
- Dash-leading session names are rejected.
- `twee --name s status`, `twee status --name s`, and
  `TWEE_SESSION=s twee status` target the same session.
- Local, top-level, and ephemeral commands reject explicit global `--name` where
  not applicable: `help`, `version`, `completion`, `sleep`, `play`, `run`,
  `codegen`, and `ls` unless filtered `ls` exists.
- Local, top-level, and ephemeral commands are unaffected by `TWEE_SESSION`.
- Short-style option aliases fail as usage errors before parsing reaches
  `go-arg`.
- Post-boundary `--help` is payload for both explicit-boundary and ordinary
  commands where `--` precedes positionals.
- Pre-boundary `--help` prints static command help.
- Missing-boundary invocations for `start`, `run`, `codegen`, `type`, and
  `paste` fail usage before launching or typing anything.
- `twee start --cols 80 -- vim file` sets daemon columns.
- `twee start -- vim file --cols 80` passes `--cols 80` to `vim`.
- `twee run --script ops.json -- /bin/echo ok` preserves child argv
  after `--`.
- `twee codegen --out ops.json -- /bin/cat --literal-child-flag` preserves
  child flags after `--`.
- `twee type -- --name`, `twee paste -- --literal`, `twee type -- --`, and
  multi-word text payloads behave as documented.
- `twee key Enter --name s`, `twee keys Escape Enter --name s`, and
  interspersed `--name` variants parse consistently.
- `twee wait text --pattern INSERT --timeout 3s --name t` parses.
- Dash-leading string option values use equals form and parse where allowed:
  `--pattern=-- INSERT --`, `--out=-file`, and `--dir=-workdir`.
- `wait no-text` and `find` use the same `--pattern` convention.
- Repeated env flags consume one value each and do not steal child argv:
  `twee start --env A=B --env C=D -- cmd ARG`.
- `/bin/false` under `start` returns an immediate JSON error with exit
  code/details, leaves no reachable socket, and the same session name can be
  started afterward.
- Long-running children return `ok:true`.
- Explicit post-boundary child args like `twee start -- /bin/echo --cols 80`
  do not trigger misplaced-daemon-option hints.
- Help output no longer advertises options after child argv for commands where
  that would be wrong.
