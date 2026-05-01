The user wants: let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

Explore the codebase thoroughly. Understand the architecture, conventions,
test patterns, and relevant files. Identify constraints, dependencies, and
any existing code that relates to the request.

Output a structured exploration summary:
- **Relevant files**: paths and their roles
- **Architecture constraints**: patterns that must be followed
- **Test conventions**: how tests are structured and run
- **Risks**: anything that could go wrong
- **Build system**: how to build and test

---
# Context from Prior Pipeline Stages

## Human Response
let's add a recording feature. It's something you invoke at any time during a session with a start and stop boundary. The precedent I have in mind is the playwright trace also Vibium, which is what I'm thinking about while I'm building twee, has a native recording format also based on playwright traces. In that case, it captures screenshots of a browser instance under test along with DOM state, network traffic, etc. So let's make a TUI-native recording file (.zip bundle of artifacts with a known manifest). Off the top of my head: path to executable, args, env vars, host details (os, arch), pid, any events (keystrokes)

## Previous Node Output
Excellent — I now have a thorough understanding of the project. Here's what I see:

---

## Pipeline Initialized ✅

**Project:** `twee` — a CLI tool for spawning terminal UIs under a PTY and driving them from outside (typing, key presses, waiting for text, screenshots, etc.). Every command prints one JSON object and exits.

**Tech stack:** Go + CGO (`libghostty-vt` via `go-libghostty`), Unix PTY, JSON-over-Unix-socket wire protocol.

**Current state:** A working implementation with:
- `tuitest/` — public Go test harness API
- `internal/daemon/` — daemon server + handlers for all ops
- `internal/engine/` — VT model, input, query, wait logic
- `internal/rpc/` — wire codec, op types, error codes
- `cmd/twee/` — the CLI itself

---

## What would you like to build?

Given this is a TUI-driving tool, here are some directions I can see being valuable — but tell me what *you* have in mind:

1. **Mouse input support** — `click <x> <y>`, `hover`, `drag` (currently listed as a known limitation)
2. **`twee run` scripting improvements** — streaming results, parallel op execution, conditional branching
3. **New wait/query primitives** — e.g. `wait regex`, `wait cell-style`, region-aware stable wait with exclusion zones
4. **Recording & replay** — full session playback for debugging flaky tests
5. **Linux CI / cross-platform hardening** — the README says "macOS-tested, Linux should work but isn't exercised"
6. **Something entirely different** — a new feature, a refactor, a bugfix you have in mind

**What would you like to build?**