# Link Alloy — Product and Engineering Overview

## Product goal

Link Alloy is a general-purpose Windows desktop application for a PC with multiple independent internet links. Its tunnel is intended to capture supported traffic from any Windows application, schedule new connections across healthy adapters, and show live per-link health and throughput.

This is connection-level scheduling, not packet-level bonding: each TCP or UDP flow uses one egress link for its lifetime. A single large TCP connection is not split across Ethernet and Wi-Fi. If its egress link or public IP changes, the application may need to reconnect.

## Implementation in this repository

The Link Alloy application and orchestration code are independently authored here. The engine currently contains:

- A stable, user-configurable, health-aware scheduler that pins each newly accepted flow to one adapter.
- IPv4/IPv6 address and port translation between captured application packets and a local user-space transport endpoint.
- Windows socket binding for upstream connections so those sockets leave through the selected physical interface while the system routes point at the TUN.
- A user-space TCP/UDP forwarder built around gVisor's TCP/IP stack.
- A Wintun-backed packet device with an embedded verified runtime, bounded route installer/remover, protected cleanup journal, single-instance lock, crash watcher, separate IPv4/IPv6 health probes, and bounded event log.
- A Wails/React dashboard with tunnel control, link eligibility and relative allocation controls, connection counts, fixed-window per-link rates/totals, transport diagnostics, and event history.

The engine and UI build on Windows. Tests cover stable allocation, lifecycle cancellation, protected-journal validation, IPv4/IPv6 flow translation, bounded TCP/UDP forwarding, packet parsing fuzzing, and PowerShell route-script parsing. A bounded elevated code-only smoke started Wintun, verified Windows route selection, sent eight HTTPS connections through two distinct egress paths, then confirmed the tunnel routes and addresses were removed on stop. Forced-process recovery, link-loss failover, and a sustained real application download remain to be verified before a functional alpha release.

## Traffic coverage and limits

The current packet path supports unfragmented IPv4/IPv6 TCP and UDP. ICMP and fragmented IP packets are not handled yet and may fail while tunnel routes are active. There is no process allowlist: supported traffic from Windows applications is routed by the OS into the TUN. Traffic that bypasses IPv4/IPv6 routing or uses unsupported protocols is outside the current engine's coverage.

Per-link IPv4 and IPv6 health is checked independently through short HTTPS requests to independent services. Returned public addresses are used only for the health decision and are not saved. The UI logs tunnel state, link-health transitions, flow assignments, and byte totals without recording payload contents or destination addresses.

The TUN adapter and route changes require administrator privileges. Link Alloy installs four most-specific IPv4/IPv6 routes while active, records validated interface and address details in an administrator-protected journal, removes only those routes on stop, and uses a single-instance lock to prevent a second process from touching a live session. A crash watcher attempts cleanup after an unexpected GUI exit. A hard process or system failure can still interrupt existing connections.

## Third-party boundary

Link Alloy's scheduler, packet mapping, adapter-bound dialing, route lifecycle, health orchestration, logging, and frontend are authored in this project. No application source files from HypoMux, HypoMuxPlus, or other product repositories are included. The project uses the gVisor user-space TCP/IP stack and the Wintun public API as separate dependencies under their own licenses. Those dependencies do not make the Link Alloy application a fork of an existing multi-link product.

See [`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md) and the bundled license texts for exact terms.

## Validation and release gate

A real application test must confirm all of the following before release:

1. A clean elevated start creates the tunnel and adds only the intended IPv4/IPv6 routes; a short smoke has passed this check.
2. Several supported TCP/UDP connections use the selected independent egress interfaces; the smoke observed two distinct paths.
3. A link health failure removes that path from new-flow scheduling.
4. Forced-process recovery restores routes after an unexpected termination.
5. A real application download completes over the tunnel, and any performance comparison records workload, conditions, and both adapter byte totals.

No speed gain or zero-interruption claim is made until measured under controlled conditions.

## Versioning

Version `0.0.1` is reserved for the first functional alpha. The release workflow checks the source version and readiness flag, builds the Windows installer and portable bundle, and retains the two newest GitHub Release records.
