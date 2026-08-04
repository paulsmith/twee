# Network Capture Plan

Status: implemented with netwrap. The historical slirp4netns design below is
superseded by the netwrap TUN/gVisor library integration described in the
repository README. The README is normative for the implemented CLI, fixed
`10.0.2.100` guest address, raw-PCAP format, lifecycle, limits, and capture
scope; the remainder of this file preserves the earlier design record.

This document uses ASD-STE100 Simplified Technical English, Issue 9.
Software terms are project technical nouns and technical verbs.

## Purpose

Twee will capture network traffic from a managed program.
The capture will be part of the Twee trace.

A typical use has these parts:

- Twee starts a development web server.
- Twee records the terminal events from the server.
- Vibium operates a browser that connects to the server.
- Twee records the network packets from the server.

Twee will not directly own each program socket.
Twee will put the managed program in a separate network namespace.
Linux will route the network traffic through interfaces that Twee can monitor.

## Terms

This document uses these technical terms:

- **Managed program**: The program that Twee starts.
- **Descendant process**: A process that the managed program starts.
- **Host**: The system on which Twee operates.
- **Guest network**: The separate network that contains the managed program.
- **Network namespace**: A Linux boundary for interfaces, routes, and ports.
- **Mount namespace**: A Linux boundary for file-system mount points.
- **Published port**: A host port that sends traffic to a guest port.
- **Packet capture**: A time-ordered copy of network packets.
- **PCAPNG stream**: A Packet Capture Next Generation file in a trace.
- **Gateway**: A process that connects the guest network to the host network.

## User result

The user will get one trace with terminal data and network data.
The trace will keep the common time source for both data types.

The user can use a published port from a browser on the host.
The managed program can also make connections to external systems.
Twee will capture both traffic directions in the guest network.

The packet capture will operate without changes to normal socket calls.
The managed program will not need a special library.

## Feasibility evidence

Linux provides the required network boundary.
A network namespace has separate interfaces, routes, firewall rules, and port numbers.

Linux packet sockets can receive packets from an interface.
A user namespace can give the required capabilities only inside its boundary.

`slirp4netns` can connect a network namespace to an external network through a TAP interface.
Its API can also add host port forwarding.

A local test on 2026-07-31 captured an HTTP exchange on a namespace loopback interface.
The packet capture file contained 12 packets and 2,587 bytes.

The current development VM does not contain `slirp4netns` or `pasta`.
The complete gateway path still requires a test after Twee adds the selected helper.

## Proposed command interface

The command names in this section are part of this proposal.

Use `--network-capture` to enable the function.
Use `--publish` one or more times to publish guest ports.

```console
twee start \
  --trace run.twee \
  --network-capture \
  --publish 127.0.0.1:3000:3000 \
  -- npm run dev -- --host 0.0.0.0 --port 3000
```

Vibium can then open `http://127.0.0.1:3000`.

The `--publish` value has this form:

```text
HOST_ADDRESS:HOST_PORT:GUEST_PORT
```

The first release will require a trace option with `--network-capture`.
The `run` command will use `--trace-out` for this purpose.

Network capture is a session start option.
`twee trace start` cannot enable it for a session that uses the host network.

`twee trace stop` will stop the packet collector.
A later `twee trace start` can restart it in the prepared guest network.

The first release will not add published ports after program start.

Twee will reject `--network-capture` on an unsupported system.
Twee will not start the managed program after this error.

## Technical design

### Network boundary

Twee will create user, network, and mount namespaces.
Twee will create all namespaces before it starts the managed program.

The user namespace will give the required network capabilities inside the new boundary.
It will not give these capabilities on the host.

A setup process will use the network capabilities.
The managed program will use the numeric user and group identifiers of the host user.
Twee will remove the setup capabilities before program execution.

Twee will configure these guest interfaces:

- A loopback interface for guest local traffic.
- A TAP interface for traffic through the gateway.

The managed program and its descendant processes will inherit the network namespace.
They will use the guest interfaces and guest routes.

Twee will put a private Domain Name System file at `/etc/resolv.conf`.
The mount namespace will prevent a change to the host file.

