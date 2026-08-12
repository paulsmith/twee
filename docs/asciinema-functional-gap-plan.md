# Asciinema functional-gap plan

## Purpose

Assess where Twee should close gaps with Asciinema without diluting its core
purpose: deterministic terminal automation, testing, inspection, and CI
artifacts. This is a product and implementation plan, not a commitment to turn
Twee into a hosted recording service.

## Current position

Twee already exceeds Asciinema in several automation-oriented areas:

- PTY-backed program control, terminal-state assertions, waits, screenshots,
  keyboard/paste/mouse/resize input, and a Go test harness.
- A validated `.twee` ZIP bundle with PTY output, input, resize, exit, and
  optional network capture.
- Offline replay artifacts: GIF, self-contained HTML, MP4, and WebM.
- Interactive graphics playback with timeline controls, speed, stepping,
  resize handling, input annotations, and Kitty/iTerm2/Sixel backends.

Asciinema is stronger as a recording distribution and presentation ecosystem:

- a compact, interoperable asciicast (`.cast`) format;
- broad terminal and browser playback;
- timeline markers/chapters;
- live streaming and late-joining viewers;
- hosted/self-hosted uploads, sharing, embeds, and access controls; and
- recording controls which can suspend capture around secrets.

## Gaps and decisions

### 1. Asciicast interoperability — complete

**Delivered (August 12, 2026):** `.twee` bundles can be exported one way to
asciicast v2 `.cast` NDJSON with `twee export session.twee -o session.cast`.
The exporter preserves recorded dimensions, timestamps, output, and resize
records; `--input` explicitly includes only type, key, and paste input events.
Unsupported events, terminal replies, mouse input, and non-UTF-8 payloads are
omitted and counted in `--machine` output as `omitted_events`. Conversion
validates the bundle before staging output and streams events without rendering
frames. The native `.twee` trace format remains authoritative because casts
cannot represent all Twee semantics.

**Status:** `.twee` bundles export to asciicast v2 `.cast` files for use with
Asciinema Player, asciinema-server, and the wider cast-file ecosystem. Import
remains out of scope.

**Decision:** Add one-way `.twee` to `.cast` export first. Do not make the
asciicast format Twee's native trace format: it cannot represent all Twee
semantics, notably typed input kinds, terminal replies, process metadata,
network capture, and rich validation metadata.

**Proposed CLI:**

```text
twee export session.twee -o session.cast
twee export session.twee -o session.cast --input
```

Default behavior exports output and resize records. `--input` opts into input
events because casts can reveal credentials and because ordinary Asciinema
recordings commonly exclude them.

**Implementation outline:**

1. Add a `.cast` export sink to `internal/export`.
2. Validate and read the source bundle through the existing trace-bundle path.
3. Write asciicast v2 NDJSON: one header followed by timestamped output and
   resize events. Map only input events that have an unambiguous cast
   representation when `--input` is set.
4. Declare an explicit policy for unsupported event kinds: omit them, count
   them, and report the count in `--machine` output; never silently reinterpret
   terminal replies as human input.
5. Preserve recorded terminal dimensions and timing. Do not apply the visual
   exporter frame-rate cap, crop, font, overlay, or max-idle options to a cast.
6. Document the information intentionally lost in conversion.

**Acceptance:**

- A converted cast validates with an independent asciicast v2 parser/player.
- Output and resize playback match the source trace's terminal behavior.
- Input is absent by default and present only with explicit opt-in.
- Invalid source bundles fail before committing output.
- Conversion is streaming and does not require rendering frames.

### 2. Marker events and navigation — high priority

**Gap:** Twee playback can pause, step, restart, and jump forward, but traces
have no user-defined labeled markers. This limits demos, incident review, and
presentation-style playback.

**Decision:** Add a native marker event to `.twee`; export it to asciicast
marker records when possible. Markers are metadata, not terminal input, and
must not affect terminal replay.

**Proposed CLI:**

```text
twee trace mark --label "Authentication complete"
twee run --script ops.json --trace-out session.twee -- ./app
# script operation: {"op":"trace_mark","args":{"label":"..."}}
twee play session.twee --pause-on-marker
```

`trace mark` targets the active trace on a named daemon. The script operation
allows deterministic CI and demo scenarios to label their own traces.

**Implementation outline:**

1. Extend the trace schema, bundle validation, and inspection output with a
   marker event containing timestamp and non-empty UTF-8 label.
2. Add daemon/RPC/script support for appending a marker to an active trace.
3. Add a marker keystroke to `twee wrap` - ^]m
4. Add playback navigation: next/previous marker, marker list in the status
   UI, and an optional pause-on-marker flag.
5. Add marker seeking and display to the self-contained HTML viewer.
6. Include markers in `.cast` export where compatible.

**Acceptance:**

- Markers retain recording order when timestamps are equal.
- Adding a marker does not change terminal-model state or exported visual
  frames unless the selected viewer displays marker UI.
- CLI, HTML, and interactive playback can find and seek every marker.
- Corrupt labels and unknown future marker fields are handled by the trace
  compatibility policy.

### 3. Capture pause/resume for secrets — medium priority

**Gap:** `twee wrap` trace recording is one-shot. Stopping finalizes the trace,
so a user cannot omit a password prompt and resume the same recording.

**Decision:** Add pause/resume to active terminal trace capture, retaining a
single trace and explicitly recording the resulting time gap. Do not infer or
redact secrets automatically.

**Proposed UX:**

```text
Ctrl+] p                  pause/resume trace capture in twee wrap
twee trace pause
twee trace resume
```

The recorder status row must make paused capture unambiguous. Script recording
remains independent and should continue to use its existing lifecycle unless a
separate need arises.

