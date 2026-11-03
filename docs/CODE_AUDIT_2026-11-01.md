# Link Alloy code audit — 1 November 2026

Scope: current local working tree in `C:\Users\sayan\Documents\GitHub\Link-Alloy`, including untracked engine code, Windows route management, transport forwarding, dashboard, packaging, and CI/release workflows. This is a source review with dependency scanning, not a penetration test or a guarantee that every defect has been found. No active routes or running app settings were changed for this audit. Application source was not changed by this audit.

## Remediation update — 2 November 2026

The findings were used as the implementation backlog. The priority-one items are now addressed in the local working tree: strict protected state and journal validation, a global process lock, cancellable startup, nonfatal flow-capacity handling, bounded transport admission, safe packet-buffer lifetime, fixed-window telemetry, per-family health, bounded logging, persistent link controls, a redesigned dashboard, Vite 8.1.6, and signed-release gates. The remaining release gates are live validation tasks rather than unimplemented code: forced-process recovery, physical link-loss failover, a controlled real application comparison, and release certificate provisioning. The original finding text remains below as the baseline audit record.

### Scheduler follow-up — 4 November 2026

The scheduler no longer treats connection counts as the only signal. It keeps the configured weights as the target, samples a fixed two-second byte window, and steers new flows away from a link carrying the larger recent Link Alloy load. A regression test covers the saturated-Ethernet/idle-Wi-Fi case. This improves multi-connection launchers, but it does not split an already-established TCP connection; a single Steam flow still cannot be turned into 100 Mbps + 100 Mbps without protocol-level multipath or a remote relay.

Priority: P1 means fix before another public distribution; P2 means fix before calling the alpha reliable; P3 means maintenance or product completeness. Findings below are based on inspected code. Runtime consequences not reproduced are identified as such.

1. **P1 — Elevated recovery interpolates untrusted journal strings into PowerShell.**

   Sources: `internal/engine/route_manager_windows.go:188`, `:208`, `:263`, `:344`, `:378`, `:411`; `network_service.go:58`.

   The cleanup record lives under the user's writable AppData directory. Recovery validates its version and numeric interface identifiers, but does not parse/allowlist address and next-hop strings before interpolating them into quoted PowerShell source. Recovery runs automatically when the elevated app opens. A locally modified record can therefore influence elevated shell code. This is a concrete privileged-input injection path; no payload was executed in this audit. Interface verification checks LUID/index agreement but does not prove the interface is the expected Link Alloy adapter, so arbitrary matching physical-interface identifiers must also be rejected.

   Fix: replace shell interpolation with Windows networking APIs where practical. Independently validate all record fields against fixed supported addresses, next hops, adapter GUID/type, and expected interface identity. Store privileged state with explicit Windows ACLs in a protected location. Do not rely on POSIX-style `0600`/`0700` modes as a Windows privilege boundary.

2. **P1 — Opening a second instance can remove the first instance's live routes.**

   Sources: `network_service.go:58`, `internal/engine/route_manager_windows.go:47`, `:92`; `main.go:15`.

   Every new app instance calls recovery before it knows whether another instance owns the tunnel. The journal contains no process/session ownership information, and there is no cross-process mutex. A second normal/candidate app can treat the live first instance as stale and clean up its routes. Locks inside the Go structs protect only one process.

   Fix: acquire a named Windows mutex before recovery or tunnel creation; record session ownership and verify it is stale before cleanup. Focus the existing window or fail cleanly on a second launch. Test overlapping GUI and smoke-tool launches.

3. **P1 — Stop cannot cancel an in-progress Start.**

   Sources: `internal/engine/controller.go:60`, `:112`, `:133`; `network_service.go:92`; `main.go:34`.

   Start sets `starting` and performs discovery/route installation before publishing a session. Stop sees a nil session during that interval and returns success. Startup can then finish and install routes after a stop request. Closing the app during startup is particularly important because shutdown calls Stop through this same path. The GUI disables its toggle while busy, but that does not serialize backend calls or application exit.

   Fix: serialize lifecycle operations, retain a startup cancellation function/completion channel, and make Stop wait for cancelled startup and rollback. Test close/stop during every startup phase.