### Packet collector

Twee will start a packet collector before it starts the managed program.
The collector will use Linux `AF_PACKET` sockets.

The collector will monitor the TAP interface and the loopback interface.
It will write packets directly to a temporary PCAPNG stream.
It will not keep the complete capture in memory.

The collector memory queue will have a fixed maximum size.
It will count packets that it cannot write.
The trace manifest will contain the packet and error counts.

### Guest gateway

The first Linux implementation will use `slirp4netns` as a rootless gateway.
This helper can give external network access without host administrator access.

Twee will disable guest access to the host loopback network.
Twee will enable the helper sandbox and system call filter when the helper supports them.

Twee will use the `slirp4netns` API socket for published ports.
Twee will start all published ports before it starts the managed program.

The Nix development shell will contain `slirp4netns`.
Release documentation will identify it as an external requirement.

A future privileged mode can use a virtual Ethernet pair and host routing.
This mode is not part of the first release.

### Trace data

Twee will store raw packets in this optional trace entry:

```text
streams/network.pcapng
```

The trace manifest will contain a `network_capture` object.
This object will contain these items:

- The capture mode and the helper version.
- The interface names and link types.
- The published port list.
- The start and stop times.
- The packet count and byte count.
- The dropped packet count.
- The final capture status.

The first implementation will add this stream to a version 1 trace.
Current readers ignore extra ZIP entries.
The bundle validator must validate the new entry and its size.

The future trace version 2 can add `net.*` timeline events.
The [extensible trace format proposal](extensible-trace-format.md) defines this event namespace.

Raw packets must not be JSON events.
This rule keeps the primary timeline small.

### Time source

The terminal recorder will use a monotonic clock.
The packet collector will convert each packet time to the same trace time.

Twee will record wall-clock and monotonic-clock anchors at capture start.
The PCAPNG interface data will identify the packet time resolution.

Twee will write the clock relation to the trace manifest.
This relation will let tools align packets with terminal events.

### Process and trace lifecycle

Twee will use this start sequence:

1. Create a private trace work directory.
2. Create the user, network, and mount namespaces.
3. Configure the guest interfaces, routes, and DNS file.
4. Start the packet collector.
5. Start the gateway and the published ports.
6. Create the terminal and start the terminal event recorder.
7. Start the managed program.

Twee will use this stop sequence:

1. Stop the managed process group.
2. Close the published ports.
3. Stop the gateway.
4. Stop and drain the packet collector.
5. Write the capture statistics.
6. Finalize the trace archive.
7. Remove the temporary network resources.

Twee must remove all resources after a partial start failure.
Twee must keep the original error information.

## Important limitations

Twee must show these limitations in command help and user documentation.

### Linux support

Full packet capture will operate only on Linux in the first release.
The Linux system must permit unprivileged user namespaces.
The system must also have the selected rootless gateway.

Some system policies disable user namespaces.
Twee cannot provide full capture on these systems without more privileges.

### Published server address

The first release will require the server to listen on a guest non-loopback address.
For example, the server can listen on `0.0.0.0`.

A server that listens only on guest `127.0.0.1` will not receive published-port traffic.
A later release can add a guest loopback relay.

The first release will publish only IPv4 Transmission Control Protocol ports.
The `slirp4netns` host forwarding API has this IPv4 limit.

The first release will not provide external Internet Protocol version 6 access.
The collector will still record version 6 packets that occur on a guest interface.

### Host services

The gateway will block access to host loopback addresses by default.
The managed program cannot use a service that listens only on host `127.0.0.1` through the gateway.

This rule does not block access through a pathname Unix socket.
The capture scope section gives more information about these sockets.

### Encrypted traffic

Transport Layer Security (TLS) encrypts application data.
The PCAPNG stream will contain the encrypted bytes.
Twee will not show the HTTPS headers or body from these bytes.

QUIC also encrypts most application data.
Twee will not describe raw QUIC packets as HTTP records.

The first release will not install a certificate authority.
It will not use a TLS interception proxy.
It will not collect TLS keys.

