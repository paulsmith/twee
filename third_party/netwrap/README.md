# Vendored netwrap library snapshot

This directory is a source snapshot of the library packages from the sibling
`../netwrap` Jujutsu repository. It is part of Twee's Go module under
`github.com/paulsmith/twee/third_party/netwrap`, so isolated Nix and source
builds do not require a sibling checkout and downstream users do not need a
local Go module replacement.

Provenance for this snapshot:

- sibling netwrap integration tip: change `yylqxruosywo`, commit `f56116b1c839`;
- Twee lifecycle adaptation: change `mloxszvxnoux`, commit `5ecd6e528dce`;
- Twee namespace/module review repairs: durable change `ykwwqtoyovxo`;
- gVisor source revision: `v0.0.0-20260801065709-124e365c3f93`.

The snapshot is not a blind byte-for-byte copy of the sibling tip. The Twee
change above adapts the managed-process and capture-completion API to
`internal/ptyrunner`; its diff is the patch record for that adaptation. Before
refreshing, reconcile that change with the sibling repository, then update the
library packages, tests, `go.mod`, and `go.sum` together. Do not copy the sibling
tree over this directory: its commands, examples, plans, and workspace metadata
are not part of the embedded module. Record the new sibling and Twee revision
IDs here in the same change as every refresh.
