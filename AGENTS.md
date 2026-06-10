Use `go doc` and `gopls` liberally when writing, reading, understanding, and
debugging Go code.

Build and test inside the Nix dev shell, which provides pkg-config and the
prebuilt libghostty-vt: `nix develop -c go test ./...`. A plain `go build`
outside the shell fails because cgo cannot find libghostty-vt.

`twee` is pre-release experimental software. There are no compatibility
guarantees for its CLI, Go API, JSON output, daemon protocol, or trace formats
yet; they may change without notice before a stable release.

Use `shellcheck` with all Bash scripts.

Use `uvx ruff format` on all Python code.

## Pushing a new bookmark with jj

`jj git push --remote <r> -b <bookmark> --allow-new` still works but prints a
deprecation warning. The replacement is to mark the bookmark as tracked on
the remote *before* pushing, e.g.:

```
jj bookmark track <bookmark>@<remote>   # if the remote ref already exists
# or, for a brand-new bookmark, configure auto-tracking once:
jj config set --repo 'git.auto-local-bookmark' true
# (or set remotes.<remote>.auto-track-bookmarks per the jj docs)
```

If a previous PR merged and GitHub auto-deleted the remote branch, a fetch
will surface the deletion as a *bookmark conflict* on the local side
(`+ <new>` vs `- <old>`). Resolve with `jj bookmark set <name> -r @` before
pushing — otherwise the push fails with "stale info".