### Protocol meaning

A raw packet capture is not a HAR file.
It does not always identify an HTTP request, response, URL, or browser action.

Vibium gets this information from browser protocol events.
A normal managed program does not provide the same events.

Future tools can make flow summaries from the packet capture.
A future HTTP parser can make HAR-compatible data for traffic that it can read.

### Capture scope

The network namespace defines the capture scope.
The capture includes traffic from all processes in that namespace.

Twee cannot reliably assign each packet to one process.
The PCAPNG stream will not contain a process identifier for each packet.

The capture does not include Unix domain socket traffic.
It also does not include file input and output.

A program can send a request through a passed host socket descriptor.
This traffic can bypass the guest interfaces.

A program can use a host service through a Unix socket.
For example, a program can use a container daemon socket.
The packet collector will not see the daemon network traffic.

A process can create or join a different network namespace.
The packet collector will not capture traffic in that namespace.

### Descendant processes

The current Twee lifecycle follows only the managed program.
A detached descendant process can continue after the managed program exits.

Network capture will stop at the documented session end.
Traffic after this point will not be in the trace.

A detached descendant process can keep the network namespace in the Linux kernel.
Twee will close its collector, gateway, and published ports at session end.

Before release, Twee must stop the managed process group during normal shutdown.
A later cgroup mode can give a stronger Linux process boundary.

### Network changes

The rootless gateway changes some network properties.
Guest addresses can differ from host addresses.
Packet timing and network speed can also change.

Some protocols can fail through the rootless gateway.
For example, Internet Control Message Protocol echo requests can require host system changes.

The capture shows traffic at the guest interfaces.
It is not a bit-for-bit capture of the host interface.

### Data loss and size

Fast traffic or a slow disk can cause dropped packets.
Twee will record the number of known dropped packets.
Twee will mark the capture as incomplete after a write error.

Packet captures can use much disk space.
The first release must have a configurable byte limit.
The command output must show when the capture reaches this limit.

Twee will let the managed program continue after the limit.
Twee will stop only the packet collector.
The trace will show that the capture is incomplete.

## Security requirements

Network packets can contain passwords, tokens, cookies, and message bodies.
Twee must show this warning before the user enables network capture.

Network capture will always be an opt-in function.
Twee will not enable it from `--trace` alone.

Twee must create trace files with mode `0600` on Unix systems.
Twee must create temporary capture files in a private directory.
Twee must not follow a symbolic link for a capture output file.

Raw packet data does not permit safe general redaction.
Twee must not describe the first release as redacted.

The gateway parses packets from the managed program.
Twee must require a maintained gateway version.
Twee must use the gateway sandbox and system call filter when available.

The [security review](security-review-2026-07-29.md) identifies current trace permission risks.
Twee must correct these risks before it adds network payloads.

## Error rules

Twee must stop before program start for these errors:

- The system does not support the required namespaces.
- The rootless gateway is not available.
- Twee cannot create a packet socket.
- Twee cannot bind a published host port.
- Twee cannot create a private trace file.

Twee must not use the host network as a fallback after one of these errors.

An error after program start can make an incomplete capture.
Twee must report the error immediately.
Twee must also write the error state to the trace when possible.

## Implementation phases

### Phase 0: Security and lifecycle work

1. Create all trace files with private permissions.
2. Prevent symbolic-link output attacks.
3. Stop the managed process group at session end.
4. Add tests for partial start and stop failures.

### Phase 1: Linux packet capture

1. Add network options to the engine configuration.
2. Add namespace setup to the program start path.
3. Add the PCAPNG writer and packet collector.
4. Add the rootless gateway controller.
5. Add published port control through the helper API.
6. Add capture metadata to the trace manifest.
7. Add the optional PCAPNG entry to trace finalization.

### Phase 2: Command and library support

1. Add `--network-capture` to `start` and `run`.
2. Add repeatable `--publish` options.
3. Add equivalent options to `tuitest`.
4. Show the active capture mode in command results.
5. Show all capture limits in command help.

### Phase 3: Inspection tools

