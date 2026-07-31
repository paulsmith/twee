# Bash: trace failed billing jobs

- **App:** Bash
- **Persona:** Ravi, an operations analyst following up on a morning batch incident.
- **Mission:** Correlate the request error in an API log with the failed jobs in the local queue export.
- **Journey:** Review the API log's error lines, then use a small `awk` query to list the failed job IDs and queues.
- **Value demonstrated:** A realistic, human-readable shell investigation with deterministic fixture data and useful command output.
