# Playback and export manual verification matrix

This versioned matrix tracks checks that cannot be completed in the Linux CI
environment. Update the date, tested version, result, and notes when running a
row. Do not infer a pass from automated protocol or browser-fixture tests.

Status as of 2026-07-29: all rows below are **not run**. HTML export is
implemented, but its cross-browser compatibility gate remains open. iTerm2 and
Sixel playback are implemented but experimental until their applicable rows
pass. Kitty remains the first-choice backend in auto mode.

## Offline HTML replay

For each browser, open a representative exported page directly with `file://`.
Confirm the first frame renders; play/pause, restart, previous/next, timeline,
speed selection, and Space/Home/arrow shortcuts work; elapsed/total time is
correct; no network requests occur; and a corrupt source bundle did not replace
an existing destination.

| Browser target | Tested version | Result | Notes |
| --- | --- | --- | --- |
| Chrome, current stable | — | Not run | Requires desktop browser. |
| Firefox, current stable | — | Not run | Requires desktop browser. |
| Safari, current stable | — | Not run | Requires macOS. |

## Direct-terminal playback

For each terminal, run auto selection and the matching explicit backend. Play a
trace containing rapid updates, resize events, wide/tall frames, long idle
gaps, and user input. Confirm frames replace rather than stack or scroll;
flicker is acceptable; footers remain correctly placed; resize behavior is
correct; and normal exit, error, Ctrl-C, SIGHUP, and SIGTERM restore raw mode,
cursor visibility, and the original screen.

| Terminal/backend target | Tested version | Result | Notes |
| --- | --- | --- | --- |
| Kitty / Kitty protocol | — | Not run | Verify auto chooses Kitty first. |
| iTerm2 / inline images | — | Not run | Experimental; macOS only. |
| Sixel-enabled xterm | — | Not run | Experimental; record pixel geometry. |
| mlterm / Sixel | — | Not run | Experimental; second Sixel terminal. |

Also verify expected failure paths from a direct terminal:

- an unavailable explicit backend names the missing capability;
- auto mode lists the reason each backend is unusable; and
- explicit Sixel fails cleanly when reliable display pixel geometry is absent.

tmux and screen are outside the current support matrix. Playback must reject
`$TMUX` or `$STY` with the direct-terminal diagnostic; passthrough support is a
separate milestone.