1. Add packet capture information to `twee bundle inspect`.
2. Add safe PCAPNG extraction to `twee bundle export`.
3. Add packet and flow totals to trace summaries.
4. Add `net.flow.start` and `net.flow.end` events to trace version 2.

### Phase 4: Protocol data

1. Examine an HTTP parser for unencrypted traffic.
2. Examine HAR output from readable HTTP messages.
3. Keep TLS interception outside the default design.

## Test plan

The implementation will include these tests:

- Capture the first connection that a managed program makes.
- Capture inbound and outbound Transmission Control Protocol traffic.
- Capture User Datagram Protocol and Domain Name System traffic.
- Use the private Domain Name System configuration.
- Capture guest loopback traffic.
- Connect from a host client through a published port.
- Capture traffic from a descendant process.
- Confirm the documented behavior for a detached descendant process.
- Confirm that TLS application data stays encrypted.
- Count packets that a full queue drops.
- Stop capture at the configured byte limit.
- Report an unavailable gateway before program start.
- Report a disabled user namespace before program start.
- Reject a published port that is not available.
- Create trace and temporary files with private permissions.
- Remove namespaces, helpers, listeners, and temporary files after failures.
- Open and validate a trace that contains `streams/network.pcapng`.
- Play an old trace that does not contain network data.

All Go tests will run in the Nix development shell:

```console
nix develop -c go test ./...
```

## Acceptance criteria

The first release is complete when all these statements are true:

- A host browser can use a published port to reach the managed server.
- The trace contains packets for both traffic directions.
- The trace contains guest loopback packets.
- The packet time and terminal event time use a documented relation.
- The capture starts before the managed program.
- The collector memory queue has a fixed maximum size.
- Twee reports packet loss and incomplete capture states.
- Twee gives an error instead of a silent capture fallback.
- Twee clearly tells the user that TLS payloads stay encrypted.
- Twee clearly tells the user which traffic is outside the capture scope.
- Trace files and temporary files have private permissions.
- Twee closes the collector, gateway, listeners, and temporary files at session end.

## Designs not selected

### Environment proxy

Twee could set HTTP proxy or SOCKS proxy environment variables.
Programs can ignore these variables.
This design cannot capture all socket traffic.

### Listener sockets only

Twee can make a listener for each known service port.
This design is useful for an explicit reverse proxy.
It does not capture unknown outbound connections.

The design also requires Twee to know each protocol or target port.
It is not the primary design for full capture.

### Socket activation

Twee could create sockets and pass their file descriptors to the managed program.
Only programs with socket activation support can use these sockets.
This design is not general.

### Library injection and system call tracing

Library injection does not cover static programs and all language runtimes.
System call tracing has high complexity and high performance cost.
Both designs can change program behavior.

The first release will not use these designs.

### eBPF capture

Extended Berkeley Packet Filter programs can observe network operations.
They require more system privileges and more kernel support.

eBPF can be a future method for process attribution.
It is not necessary for raw network capture.

## Open decisions

The implementation must resolve these decisions before Phase 1 ends:

- Select the default capture byte limit.
- Select the behavior when the gateway process stops unexpectedly.
- Select the minimum `slirp4netns` version.
- Select the PCAPNG time resolution.
- Select the trace version for `net.*` flow events.
- Select whether release packages include `slirp4netns`.

## References

- [ASD-STE100 Simplified Technical English, Issue 9](https://www.asd-ste100.org/assets/files/ASD-STE100_ISSUE9.pdf)
- [Linux network namespaces](https://www.man7.org/linux/man-pages/man7/network_namespaces.7.html)
- [Linux packet sockets](https://www.man7.org/linux/man-pages/man7/packet.7.html)
- [Linux user namespaces](https://www.man7.org/linux/man-pages/man7/user_namespaces.7.html)
- [`slirp4netns` manual](https://github.com/rootless-containers/slirp4netns/blob/master/slirp4netns.1.md)
- [TLS 1.3](https://www.rfc-editor.org/info/rfc8446/)
- [Vibium recording format](https://github.com/VibiumDev/vibium/blob/main/docs/explanation/recording-format.md)
