Use `go doc` and `gopls` liberally when writing, reading, understanding, and
debugging Go code.

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