4. **P1 — Flow-port exhaustion stops the entire tunnel.**

   Sources: `internal/engine/nat.go:15`, `:92`, `:193`; `internal/engine/forwarding_stack.go:174`, `:250`, `:275`.

   A protocol has 20,001 virtual ports. Allocation failure returns `ErrVirtualPortsFull`, and the capture loop treats that as a fatal data-path error. TCP mappings are not removed when upstream dialing or endpoint creation fails; they can remain for the 24-hour NAT idle period. An ordinary local app generating enough failed/new connections can therefore exhaust the table and bring down routing for every application.

   Fix: reject/reset only the affected flow when capacity is reached, reclaim failed mappings immediately, and enforce bounded admission with useful counters. Add a test that fills the table while established transfers continue.

5. **P2 — Active connection resources and idle TCP sockets lack effective limits.**

   Sources: `internal/engine/forwarding_stack.go:32`, `:93`, `:269`, `:342`, `:386`; `internal/engine/nat.go:174`.

   The TCP forwarder limit governs in-flight requests rather than the total established bridge population. UDP has no explicit admission limit and allocates two 65 KB buffers per bridge. TCP copies have no idle deadline. NAT expiration removes mappings but does not itself close the corresponding bridge/socket or release its scheduler lease. Many idle or half-closed sessions can retain goroutines, sockets, and misleading active counts.

   Fix: explicit total/per-protocol limits, coordinated flow ownership, TCP idle/half-close deadlines, and expiration that closes transport resources. Load-test steady state and teardown.

6. **P2 — Adaptive weights use workload as a substitute for link capacity.**

   Sources: `internal/engine/scheduler.go:78`, `:130`, `:175`, `:203`, `:231`.

   A link with fewer assigned download bytes looks slower even when it has unused capacity. Its weight drops, so it receives fewer new connections, reinforcing the imbalance. Conversely, the busy link can get more work. Weight updates run on Acquire and on every Snapshots call, including dashboard polling and internal connection attempts. The same rolling samples can be incorporated repeatedly, so opening/polling the UI and creating connections changes adaptation speed. These are separate issues from the Wi-Fi health-check badge.

   Fix: update on one fixed clock using non-overlapping samples; separate capacity estimates, saturation, latency, failures, and actual demand. Add exploration/minimum service and bounded weight changes. Test asymmetric links, sparse flows, bursty downloads, and different dashboard poll frequencies.

7. **P2 — Displayed instantaneous rates can exaggerate bursts.**

   Source: `internal/engine/scheduler.go:139`.

   Samples from the last second are divided by time since the first retained sample, which can be very short. A fresh burst can therefore show a much higher rate than a fixed-window average. This can contribute to the apparent seesaw between MB/s and KB/s even without a route failure.

   Fix: fixed buckets or monotonic byte deltas over a defined interval; show a smoothed rate and retain totals. Validate against OS interface deltas while accounting for protocol overhead and other processes.

8. **P2 — Health checks still depend on one external service and do not distinguish IP families.**

   Sources: `internal/engine/controller.go:227`, `internal/engine/link_health.go:7`, `internal/engine/dialer_windows.go:75`.

   The candidate now requires two consecutive probe failures. That is a mitigation, not a complete health model. All links depend on one HTTPS endpoint, system DNS, and an eight-second timeout; probes are sequential. A service/DNS problem or congestion can mark a usable link down. Successful IPv4 connectivity is also not proof the adapter can carry IPv6 destinations, and vice versa. We observed the dashboard label Wi-Fi unavailable while existing Wi-Fi transfers continued; the exact failing probe response was not recorded.

   Fix: record safe failure categories, test multiple independent signals/endpoints, track IPv4 and IPv6 eligibility separately, probe links concurrently, and distinguish degraded/probing/unavailable. Preserve the two-miss protection and validate actual disconnect/reconnect behavior.

9. **P2 — Connected adapters are discovered only at tunnel startup.**

   Sources: `internal/engine/controller.go:80`, `:236`, `:364`; `network_service.go:71`.

   The GUI can refresh Windows adapters, but the scheduler only monitors its original startup list. A newly connected USB tether/hotspot or recreated adapter index cannot join the active pool without a restart. The discovery loop also considers every up non-loopback interface rather than explicitly identifying physical upstreams and user choices.

   Fix: subscribe to adapter/address changes, reconcile the scheduler safely, exclude unsuitable virtual/VPN interfaces, and provide link inclusion/priority controls. Test hotplug, DHCP/address changes, sleep/resume, and interface-index replacement.

