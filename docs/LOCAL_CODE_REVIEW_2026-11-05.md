# Link Alloy: local UI revision and engine review

Reviewed 5 November 2026 against main `f9d7031`, with the local changes described below. Nothing in this review was pushed, tagged, or released. No live tunnel, Windows route, launcher, or adapter settings were changed.

Publication update, 6 November: the user subsequently authorized publishing the reviewed changes as the 0.0.4 testing alpha. The findings below retain their status; this release does not fix R1–R5 or establish throughput acceptance.

## Verdict

The local client architecture is feasible for distributing independent TCP/UDP connections over multiple Windows adapters. It is not equivalent to the original transport implementation and has not met the sustained throughput acceptance target. Equal scheduling weights allocate connection attempts, not equal bytes or full link capacity. The implementation uses third-party Wintun, gVisor, Wails, and Go libraries; original application integration does not make those dependencies original Link Alloy code.

Two additional engine defects were reproduced with failing tests and fixed locally. Important recovery and protocol issues remain below. Passing unit tests and producing an executable do not establish universal Windows compatibility or a performance pass.

## Scope and method

Read the startup/service/controller paths, Windows interface discovery and socket binding, family health checks, preferences, scheduler, flow diagnostics and Windows process attribution, packet parsing/NAT/checksums, gVisor TCP/UDP forwarding, Wintun runtime/device handling, protected state and route scripts, watchdog/process locking, frontend state/polling and views, build tasks, and CI/release configuration. Inspected existing regression tests and ran the checks listed below.

GitNexus CLI points to the correct checkout and base commit. Its MCP instance points to a different registry, so the CLI was used. Keyword queries and symbol context work; vector search is unavailable. Callback edges are incomplete: for example, impact reports no callers for `handleUDP`, although source explicitly registers it with gVisor. Source and executed tests take precedence over missing graph edges. This is a focused source/functional review, not an exhaustive penetration test or a proof that every OS race is absent.

The post-edit index refresh did not finish and was cancelled. The existing index does not contain the newly added `connectUDP` symbol; use source/tests for the local diff until a refresh completes.

## What the earlier setup actually did

The retained runner is `outputs/dual-network-prototype/launcher-tun.js` under the earlier prototype workspace. Its configuration explicitly selects:

- HypoMux engine version `v2.8.2`, `mode: tun_tcp_pool`, `weighted: true`, **`strategy: weighted`**, and a 6-second connect timeout.
- sing-box TUN with **`stack: system`**, IPv4 address `198.18.0.1/30`, MTU 1492, and process rules for the selected launchers.
- A local SOCKS aggregation channel backed by the interface-bound HypoMux pool. Other processes use the Ethernet direct path.

The inspected HypoMux source snapshot is `c94a9c4af70af14410f94c8a967e35b3ad75719a`; local changes there are documentation/site changes. Its `scheduler.go` and `adaptive.go` support additional adaptive/performance machinery, but the saved runner requests weighted mode. The adaptive implementation specifically avoids changing the full pool's learning for a constrained retry. Do not describe this runner as an adaptive-mode test or infer that inspecting this source proves every detail of the historical executable's build.

| Area | Earlier runner | Current Link Alloy |
| --- | --- | --- |
| Capture/transport | sing-box system stack → local SOCKS channel | Wintun → packet translation → gVisor → upstream socket |
| Scope | Selected launcher processes, IPv4 | Routed supported TCP/UDP, IPv4 and IPv6; more-specific Windows routes can bypass the split defaults |
| Scheduling | Explicit weighted pool | Weighted rotation per application/service/family cohort |
| Binding | HypoMux interface-bound sockets | Windows IP_UNICAST_IF/IPV6_UNICAST_IF plus local-address verification |
| TLS | Passed through the local proxy path | Initial visible ClientHello inspected on 443/8443, then bytes relayed unchanged |
| Recovery | Console controller and separate restore script | Protected journal, route identity checks, controller cleanup and watchdog |

Both approaches place a connection on one physical path. Neither saved implementation splits an arbitrary existing TCP stream across public source IPs. The old EA transfer showed distribution; it was not a controlled proof of full combined capacity. Replacing its transport stack is a material change, so its observed throughput cannot be inherited by the new code.

## Defects reproduced and fixed locally

### F1 — UDP startup could stall all captured traffic (high)

Source: `internal/engine/forwarding_stack.go`, `handleUDP`, `InjectCaptured`, `RunTunnel`.

Before the change, `handleUDP` called `openUpstream` synchronously. The pinned gVisor UDP forwarder invokes this handler synchronously through channel `InjectInbound`. A slow dial therefore blocks the shared receive/injection loop, including unrelated TCP traffic. This is not proof that normal Windows UDP socket creation always takes a long time; the defect is that any delayed dial is allowed to stall unrelated traffic.

