# htop: triage a busy workstation

- **App:** htop
- **Persona:** Imani, a support engineer checking a workstation before a customer-call handoff.
- **Mission:** Quickly inspect the live process table, sort it by CPU, find the htop session, and compare its memory ordering before leaving the monitor.
- **Journey:** Let the process table settle, sort by CPU, search for `htop`, switch to memory ordering, pause to inspect each view, then quit.
- **Value demonstrated:** A compact but realistic monitoring loop using htop's interactive sort and search controls against real local process data.
