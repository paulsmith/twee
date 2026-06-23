# Extensible Trace Format Proposal

Status: proposal for consideration.

This note proposes a trace format that can represent either a terminal/TUI/CLI
session or a browser session. It is based on the current `twee` trace bundle and
the Vibium recording format.

## Context

`twee` currently uses one terminal recording format: a `.twee` zip bundle
containing `manifest.json` and `events.jsonl`. It records PTY output bytes,
input events, resize events, and process exit.

Vibium uses a richer browser-oriented zip recording:

- `<n>-trace.trace`: newline-delimited JSON timeline events.
- `<n>-trace.network`: newline-delimited HAR-compatible network events.
- `resources/<hash>`: content-addressed screenshots and snapshot resources.

The useful durable commonality is not the event vocabulary itself. It is the
container, append-only timeline, sidecar resources, operation spans, and
references between events and resources.

## Goals

- Represent both terminal and browser sessions without forcing one model into
  the other.
- Keep terminal replay byte-exact and simple.
- Preserve browser recording concepts such as action spans, screenshots, DOM
  snapshots, input overlays, groups, protocol events, and network data.
- Allow future event types without breaking older readers.
- Support large recordings by keeping binary data and high-volume side streams
  out of the primary timeline.
- Make causality easy to inspect: which action caused which output, screenshot,
  DOM snapshot, or network request.

## Non-Goals

- Full compatibility with existing `.twee` v1 files without conversion.
- Replacing Playwright trace viewer compatibility in Vibium immediately.
- Defining every browser protocol, network, or DOM snapshot detail in the core
  schema.
- Requiring every producer to emit screenshots or snapshots.

## Bundle Layout

Use a zip archive as the durable trace container:

```text
trace.zip
|-- manifest.json
|-- events.jsonl
|-- resources.jsonl
|-- resources/
|   `-- sha256/
|       `-- ab/
|           `-- <digest>
`-- streams/
    |-- network.har.jsonl
    `-- protocol.bidi.jsonl