Reproduction: hold the injected UDP dialer on a channel and check whether `InjectCaptured` returns within 100 ms. The released source fails with “one UDP dial stalled the shared captured-packet path.”

Local correction: register the UDP endpoint and queue its first datagram synchronously, then dial in an admitted worker. On failure, close the endpoint, remove its mapping, and release admission. Regression tests confirm the packet path remains available and shutdown cancels a pending dial without leaking its worker or mapping. Existing UDP echo forwarding still passes.

### F2 — failed-dial retries reset normal scheduling (medium)

Source: `internal/engine/scheduler.go`, `acquireCohort`; caller `openUpstreamForService`.

Retry exclusions were included in the cohort's eligibility fingerprint. Temporarily excluding a failed link cleared that cohort's credits; the next ordinary arrival changed the fingerprint again and cleared them again. This could repeatedly bias new assignments despite healthy normal eligibility.

Reproduction: allocate the first equal-weight connection, retry excluding that link, then allocate the next ordinary connection. Released code assigns the next ordinary connection to the first link again. The new regression fails before the correction.

Local correction: retries choose an eligible alternative without mutating the normal cohort's credits, fingerprint, or tie history. Alternate selection honors disabled links, family restrictions, exclusions, and weights. This preserves ordinary weighted rotation; it does not claim a retry can rebalance an already established stream.

## Remaining findings requiring fixes

| ID / priority | Evidence and impact | Recommended correction |
| --- | --- | --- |
| R1 / P1 | `main.go` logs watchdog startup failure but still allows normal operation. `StartRecoveryWatchdog` treats process creation as success without a child-ready handshake. On exit, `main.go` unconditionally stops the watchdog even when `Shutdown()` returns a cleanup error. `TunnelController.Stop()` returns nil once the session is gone, so a previous cleanup failure can also be masked during a later shutdown. | Establish watchdog readiness and expose its health. Preserve/retry journal recovery after cleanup errors, and retire the watchdog only after verified restoration. Add startup-failure, prior-stop-failure, and exit-cleanup-failure tests with injected dependencies before a live fault test. |
| R2 / P2 | `bridgeTCPWithPrefix` starts a fixed 15-second timer after either direction reaches EOF. Even if the remaining direction continuously transfers valid data, the timer still closes both sockets. This can truncate legitimate half-closed TCP protocols. Source-confirmed; not demonstrated as the Steam cause. | Bound inactivity or use a separate suitable completion policy rather than a fixed wall-clock limit on productive half-closed traffic. Test a stream that continues beyond the half-close timeout and an actually stalled peer. |
| R3 / P2 | `Start` copies preferences before publishing the session. `ConfigureLink` remains callable during startup; when session is nil it saves the new policy without applying it to the scheduler already being created. A later `Reconcile` preserves the scheduler's old weight/disabled values. A save during that interval can therefore appear successful but not affect the active session. Source-confirmed interleaving; no elevated integration reproduction performed. | Reject policy writes while starting/stopping in the backend, or serialize policy application with session publication. Disable the corresponding UI controls and test the interleaving with a blocked route install. |
| R4 / P2 | Runtime status supplies `logError` and `logDropped`, but the frontend does not render either. `Lease.Release` reports connected flows as `completed` even when a copy failed; TCP bridge errors are not retained in the flow diagnostic or event outcome. | Show logging degradation and preserve sanitized failure categories such as timeout/reset/clean EOF. Do not log payloads, full URLs, or remote addresses to achieve this. |
| R5 / P2 | `openUpstreamForService` gives each candidate a separate 10-second timeout, with no overall per-connection budget. With N unavailable candidates, setup can wait approximately N × 10 seconds. Failure history is not fed back to a target-specific short cooldown. | Introduce a total dial budget plus bounded per-attempt time and target/family-aware failure feedback. Avoid globally disabling an adapter because one CDN rejects it. |

## Risks and capability gaps, not proven root causes

