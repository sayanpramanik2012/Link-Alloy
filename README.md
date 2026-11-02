# Link Alloy

Link Alloy is an independent Windows application that routes new connections across Ethernet, Wi-Fi, USB tethering, and other eligible adapters.

It addresses a practical problem: a Windows PC can have multiple independent internet links while a download workload uses only one of them.

## What it does

Link Alloy captures supported routed traffic through a Windows tunnel, assigns each new TCP or UDP connection to a healthy link, and binds the upstream socket to that adapter. The dashboard shows link health, active connections, throughput, totals, diagnostics, and recent activity.

Link Alloy is connection-level scheduling:

- Equal weights give new connections equal scheduling opportunity.
- Multi-connection downloads can use Ethernet and Wi-Fi together.
- A connection already assigned to one link stays on that link.
- Weight changes affect new connections.
- New connections can move to another link after a health failure.
- Existing connections may need to reconnect when a public route changes.

No speed increase or zero-interruption failover is guaranteed. Results depend on the launcher, server, parallelism, disks, and each provider’s available capacity.

## Quick start

The portable handoff is a single executable: LinkAlloy.exe.

Run it as administrator.

1. Connect the Ethernet, Wi-Fi, or tethered links you want to use.
2. Close older Link Alloy windows or engine-test processes.
3. Run LinkAlloy.exe with Run as administrator.
4. Open Settings and enable the intended links and relative weights.
5. Return to Overview and press Start tunnel.
6. Wait for the links to show their health state.
7. Start or resume the launcher download.
8. If the launcher was already downloading, close and reopen it or pause/resume it to create fresh connections.
9. Stop the tunnel from Link Alloy before closing the app.

Do not run two Link Alloy windows at the same time. A Windows session lock prevents a second process from changing a live session.

## Dashboard

- Overview: tunnel status, combined throughput, rolling history, and link cards.
- Connections: active counts, packet counters, and per-link totals.
- Activity: local event history with filters.
- Settings: link eligibility, relative weights, IPv4/IPv6 health, and diagnostics.

The dashboard displays engine counters. It does not invent a combined speed value or claim that one connection is split across links.

## Supported traffic and limits

The current path supports unfragmented IPv4/IPv6 TCP and UDP. ICMP and fragmented packets are not handled yet and may fail while the tunnel is active.

Other boundaries:

- One connection uses one egress link for its lifetime.
- Existing connections are not transparently migrated between public IPs.
- VPNs, other TUN adapters, proxy accelerators, and tools that own the default route may conflict.
- There is no per-application allowlist yet; supported routed traffic is system-wide.

## Recovery and local data

Protected route journal:

    C:\ProgramData\Link Alloy\tunnel-cleanup.json

Protected event log:

    C:\ProgramData\Link Alloy\logs\engine.jsonl

The journal is validated before elevated cleanup. Link Alloy uses a single-instance lock, bounded startup/shutdown, protected state paths, and a recovery watcher for unexpected GUI exit.

## Engine smoke test

Build and run the controller without the GUI:

~~~powershell
go build -mod=mod -o bin\linkalloy-engine-smoke.exe .\cmd\linkalloy-engine-smoke
.\bin\linkalloy-engine-smoke.exe -duration 30s
~~~

The smoke test requires elevation, opens eight HTTPS connections, checks egress paths, prints counters, and restores routes. Use it only when a short system-wide route handoff is acceptable.

Recover a saved cleanup record without starting a tunnel:

~~~powershell
.\bin\linkalloy-engine-smoke.exe -recover-only
~~~

## Build on Windows

Prerequisites: Go 1.27.3, Node.js 26, pnpm 10.34.9, and Wails v3 CLI v3.0.0-alpha2.119.

~~~powershell
go mod tidy
pnpm --dir frontend install --frozen-lockfile
wails3 generate bindings -clean=true -ts -i
pnpm --dir frontend build
wails3 task windows:build
~~~

This repository is Windows-only. Android, iOS, Linux, macOS, and Docker build scaffolding is not part of the product.

## Validation status

Local checks include Go tests, lifecycle tests, go vet, packet-parser fuzzing, Go vulnerability scanning, frontend state tests, production frontend build, dependency audit, Wintun hash/signature verification, and a Windows executable build.

The public release remains blocked until forced-process recovery, physical link-loss failover, a controlled launcher comparison, and Windows signing secrets are completed.

## Independent implementation

Link Alloy’s scheduler, flow mapping, adapter-bound dialing, route lifecycle, health orchestration, logging, and frontend are authored here. No application source files from HypoMux, HypoMuxPlus, Speedify, or another multi-link product are included. gVisor and Wintun are separate third-party components; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## License and attribution

Link Alloy uses the custom Link Alloy Source-Available Attribution License in [LICENSE](LICENSE).

You may view the repository and run official unmodified releases. You may make a private local copy for evaluation or internal use. Redistribution, repackaging, forks as products, modified public builds, source reuse, and competing products require written permission from Sayan Pramanik.

Any permitted public reference must retain the notice and state:

> Based on Link Alloy by Sayan Pramanik — https://github.com/sayanpramanik2012/Link-Alloy

Third-party components remain under their own licenses.