```

Required entries:

- `manifest.json`: trace metadata, sessions, targets, producer details, and
  feature flags.
- `events.jsonl`: the authoritative ordered event timeline.

Optional entries:

- `resources.jsonl`: resource metadata. This is useful when resource metadata
  should be inspectable without scanning all events.
- `resources/...`: binary or text sidecar payloads, content-addressed by digest.
- `streams/...`: high-volume side streams such as HAR entries or raw protocol
  messages.

## Manifest

The manifest describes the recording as a whole and declares which features a
reader must understand.

```json
{
  "format": "dev.twee.trace",
  "version": 2,
  "producer": {
    "name": "twee",
    "version": "0.0.0"
  },
  "started_at": "2026-05-06T19:21:00Z",
  "stopped_at": "2026-05-06T19:21:14Z",
  "time_unit": "us",
  "features": [
    "ops.v1",
    "resources.v1",
    "terminal.v1"
  ],
  "required_features": [
    "terminal.v1"
  ],
  "host": {
    "os": "darwin",
    "arch": "arm64",
    "hostname": "example-host"
  },
  "sessions": [
    {
      "id": "s1",
      "kind": "terminal",
      "title": "vim",
      "command": ["vim"],
      "cwd": "/repo",
      "env": {},
      "terminal": {
        "cols": 80,
        "rows": 24,
        "encoding": "utf-8"
      }
    }
  ]
}
```

For a browser trace, the session entry can use `kind: "browser"`:

```json
{
  "id": "s1",
  "kind": "browser",
  "title": "SauceDemo E2E",
  "browser": {
    "name": "chromium",
    "headless": true
  },
  "context": {
    "viewport": {"width": 1280, "height": 720},
    "options": {}
  }
}
```

## Event Envelope

Every line in `events.jsonl` is a self-contained JSON object with a stable
envelope:

```json
{
  "seq": 17,
  "t_us": 153240,
  "wall_time": "2026-05-06T19:21:00.153Z",
  "type": "terminal.output",
  "session_id": "s1",
  "target_id": "term1",
  "op_id": "op3",
  "body": {}
}
```

Fields:

- `seq`: strictly increasing event sequence number. This breaks ties when
  timestamps are equal.
- `t_us`: monotonic time since trace start, in the unit declared by the
  manifest. Replay should use this instead of wall-clock time.
- `wall_time`: optional wall-clock timestamp for human inspection.
- `type`: namespaced event type.
- `session_id`: session that owns the event.
- `target_id`: optional finer-grained target, such as terminal, page, frame, or
  process.
- `op_id`: optional operation span this event belongs to.
- `body`: event-type-specific payload.

Readers should skip unknown event types unless the relevant feature appears in
`required_features`.

## Event Namespaces

Recommended top-level namespaces:

- `op.*`: operation spans and groups.
- `terminal.*`: PTY and terminal model events.
- `process.*`: non-terminal process events.
- `browser.*`: browser page, frame, screenshot, DOM, and input events.
- `net.*`: network events.
- `protocol.*`: raw protocol commands and events.
- `resource.*`: resource declaration or lifecycle metadata.
- `annotation.*`: comments, labels, bookmarks, diagnostics, or tool notes.

The namespace convention lets a reader understand the broad category even when
it does not know a specific event type.

## Operation Spans

Operation spans are the most important shared abstraction from Vibium that
should be adopted by `twee`.

They answer "what was the tool/user trying to do?" while terminal output,
screenshots, network events, and DOM snapshots answer "what happened?".

```json
{
  "seq": 10,
  "t_us": 120000,
  "type": "op.start",
  "session_id": "s1",
  "target_id": "term1",
  "op_id": "op3",
  "parent_op_id": "op2",
  "body": {
    "name": "Key Down",
    "method": "twee.key",
    "params": {"key": "Down"}
  }
}
```

```json
{
  "seq": 12,
  "t_us": 121000,
  "type": "op.end",
  "session_id": "s1",
  "target_id": "term1",
  "op_id": "op3",
  "body": {
    "status": "ok"
  }
}
```

Span rules:

- `op_id` is stable within the trace.
- `parent_op_id` supports groups and nested protocol calls.
- `op.end` should include `status: "ok" | "error" | "canceled"`.
- Errors should be structured, not only strings:

```json
{
  "status": "error",
  "error": {
    "type": "timeout",
    "message": "wait text timed out",
    "retryable": true
  }
}
```

Vibium's current `before` and `after` events map naturally to `op.start` and
`op.end`. `twee` input commands can also be wrapped in operation spans, which
would make playback and debugging much easier without complicating terminal
replay.

## Terminal and TUI Events

Terminal events should stay byte-oriented so replay remains exact.

```json
{
  "seq": 11,
  "t_us": 120200,
  "type": "terminal.input",
  "session_id": "s1",
  "target_id": "term1",
  "op_id": "op3",
  "body": {
    "kind": "key",
    "key": "Down",
    "bytes_b64": "G1tC"
  }
}
```

```json
{
  "seq": 13,
  "t_us": 122000,
  "type": "terminal.output",
  "session_id": "s1",
  "target_id": "term1",
  "body": {
    "origin": "pty",
    "bytes_b64": "..."
  }
}
```

```json
{
  "seq": 20,
  "t_us": 200000,
  "type": "terminal.resize",
  "session_id": "s1",
  "target_id": "term1",
  "body": {
    "cols": 100,
    "rows": 32
  }
}
```

Terminal event types:

- `terminal.output`: bytes read from the PTY.
- `terminal.input`: bytes written to the PTY, with semantic metadata when
  available.
- `terminal.resize`: terminal size change.
- `terminal.snapshot`: optional structured VT snapshot or resource reference.
- `terminal.screenshot`: optional rendered screenshot resource reference.

For a non-interactive CLI process, use `process.output` instead of
`terminal.output`:

```json
{
  "type": "process.output",
  "session_id": "s1",
  "target_id": "proc1",
  "body": {
    "stream": "stdout",
    "bytes_b64": "..."
  }
}
```

Process event types:

- `process.start`
- `process.output`
- `process.signal`
- `process.exit`

## Browser Events

Browser events should describe pages, frames, visual frames, DOM snapshots, and
input overlays while leaving protocol details to optional side streams.

```json
{
  "seq": 30,
  "t_us": 500000,
  "type": "browser.page.created",
  "session_id": "s1",
  "target_id": "page1",
  "body": {
    "context_id": "ctx1",
    "url": "about:blank"
  }
}
```

```json
{
  "seq": 40,
  "t_us": 700000,
  "type": "browser.screencast.frame",
  "session_id": "s1",
  "target_id": "page1",
  "op_id": "op7",
  "body": {
    "resource": "sha256:abcdef...",
    "media_type": "image/jpeg",
    "width": 1280,
    "height": 720
  }
}
```

```json
{
  "seq": 41,
  "t_us": 710000,
  "type": "browser.dom.snapshot",
  "session_id": "s1",
  "target_id": "page1",
  "op_id": "op7",
  "body": {
    "snapshot_id": "after@op7",
    "resource": "sha256:123456...",
    "url": "https://example.com",
    "viewport": {"width": 1280, "height": 720}
  }
}
```

```json
{
  "seq": 42,
  "t_us": 720000,
  "type": "browser.input",
  "session_id": "s1",
  "target_id": "page1",
  "op_id": "op8",
  "body": {
    "kind": "mouse",
    "point": {"x": 640, "y": 360},
    "box": {"x": 600, "y": 340, "width": 80, "height": 40}
  }
}
```

Browser event types:

- `browser.context.created`
- `browser.page.created`
- `browser.page.navigated`
- `browser.frame.attached`
- `browser.frame.navigated`
- `browser.screencast.frame`
- `browser.dom.snapshot`
- `browser.input`
- `browser.dialog`
- `browser.console`

Vibium can continue emitting Playwright-compatible trace files while also
emitting this normalized event stream, or it can provide a converter.

## Network and Protocol Streams

Network data can be large and has its own mature schema. Keep it HAR-shaped and
link it back to the main timeline:

```json
{
  "seq": 90,
  "t_us": 1200000,
  "type": "net.har.entry",
  "session_id": "s1",
  "target_id": "page1",
  "op_id": "op7",
  "body": {
    "stream": "streams/network.har.jsonl",
    "offset": 18290,
    "request_id": "req-1"
  }
}
```

Alternatively, small HAR entries may be placed directly in `body.entry`.

Raw protocol data should be optional:

```json
{
  "type": "protocol.command",
  "session_id": "s1",
  "target_id": "page1",
  "op_id": "op7",
  "body": {
    "protocol": "webdriver-bidi",
    "method": "browsingContext.navigate",
    "params": {}
  }
}
```

## Resources

Binary and large text payloads should be content-addressed.

Resource reference format:

```text
sha256:<hex-digest>
```

`resources.jsonl` can describe each stored resource:

```json
{
  "id": "sha256:abcdef...",
  "path": "resources/sha256/ab/abcdef...",
  "media_type": "image/jpeg",
  "size": 42831,
  "created_t_us": 700000
}
```

This preserves Vibium's natural deduplication while making resource metadata
explicit and independent of a particular event type.

## Replay Model

Terminal replay:

1. Open `manifest.json`.
2. Find a `kind: "terminal"` session.
3. Create a VT model with the initial `cols` and `rows`.
4. Walk `events.jsonl` in `seq` order.
5. Feed `terminal.output` bytes into the VT model.
6. Apply `terminal.resize`.
7. Treat `terminal.input`, `op.*`, screenshots, and annotations as diagnostic
   overlays.

Browser replay:

1. Open `manifest.json`.
2. Find a `kind: "browser"` session.
3. Walk `events.jsonl` in `seq` order.
4. Render `browser.screencast.frame` resources for visual playback.
5. Use `op.*`, `browser.input`, and snapshots as timeline overlays.
6. Load HAR or protocol side streams on demand.

This avoids a common lowest-denominator player. Terminal and browser players can
share the timeline, resource lookup, span tree, and annotation machinery while
using domain-specific rendering.

## Migration

### From `twee` v1 Trace Bundles

Current `.twee` fields map directly:

- `manifest.version` becomes `manifest.version`.
- `manifest.command`, `env`, `cols`, `rows`, `pid`, `host`, `started_at`, and
  `stopped_at` move under the trace manifest and terminal session metadata.
- `events.jsonl` `output` becomes `terminal.output`.
- `events.jsonl` `input` becomes `terminal.input`.
- `events.jsonl` `resize` becomes `terminal.resize`.
- `events.jsonl` `exit` becomes `process.exit`.

The existing `twee play` model can be preserved by only consuming
`terminal.output` and `terminal.resize`.

### From Vibium Recordings

Current Vibium fields map as follows:

- `context-options` becomes manifest and browser session metadata.
- `before` becomes `op.start`.
- `after` becomes `op.end`.
- Group `before` and `after` become nested `op.*` spans.
- `input` becomes `browser.input`.
- `screencast-frame` becomes `browser.screencast.frame`.
- `frame-snapshot` becomes `browser.dom.snapshot`.
- `.network` resource snapshots become `net.har.entry` events or
  `streams/network.har.jsonl`.
- Raw BiDi command and event records become `protocol.*` events or
  `streams/protocol.bidi.jsonl`.
- `resources/<sha1>` becomes `resources/sha256/...` or is preserved with a
  manifest-declared digest algorithm during conversion.

## Versioning and Compatibility

Compatibility rules:

- Increment `version` for breaking changes to the envelope or manifest.
- Add optional event types without changing `version`.
- Use `features` to advertise what appears in the trace.
- Use `required_features` when a reader cannot safely ignore a feature.
- Readers must preserve unknown events when rewriting traces.
- Writers should prefer additive fields inside `body` over new event types when
  the meaning is still the same.

## Open Questions

- Should the container extension remain `.twee`, or should browser-compatible
  traces use a neutral extension such as `.trace.zip`?
- Should resource paths preserve Vibium's SHA1 names for Playwright trace viewer
  compatibility, or should conversion normalize to SHA256?
- Should `events.jsonl` allow chunk boundaries in one file, or should chunks use
  separate event files like Vibium does today?
- Should `wall_time` be present on every event or only on selected events?
- Should terminal VT snapshots be first-class structured events, resource
  references, or only derived from byte replay?

## Recommendation

Adopt the shared zip container and event envelope, then keep terminal and browser
event vocabularies separate under namespaces.

The most valuable immediate change for `twee` would be adding operation spans
around input commands and trace-control actions. That would make `.twee` traces
much easier to inspect while preserving the current byte-oriented terminal replay
path.

For Vibium, the most valuable step would be a converter or parallel writer that
maps existing Playwright-shaped events into the shared envelope without dropping
the current viewer-compatible files.
