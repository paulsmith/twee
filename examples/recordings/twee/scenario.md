# Release artifact triage with twee

- **App:** twee
- **Persona:** Rowan, a CI and release engineer reviewing a captured deployment check before attaching it to a release incident.
- **Mission:** Verify the supplied trace, inspect its command, duration, and event counts, then export a browser-shareable replay.
- **Journey:** Rowan validates `deploy-check.twee`, filters `bundle info` into a readable execution summary, exports the trace to self-contained HTML with bounded idle periods, and confirms the replay exists.
- **Value demonstrated:** Twee makes a terminal artifact immediately inspectable and shareable without needing the original program or a live daemon.