10. **P2 — PacketBuffer ownership is not released after inbound injection.**

    Source: `internal/engine/forwarding_stack.go:122`.

    The code creates a gVisor PacketBuffer inline and never releases its initial reference. The pinned gVisor PacketBuffer API gives the creator one reference and requires DecRef when finished; channel.InjectInbound dispatches without consuming that reference. This is a confirmed lifetime/API defect. The magnitude of heap/pool retention requires allocation profiling; no long-run memory claim is made here.

    Fix: hold the packet buffer in a variable and release the caller's reference after injection. Verify with release hooks/reference diagnostics and a sustained transfer allocation profile.

11. **P2 — Forwarder shutdown is outside the route-cleanup deadline.**

    Sources: `internal/engine/controller.go:195`, `:147`; `internal/engine/forwarding_stack.go:222`.

    runSession calls forwarder.Close, which waits for goroutines, before starting the bounded route cleanup. A stuck handler, transport operation, or synchronous event sink can prevent cleanup from being reached. Stop reports a timeout after 40 seconds but does not itself complete route restoration. TCP handler admission and WaitGroup lifetime during shutdown also need a concurrency test because handlers add workers asynchronously.

    Fix: stop admissions, cancel workers, bound the drain independently, and guarantee route restoration proceeds on timeout. Exercise blocked I/O, close during dial, and slow logging.

12. **P2 — Hard-crash recovery relies on restarting the app or recovery CLI.**

    Sources: `main.go:34`, `network_service.go:58`, `cmd/linkalloy-engine-smoke/main.go:33`; `docs/RELEASE_READINESS.md`.

    Normal-stop cleanup and stale-journal recovery exist. There is no separate service/watchdog that survives GUI termination and restores routes. Windows may remove the transient interface and its routes on process death, but that behavior and the remaining recovery path have not been validated by a forced-kill test in this implementation. A corrupt journal currently blocks recovery/start rather than offering a clearly bounded repair flow.

    Fix: test forced kill/reboot/power loss first, then choose a privileged service/watchdog design with protected journal ownership and an emergency repair path. Do not advertise seamless crash recovery until verified.

13. **P2 — Packet handling and temporary queue pressure can affect every app.**

    Sources: `internal/engine/nat.go:249`, `:314`; `internal/engine/forwarding_stack.go:174`, `:194`; `internal/engine/tunnel_device_windows.go:149`.

    All default traffic enters the tunnel, but ICMP, fragments, IPv6 ESP/AH, and other unsupported formats are dropped. A single Wintun send allocation error is returned as a fatal loop error, with no policy separating temporary queue pressure from permanent device failure. This can turn a temporary buffer condition into a whole-tunnel stop. UDP send/receive buffers and path-MTU behavior also need large-datagram testing.

    Fix: define recoverable versus fatal packet errors, count drops, handle backpressure safely, and add ICMP/PMTU and fragmentation support or clearly scoped bypass behavior. Avoid universal protocol-support claims.

14. **P2 — Logging can grow without bound and block forwarding.**

    Sources: `internal/engine/controller.go:280`, `:301`, `:324`; `internal/engine/forwarding_stack.go:380`.

    Each event synchronously opens/appends/closes a file under the logging mutex. There is no rotation or disk budget. Startup reads the complete log before keeping only the last eight MB, so the intended cap does not cap allocation. TCP completion emits the same flow-end event twice, inflating audit entries and any totals later calculated from those events. Runtime transfer counters are separate and are not doubled by this particular duplicate.

    Fix: bounded asynchronous logging, rotation/retention, tail reads, explicit disk-full handling, and one terminal event per flow. Verify the actual elevated-session log location; our earlier standard-profile file did not reflect the GUI's newer visible events.

15. **P2 — The dashboard can show stale success and hide an action error.**

    Sources: `frontend/src/App.tsx:59`, `:93`, `:108`; `network_service.go:108`.

    If any request in the refresh Promise.all fails, previous tunnel/link values remain displayed; there is no stale-state indicator or request timeout. The toggle catch sets an error and then calls refreshData, whose successful path clears that error. A stop-timeout error can therefore disappear while the user is left with an uncertain state. Link status also collapses one failed external check into the broad label 'No internet path'.

    Fix: preserve action failures separately, show the age of each reading, provide request deadlines and independent status refresh, and distinguish physical connectivity, probe status, and actual forwarding activity. Add component tests for startup/stop failure and stale responses.

