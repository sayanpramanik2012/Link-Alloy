# Multi-link downloads: research, evidence, and implementation plan

Date: 4 November 2026. Source reviewed: `77d40333f373fb435ec88c05d032f4c433ef42e0`.
Status: research baseline followed by the 0.0.2 implementation update below. Proposed phases are not all implemented.
Delivery rule updated by user request: implement and verify locally, push, publish v0.0.2 as a standalone portable EXE, then user tests the release.

## Version 0.0.2 implementation update

Implemented: Windows IPv4/IPv6 TCP owner attribution with process creation identity, application/service/family cohorts, atomic smooth weighted rotation, bounded cohort cache, connection start/end correlation, in-memory per-flow byte rates and bound-source diagnostics, and a filterable Connections table. Unknown ownership falls back to a protocol/service/family cohort. UDP remains in a separate unattributed cohort; it does not share a TCP application's rotation. Ordinary monitor refreshes preserve allocation credit; eligibility and policy changes reset it.

The prior global byte-pressure heuristic is removed. Active upload/download demand is not treated as a measurement of available link capacity. Instead each cohort rotates independently before any actual transfer bytes arrive, avoiding burst concentration and unrelated-traffic bias. This guarantees weighted assignment opportunities, not equal bytes, link saturation, or a download speed increase.

Verification includes concurrent burst allocation on 2/3/4 links with unrelated upload traffic, weighted shares, monitor reconciliation, disabled-link exclusion, fallback, bounded state, process-table parsing and live Windows socket ownership, diagnostic retention, and transport regression tests. Build and CI results are recorded in the release process. Sustained Steam/EA/Epic and physical failure tests remain pending user validation.

Deferred: automatic capacity estimation, CDN candidate optimization, application-owned range downloads, and relay packet bonding. No changes to Steam settings, TLS trust, or host DNS are included. The research and evidence below remain the baseline that motivated this release; statements of missing functionality refer to the source revision named at the top unless covered by this update.

## Decision

Pursue local aggregation of independent download connections first. Steam's own log proves this workload has several content connections, so a local solution deserves investigation before requiring a relay. Add reliable attribution and reproduce the assignment defect before changing routing policy again.

Keep a separate design for packet bonding through a remote endpoint. That is the route to aggregation of an opaque single connection without requiring the destination application to understand multiple paths. It is not necessary to begin the Steam investigation, and deploying infrastructure is outside this research change.

The requirement is one Windows control app with a pool of N adapters. The number of adapters is not the main obstacle: transport identity, available request parallelism, and return-path control determine what can be combined.

## Corrections to earlier conclusions

- The EA experiment demonstrated concurrent traffic on both adapters. It did not establish universal packet bonding or a controlled speed increase.
- The Steam screenshots and adapter samples showed a particular interval dominated by one adapter. They did not establish a single content connection.
- Steam's local log now confirms several content connections. A relay is therefore not established as necessary for this specific multi-connection workload.
- A working EA test does not establish Steam, Epic, or arbitrary protocol compatibility. Each workload needs its own result.
- The load-aware scheduler patch is a candidate heuristic. Passing its one-flow regression test does not establish balanced launcher downloads.

## Evidence from this computer

Read-only source: `C:\Program Files (x86)\Steam\logs\content_log.txt`.
The following entries are historical observations, not a claim that a download is running now.

| Local time on 4 November | Observation |
| --- | --- |
| 02:22:31 | App 1172470 requests 5,273 chunks; Steam reports HTTP/2 available. |
| 02:22:32 | Four HTTPS content connections negotiate HTTP/2 to `cache2-sgp1`, `cache9-sgp1`, `cache1-sgp1`, and `cache10-sgp1.steamcontent.com`. |
| 02:22:44 through 02:23:57 | Steam raises its target download-connection count from 9 to 12. These are targets, not proof of 12 physical TCP sockets. |
| 02:23:45 and 02:24:46 | Steam reports 244.241 and 308.159 Mbps respectively. |
| 02:28:02 | Per-node summaries record approximately 577 MB, 1,071 MB, 650 MB, and 2,196 MB; total 4,494,061,920 bytes over 149 seconds, reported average 242.08 Mbps. |

