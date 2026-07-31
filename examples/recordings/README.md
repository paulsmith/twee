# Recording examples

Each app directory contains:

- `scenario.md` — the persona, mission, journey, and demonstrated value;
- `record.sh` — an isolated, rerunnable recording driver; and
- `<app>.twee` — the finalized trace bundle.

The drivers deliberately send every printable character at 0.2 seconds per
character (about 60 WPM). Named keys and waits add readable pauses and keep
the recordings synchronized with the application.

| App | Persona | Mission |
| --- | --- | --- |
| [Bash](./bash/scenario.md) | Ravi, operations analyst | Correlate API request errors with failed queue jobs. |
| [Claude Code](./claude/scenario.md) | Release manager | Identify the three release risks needing an owner. |
| [Codex](./codex/scenario.md) | On-call lead | Assess the three operational risks in a deployment queue. |
| [fzf](./fzf/scenario.md) | Luis, release engineer | Find the rollback runbook and state the next action. |
| [Herdr](./herdr/scenario.md) | Engineering lead | Set up an incident desk with handoff and diagnostic checklist. |
| [htop](./htop/scenario.md) | Imani, support engineer | Inspect, sort, and search the live process table. |
| [rlwrap sqlite3](./sqlite3/scenario.md) | Priya, operations coordinator | Identify low stock, record an incoming quantity, and verify it. |
| [tmux](./tmux/scenario.md) | Maya, on-call engineer | Keep incident symptoms and response actions visible side by side. |
| [tree](./tree/scenario.md) | Elena, release coordinator | Review the release layout and its Markdown documents. |
| [twee](./twee/scenario.md) | Rowan, CI and release engineer | Validate, inspect, and export a captured deployment trace. |
| [Vibium](./vibium/scenario.md) | Customer-success coordinator | Complete and confirm a renewal handoff. |
| [Vim](./vim/scenario.md) | Mina, on-call release engineer | Update and save the deployment handoff checklist. |

## Play, inspect, and regenerate

Play a bundle with the terminal replay UI:

```bash
bin/twee play examples/recordings/vim/vim.twee
```

Check a trace without playing it:

```bash
bin/twee bundle validate examples/recordings/vim/vim.twee
bin/twee bundle info examples/recordings/vim/vim.twee
```

Regenerate one bundle from the repository root. A driver replaces only its
own trace file; set `TWEE_BIN` to use another Twee binary if needed.

```bash
examples/recordings/vim/record.sh
TWEE_BIN=/path/to/twee examples/recordings/vim/record.sh
```

## Runtime dependencies

All drivers need Bash and a built `bin/twee` (or `TWEE_BIN`). They also need
their named app: `vim`, `htop`, `fzf`, `tmux`, `tree`, or `sqlite3`; the Bash
example uses `grep` and `awk`. The SQLite driver invokes `nix shell
nixpkgs#rlwrap` to provide `rlwrap`. The Herdr example requires `herdr` and
`jq`. The Twee example also requires `jq`. The Vibium example requires
`vibium`, BusyBox `httpd`, and `nc`.

The Claude Code and Codex recordings require their respective CLI binaries
and existing authentication; they do not provision credentials. The remaining
Twee example uses the selected `TWEE_BIN` itself.
