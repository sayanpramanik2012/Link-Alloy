# Link Alloy — Product Overview

## Status

This is a new development line for Link Alloy. The current Windows preview only reads adapter metadata. It does not route application traffic or claim download acceleration.

## Product goal

Provide a simple Windows dashboard for users with multiple independent network links—Ethernet, Wi-Fi, USB tethering, and similar interfaces. The intended future engine will choose an egress interface for each new connection and show link health and traffic counters.

## Current implementation

The Wails desktop shell calls a new Go service that enumerates non-loopback Windows interfaces and reports their names, adapter type, administrative state, index, and MTU. It does not read IP addresses, change network settings, open sockets to remote hosts, create a virtual adapter, or start a proxy. The start control is disabled in this build.

## Planned routing model

The target design is local connection-level scheduling:

1. Capture app traffic in an all-app tunnel.
2. Assign each new connection to one selected physical adapter.
3. Keep that connection on the chosen path for its lifetime.
4. Send later connections to a healthy link if an earlier link goes down.

This is not packet-level bonding. An existing TCP session may reconnect after a path or public-IP change. The UI and release notes must not promise zero interruption or a speed increase.

## Inspiration and source boundary

The product requirements are informed by public documentation from HypoMux, HypoMuxPlus, and Speedify. The application source in this repository is separately authored; no application source files from those projects are included. This is an independent implementation of an established product category, not a novel multipath protocol.

## Milestones and acceptance

- Adapter inventory: implemented, read-only.
- Live adapter throughput: not implemented.
- Connection scheduler: not implemented.
- All-app TUN capture: not implemented.
- New-connection failover: not implemented.
- Existing-session continuity: not promised; it requires endpoint or remote-protocol support when link IPs differ.

A public functional release should wait until the routing engine is implemented, local tests prove cleanup on stop/failure, and a controlled multi-link test verifies actual traffic distribution. Any speed comparison must report the workload and test conditions.

## Licensing

Original Link Alloy code is all rights reserved. Third-party dependencies and generated materials remain governed by their upstream licenses. This notice applies only to source code whose rights Link Alloy controls.

## Versioning

`VERSION` reserves `0.0.1` for the first functional alpha. The release workflow checks the version, release notes, and a readiness flag before packaging, then retains only the newest two GitHub Release records.