The four content destinations in that session were `103.10.124.85`, `.68`, `.84`, and `.69`, TCP 443. These addresses are evidence for that session only, never permanent routing rules.

Earlier live samples in the conversation showed approximately 109-118 Mbps on Ethernet with negligible Wi-Fi; after restarting with the candidate, another interval showed approximately 196.6 Mbps on Wi-Fi and negligible Ethernet. These were different windows. Their totals must not be added or treated as a controlled baseline.

The content log has no physical-adapter assignment. Existing Link Alloy events have no common flow identifier tying captured app tuple, selected link, and live byte counters together. Consequently, the record cannot determine which of those four content connections used each link or whether the later 308 Mbps interval used both links.

## What the primary sources establish

### Steam content is already divided into requests

Valve describes SteamPipe files as chunks of roughly 1 MB, compressed and encrypted before distribution and expanded on the client. Steam's installation process also has substantial disk work. [Valve SteamPipe documentation](https://partner.steamgames.com/doc/sdk/uploading)

Our local log independently establishes HTTPS/HTTP/2 content connections for this download. HTTP/2 multiplexes multiple request streams within a TCP connection; thousands of chunks do not imply thousands of independently schedulable sockets. [RFC 9113, sections 2 and 5](https://www.rfc-editor.org/rfc/rfc9113.html)

### HypoMux has two relevant mechanisms

Its connection scheduler assigns each new connection to an adapter. It explicitly distinguishes connection shares from traffic ratios and does not move existing TCP sessions. [HypoMux aggregation documentation](https://hypomux.com/en/docs/aggregation)

Its separate experimental Steam optimizer discovers candidate CDN nodes using the egress adapters, verifies them, and trials candidates for new connections. It preserves TLS and original-node fallback. That suggests a possible additional local optimization; it does not prove the same algorithm is needed here or that it guarantees a gain. We consulted its documentation, not copied its engine. [HypoMux Steam optimization documentation](https://hypomux.com/en/docs/tools-steam)

### Packet bonding has a cooperating endpoint

Speedify describes a TUN client that sends packets across links to its servers, where packets are reordered and forwarded, with responses returned through the bonded links. Its single-session aggregation includes the server component. [Speedify architecture](https://support.speedify.com/article/273-how-speedify-works)

MPTCP defines multiple subflows and connection-level reassembly. If the peer does not negotiate MPTCP, it falls back to ordinary TCP. Enabling a local option cannot make every CDN support it. [RFC 8684, sections 3.1 and 3.7](https://www.rfc-editor.org/rfc/rfc8684.html)

### A virtual adapter captures packets; it does not merge ISP identities

Wintun gives userspace an interface to read and write layer-3 packets. It does not provide a second-endpoint bonding service. [Wintun](https://www.wintun.net/)

Ordinary TCP identifies a connection by its local and remote sockets. With separate ISP NAT mappings, alternating packets through a different public source does not make them part of the original server-side connection. Sending an acknowledgment via Wi-Fi also does not instruct the server to send the next response over Wi-Fi. This is the architectural reason a local virtual machine, bridge, or TUN alone cannot transparently bond arbitrary single TCP sessions. [RFC 9293, section 2.2](https://www.rfc-editor.org/rfc/rfc9293.html)

### Request splitting is possible for a narrower workload

A downloader can issue concurrent byte-range requests using different interfaces, then assemble the file locally. Servers may ignore ranges, so correctness requires validated 206 responses, Content-Range, a stable representation, validators, and a safe single-request fallback. [RFC 9110, section 14](https://www.rfc-editor.org/rfc/rfc9110.html)

This works when Link Alloy owns the download request. Transparently rewriting another app's HTTPS requests would require access to its protected application data. TLS protects those records; copying encrypted bytes into another TLS connection is not equivalent to creating the same request. Do not install a certificate or intercept TLS as part of the default solution. [RFC 8446](https://www.rfc-editor.org/rfc/rfc8446.html)

## Current engine findings

Reviewed files: `internal/engine/scheduler.go`, `scheduler_test.go`, `forwarding_stack.go`, `dialer_windows.go`, `events.go`, and `nat.go`.

| Finding | Consequence | Confidence |
| --- | --- | --- |
| `openUpstream` acquires one lease, dials one interface, and `bridgeTCP` retains that socket. | Existing flows cannot move when weights change. | Confirmed in source. |
| Scheduler pressure uses aggregate link bytes and all active flow counts, including unrelated traffic and UDP. | Background uploads/control traffic can bias placement of the next Steam content connection. | Mechanism confirmed; contribution to this incident unproven. |
| Initial assignment has no workload identity or content-connection cohort. | Background sockets can consume round-robin positions before the actual download sockets arrive. | Confirmed missing context; actual assignments require instrumentation. |
| A two-second demand window is used as load pressure. | Low demand does not establish spare capacity, and high demand does not prove saturation. | Confirmed measurement limitation. |
| After a busy-link sample, many new flows may select the idle link before their traffic becomes visible. | Burst placement can concentrate a whole new content cohort on one adapter. | Testable hypothesis. |
| Deficits accumulate even when pressure overrides deficit selection. | Prolonged imbalance can leave accumulated scheduling credit; bound/reset it around policy and eligibility changes. | Confirmed source behavior; fairness effect needs testing. |
| No per-flow live-byte view, process attribution, or dial endpoint comparison. | Cannot distinguish bad placement, bind failure, stalled CDN, or Steam request preference from aggregate counters. | Confirmed diagnostic gap. |
| No per-adapter CDN discovery or ranking. | A content node selected through one provider may perform poorly through another. | Missing feature; performance effect is a hypothesis. |
| The new test asks only that the next flow avoid the busy link. | It cannot detect four simultaneous content flows concentrating on one link, or regression with unequal capacities. | Confirmed test gap. |

## Implementation sequence

### 1. Make the actual assignment observable

Add a bounded in-memory flow registry with a session-local flow ID, app PID when resolvable, original tuple, selected interface, actual upstream local address, dial duration, start/end times, separate sent/received counters, and failure category. Emit correlated start/end events and expose a bounded live snapshot. Sample outside the packet loop. Keep endpoint details out of persistent logs by default; diagnostic export is explicit and sanitized.

Resolve Windows TCP ownership using owner-PID tables, matching the original app tuple before NAT. Handle IPv4/IPv6, short-lived sockets, PID reuse, missing matches, and process exit. Never block forwarding indefinitely for process resolution. Unknown ownership remains unknown. The documented API is [GetExtendedTcpTable](https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable).

First reproduce a four-content-connection Steam sample and identify the bytes carried by every physical path. Check actual bound source addresses against selected interface addresses. This settles whether the failure is placement, binding, or performance after placement.

### 2. Correct assignment using the reproduced failure

Separate configured preference, observed traffic, and measured capacity. Keep upload and download demand distinct. A general per-app cohort can distribute newly opening connections across eligible adapters before applying cautious load adjustments; it must also work for unknown processes and other launchers.

Reserve placement immediately at admission so concurrent opens see pending work. Release reservations on failure, completion, cancellation, link removal, and shutdown. Include eligibility per IP family. Bound scheduling credit and exploration so a snapshot cannot send every connection in a burst to the same link. A PID alone cannot identify which sockets carry content; verify data after placement and do not assume every socket is a download.

Do not forcibly close healthy TCP flows to fake redistribution. Do not inject extra authenticated requests or replay writes. Fresh user-resumed connections provide the first validation opportunity.

### 3. Evaluate content-node performance only if placement is correct

If content connections are distributed but one path receives little useful data, compare the same original content endpoints on each adapter using a bounded, explicitly selected test resource. Do not replay private URLs or credentials from logs.

Only then consider a separate opt-in Steam adapter: domain-aware candidate discovery, bounded probes, TLS and content validation, original-host preservation, cooldown, and immediate original-node fallback. DNS/IP substitution must preserve server identity and cannot redirect arbitrary sessions to unrelated servers. Keep this feature outside the general scheduler. Do not change Steam's undocumented console settings or disable HTTPS by default.

### 4. Optional local download accelerator

For explicit URLs supplied to a Link Alloy downloader, implement adaptive parallel range requests across N links with bounded buffers, validator consistency, offset validation, final integrity checks, and cancellation. This can demonstrate request-level aggregation for supported resources. It is a separate capability and cannot be advertised as transparently accelerating every Windows application.

### 5. Universal transport option

For opaque sessions with insufficient request parallelism, a complete implementation needs a cooperating destination or remote relay. Proposed relay architecture:

```text
Windows applications -> Link Alloy TUN -> per-interface authenticated transports
                                          Ethernet -----+
                                          Wi-Fi --------+-> relay -> destination
                                          USB / others -+
```

The client and relay coordinate both directions, with per-path congestion control, session authentication, path validation, replay protection, bounded queues/reordering, MTU management, loss recovery, and fair scheduling. The relay supplies a stable outward session identity. Preserve inner end-to-end TLS; the relay forwards encrypted application bytes. Use established encrypted transport libraries rather than inventing cryptography. A local VM alone is not the remote convergence point.

Start with a small TCP-stream bonding experiment if this option is chosen; supporting arbitrary IP packets, UDP/QUIC, and crash-safe system routing is a larger follow-up. Do not assume multiple ordinary QUIC connections automatically become one bonded session.

No relay is provisioned, purchased, or exposed by this document.

## Validation gates and acceptance criteria

1. Unit/simulation tests: 2, 3, and 4 links; equal/unequal capacity; simultaneous cohort opens; unrelated uploads; idle control sockets; slow/failed dials; IPv4-only/IPv6-only links; zero demand; hotplug; bounded credits; polling independence; teardown releases reservations. Include a negative test proving one opaque TCP flow remains on one link in direct mode.
2. Local controlled transfer: independently bound single-link baselines, then parallel transfers through the tunnel against a controlled server. Verify content hashes, useful payload bytes, per-link assignment, and aggregate application throughput. Use a server that is not itself the bottleneck.
3. Real Steam: same game and download region, fresh content connections, Ethernet-only and Wi-Fi-only reference windows, then combined windows; repeat in a different order. Record per-flow useful bytes/rates, physical adapter deltas, Steam progress, CPU and disk activity. Exclude verification/staging-only windows and health-probe bytes. Avoid unrelated downloads during measurement.
4. Repeat separately with EA and Epic; neither inherits a pass from Steam. Include ordinary browser/HTTP downloads, HTTPS with HTTP/2, and a single-stream case to establish boundaries.
5. Fault/lifecycle: stop, cancellation, route restoration, crash recovery, link loss/recovery, and no network regression after app exit.
6. Publish v0.0.2 with build identity and checksum as explicitly requested, then run the user acceptance test. Existing v0.0.1 assets do not acquire changes automatically.

For a controlled 100 Mbps + 100 Mbps setup with sufficient offered work, use a provisional engineering target of at least 160 Mbps useful aggregate throughput, with each adapter carrying substantial verified payload over overlapping 60-second windows in repeated runs. This is a test target, not a promise for internet downloads. A result below the best single-link baseline is a regression. An internet Steam test needs repeatable improvement over its best contemporary baseline; a peak or nonzero counters alone is insufficient.

Percent controls must distinguish preference from a real rate cap. An optional maximum-use cap requires measured or user-specified capacity; 80% of 100 Mbps is an 80 Mbps ceiling, not an instruction that creates 80 Mbps of demand. Any automatic capacity test must be bounded and visible, especially on metered links.

## Next validation after 0.0.2

The diagnostics and cohort/burst correction are implemented in 0.0.2. Test the released EXE with a real Steam download, inspect content connections and their actual per-link bytes, and compare sustained throughput against single-link baselines. If placement is balanced but one path still stalls, continue with the content-node investigation in phase 3. Universal local-only packet bonding is not a completed capability.