**Implementation outline:**

1. Model active trace state as recording or paused rather than only active or
   finalized.
2. While paused, omit PTY output, input, resize, and terminal-reply events.
3. On resume, add a metadata discontinuity event so tools can disclose that
   trace time and terminal state are incomplete across the gap.
4. Make `inspect` report each omitted interval and make playback show a brief
   discontinuity notice rather than pretending the trace is complete.
5. Reject or clearly define pause behavior for whole-session network traces,
   whose PCAP semantics otherwise diverge from terminal capture.

**Acceptance:**

- A pause excludes data generated in its interval from the bundle.
- Resume appends to the original bundle and the trace remains valid.
- Playback and inspect visibly disclose all gaps.
- Typed secrets are not retained in events or input overlays during a paused
  interval.

### 4. Embeddable web player — medium priority

**Gap:** Twee's self-contained HTML output is a strong offline artifact, but
it is not a reusable browser player. It lacks a stable embedding API, externally
loaded recordings, and presentation options such as autoplay, loop, poster,
or start offset.

**Decision:** First evolve the HTML viewer into a documented, versioned
standalone player asset which consumes a privacy-filtered replay manifest.
Keep fully self-contained HTML export as the default CI artifact. Do not expose
raw `.twee` bundles directly to browsers.

**Implementation outline:**

1. Extract the embedded viewer's CSS/JavaScript into versioned build assets
   while keeping a self-contained assembly path.
2. Define a small replay-manifest schema containing only composed frames,
   durations, input overlay text when explicitly included, and marker metadata.
3. Provide documented options: autoplay, loop, initial time/marker, controls,
   poster frame, speed, and theme chrome.
4. Provide a custom element or small JavaScript API only after the manifest and
   options are stable.
5. Ensure a host can serve replay assets with a restrictive CSP and without
   third-party requests.

**Acceptance:**

- Existing `twee export -o replay.html` remains self-contained and works from
  `file://`.
- A separately hosted player can embed a privacy-filtered replay without access
  to raw terminal events or source manifest secrets.
- Embed options work consistently across current major browsers.
- The player has no mandatory analytics, remote fonts, or third-party network
  dependencies.

### 5. Live streaming — defer pending product validation

**Gap:** Twee only replays completed bundles. Asciinema can publish a live
terminal session, including to viewers who join after it starts.

**Decision:** Treat live streaming as a separate product surface. Do not add it
until the compatibility, markers, and capture-pause work establishes a useful
recording format and audience demand is demonstrated.

The narrow first version should be a local or self-hosted relay, not a public
Twee cloud service:

```text
twee stream --listen 127.0.0.1:0 -- ./app
```

It should relay semantic terminal state plus incremental trace events, give
late joiners a snapshot, and use explicit bearer-token authentication whenever
it listens beyond loopback.

**Non-goals for v1:** public discovery, permanent hosting, accounts, social
features, comments, analytics, and arbitrary unauthenticated internet access.

**Entry criteria:**

- Evidence that review/demo workflows need real-time viewing rather than a
  quickly exported HTML artifact.
- A threat model for terminal output, typed input, network capture, tokens,
  exposure, retention, and revocation.
- A clear answer on whether this belongs in the `twee` binary or a separately
  deployed relay.

### 6. Hosted sharing and uploads — explicitly out of scope for now

**Gap:** Asciinema provides a complete hosting, sharing, account, embed, and
self-hosting ecosystem. Twee has no upload/share command or access-control
service.

**Decision:** Do not build a hosted platform now. Users can publish exported
HTML/video/GIF through existing CI artifacts, object storage, or documentation
hosting. Reconsider only after local/self-hosted streaming has a validated
security and operational model.

## Playback portability follow-up

Twee graphics playback currently requires a direct Kitty, iTerm2, or Sixel
terminal and does not support tmux/screen passthrough. Asciinema's text-native
playback works in substantially more terminal environments.

This is a meaningful gap for local playback but should not block the priorities
above because Twee already offers portable HTML, GIF, MP4, and WebM artifacts.
If direct terminal playback becomes a primary workflow, investigate either:

1. tmux/screen graphics-protocol passthrough with robust capability probing; or
2. a text-mode VT replay fallback, explicitly documented as lower visual
   fidelity than Twee's rendered playback.

Avoid a fallback that claims exact parity while silently changing font metrics,
emoji behavior, or mouse annotations.

## Delivery order

1. `.cast` export with explicit input policy.
2. Native markers, inspection, playback navigation, HTML support, and cast
   marker export.
3. Capture pause/resume with disclosed discontinuities.
4. Versioned embeddable player while preserving self-contained HTML export.
5. Validate demand and security model for self-hosted live streaming.

## Cross-cutting requirements

- Preserve Twee's privacy posture: raw traces can contain terminal output,
  typed input, command arguments, environment-derived metadata, and PCAP data.
  Every sharing/conversion feature must be opt-in where it expands exposure.
- Preserve atomic artifact output and owner-only permissions for newly created
  files.
- Keep trace validation forward-compatible and make unsupported conversion
  explicit rather than silently lossy.
- Test interoperability with independent Asciinema tooling, not only Twee
  round trips.
- Run the full suite in the Nix shell: `nix develop -c go test ./...`.

## References

- Asciinema CLI, cast format, player, markers, streaming, and server manuals:
  https://docs.asciinema.org/
- Twee trace format proposal: `docs/extensible-trace-format.md`
- Twee playback/export implementation plan: `docs/playback-export-reach-plan.md`
- Twee CI artifact guidance: `docs/ci-artifacts.md`