16. **P2 — Vite 8.1.5 has five reported dependency advisories.**

    Source: `frontend/package.json`; pinned pnpm audit result: one high, three moderate, one low. All five list Vite 8.1.6 as the fixed version on the current minor line. The high issue concerns Windows launch-editor handling; see the [upstream advisory](https://github.com/vitejs/launch-editor/security/advisories/GHSA-587h-7wmj-h5pm). The non-Windows file-disclosure advisory has additional prerequisites and does not apply to a Windows-only runtime; see [its upstream scope](https://github.com/vitejs/vite/security/advisories/GHSA-rq7h-c2jc-7f22).

    These findings primarily concern development tooling. The packaged production app embeds static frontend assets rather than a Vite development server, so the scanner count is not proof of five exploitable flaws in the shipped EXE.

    Fix: update Vite to at least 8.1.6 on the chosen release line, regenerate the lockfile, rebuild, and rescan. Add dependency checks to CI. The Go source scan reported no known reachable vulnerabilities; that does not cover our custom logic, WebView2 runtime, or every native component.

17. **P2 — Release/install lifecycle and source completeness are unfinished.**

    Sources: `build/windows/nsis/project.nsi:114`, `.github/workflows/release.yml:88`, `build/windows/wails.exe.manifest:23`; current git status.

    The installer/uninstaller does not coordinate stopping a live tunnel before deleting files. The release workflow does not sign the app/installer, even though signing tasks exist. The complete GUI runs elevated rather than limiting privilege to a small authenticated service. Most importantly, `internal/`, `cmd/`, Wintun runtime files, and the verification script are currently untracked: the engine being tested locally is not yet represented by the tracked commit that CI/release would build.

    Fix: commit the reviewed source/runtime inventory, exercise clean-clone packaging, handle upgrade/uninstall while active, sign distributed binaries, and move privileged network operations behind a narrow service boundary. Keep release readiness false until the blocking validations pass.

18. **P3 — Build/release hardening and project cleanup remain.**

    Sources: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `tools/retain-two-releases.ps1:24`, `build/`.

    Workflow actions use version tags rather than immutable commit hashes. Release jobs for different tags can overlap while pruning the same release list. There is no checked-in frontend test script, dependency audit step, packet fuzzing, race/lifecycle regression suite, or signed-artifact verification gate. Android/iOS/Linux/macOS/Docker scaffolding remains physically present despite Windows-only task changes; go test still discovers Android/iOS packages. Multiple candidate/root executables and a generated syso make it easy to launch the wrong build. The candidate's two-miss behavior has passed unit tests but has not been validated in a fresh live GUI session.

    Fix: pin action hashes, serialize publication/retention globally, add meaningful security and lifecycle gates, remove unused platform/build artifacts when permitted, and generate one clearly versioned handoff folder with provenance/checksums.

Known capability gaps that are not new vulnerabilities: one connection remains on one link; no packet bonding or guaranteed single-stream acceleration; no transparent migration of existing TCP sessions after a public-IP change; no per-app byte attribution/allowlist; no mobile-data budget or metered-link controls; no independent-link/shared-upstream detection; no tested VPN coexistence policy; no persistent per-link historical charts or exportable diagnostics. Some are explicitly documented limitations. They should not be presented as completed features.

Verification completed for this audit:

- GitNexus symbol context was used and source was inspected directly. The graph contains the latest health tracker.
- `go vet -mod=mod ./...` passed.
- `govulncheck` source scan completed with 'No vulnerabilities found'.
- Pinned pnpm 10.34.9 audit completed with the five Vite advisories above. The older default pnpm failed to parse the current dependency format; its failure was not treated as a clean scan.
- The full Go suite passed immediately before the audit for the health-check candidate. Those tests cover basic scheduler/NAT/TCP/UDP behavior and PowerShell parsing; they do not validate the issues above merely by passing.
- No exploit, forced-kill, adapter-disconnect, malicious-journal execution, or route mutation was performed during this audit.

Recommended order: fix items 1–4 first; then resource/packet lifetime and bounded cleanup; then redesign scheduler measurement and health diagnostics; then lifecycle/UI/install tests and a controlled EA comparison. Keep the game paused until the security and lifecycle blockers have been addressed. The health-check candidate alone is not a release-ready remedy for this full list.