- **Health-check DNS coupling:** `ProbeLinkFamily` binds its HTTPS sockets but uses hostname resolution through the default resolver. Once the TUN is active, resolver traffic may itself depend on the tunnel. A resolver/probe-service failure can look like lost link eligibility even when an existing content transfer works. Two failed rounds reduce flapping but do not remove this dependency. Test resolver failure separately from adapter loss; consider a controlled independent resolution/bootstrap path. No current live reproduction is claimed.
- **Transport performance unisolated:** Windows TUN packets are copied, translated and checksummed, processed through gVisor, then copied between sockets. Counters/deadlines take locks on the copy path. No controlled CPU/lock/allocation profile establishes which part, if any, explains the measured throughput deficit. Do not guess at buffer sizes or restore borrowed code as a substitute for profiling.
- **Coverage:** ICMP, fragmentation, IPsec and some IPv6 extension headers are dropped by `parsePacket`; there is no general bypass/reassembly implementation. This can break traffic captured by the global routes. TCP/UDP support is not support for every Windows protocol or every application behavior.
- **No demand/capacity controller:** byte counters are telemetry, not link-capacity estimates. Balanced connection counts cannot make a launcher offer equal work to every socket. SNI grouping also falls back for unavailable/encrypted names and does not classify plain HTTP traffic into distinct services.
- **Continuity:** established flows remain pinned. No transparent migration of ordinary TCP sessions on public-IP change is implemented. Health monitoring controls new-flow eligibility, not seamless recovery of existing flows.
- **Identity/configuration:** saved policies use friendly names, and adapter-kind labels are inferred from names. Renaming/replacing adapters and same-index replacement with old active flows need dedicated hotplug tests; a stable adapter identifier is preferable for saved policy.
- **Runtime distribution:** the unsigned portable EXE extracts its verified DLL beside itself. A read-only folder fails extraction. A writable application directory is not a privileged service installation boundary; handle-based extraction/loading and a protected installation directory deserve a separate hardening review. The current hash check is useful but is not a complete DLL loading threat model.
- **Evidence retention:** flow/service/process diagnostics are in memory and capped at 256 returned records. Rotating event logs omit destinations and failure details by design. A privacy-preserving diagnostic export is missing, making delayed throughput investigations harder.
- **Acceptance coverage:** long Steam/EA sessions, interrupted startup, abrupt process termination, watchdog failure, repeated pause/resume, suspend/resume, adapter removal/reappearance and simultaneous VPN use are not all covered by successful end-to-end tests for this candidate.

## UI changes made locally

- Fixed window shell with persistent navigation, status, actions and footer. No document/page scrollbar.
- The overview fits the supported desktop size, shows chart plus link rates, and pages additional adapters two at a time. All adapters remain included in totals and routing regardless of the displayed page.
- Settings split into Link policy, Health & logs, Diagnostics, and About & notices; detailed lists have bounded content areas rather than expanding the entire window.
- Blue/slate palette, system-font rate figures, clearer labels/contrast, and quieter surfaces. No animation or extra UI dependency added.
- Removed duplicated overview activity/model panels; the full Activity and Connections views remain available.
- Narrow windows and accessibility zoom retain bounded scrolling access rather than clipping controls. Long logs/tables/notices can still scroll within their own regions.
- The event list no longer announces every periodic log replacement through an ARIA live region.

## Verification and limits

- Both new defect regressions failed before the fixes and pass afterward. UDP cancellation/cleanup and the affected regression subset pass ten repeated runs.
- Full Go tests and `go vet` pass after the frontend build. An initial concurrent build attempt raced the frontend asset replacement and failed Go embedding; rerunning in dependency order passes. No product defect is inferred from that orchestration error.
- Frontend's eight state/format tests and production TypeScript/Vite build pass.
- govulncheck reports zero reachable vulnerabilities and zero affected imported packages, with seven module-level findings in paths the application does not call. This is not a clean bill of health for every third-party file.
- Browser preview checked at 1220×800 and 960×720. At 960×720, the document is 720 px high / 960 px wide and the two-card link region has equal content/client height (254 px), so neither page nor link-region scrolling is required in that sample. Pagination and Settings section switching work. Connections, Activity and Settings were opened; browser console reports zero errors and the expected Wails-outside-native warning.
- Screenshots in `output/playwright/local-review-*.png` contain clearly labelled sample values, not live throughput evidence. The candidate's native tunnel has not been activated in this review.
- Built a separate native candidate at `bin/LinkAlloy-Local-Review/Link Alloy Local Review.exe`, retaining the administrator manifest and embedded Wintun runtime. Base version metadata remains 0.0.3; its filename and companion README distinguish it from the published asset. The executable was not launched. A second 960×680 browser layout check with the development-only preview banner hidden also fits both displayed cards without scrolling; this is layout evidence, not a native runtime test.

## Next work order

1. Fix and test R1 recovery readiness/cleanup handling before another deliberate crash or route-failure test.
2. Correct R2–R5 with focused regressions; expose enough safe diagnostics to distinguish resets, inactivity, dial failures and logging loss.
3. Profile a controlled transfer through the real TUN against the same server, file sizes, concurrency and physical interfaces used by the direct tests. Record CPU, allocations, packet/drop counters, per-service payload, RTT and disk activity.
4. Repeat the same launcher download after a clean launcher restart, followed by long reuse and hotplug/failure tests. A faster short sample alone is not acceptance.

The earlier documented sequential measurements (Ethernet 123.53 Mbps, Wi-Fi 29.60 Mbps, tunnel 86.87 Mbps) remain historical failed acceptance evidence, not a fresh result for this candidate. No new combined-speed claim is made.
