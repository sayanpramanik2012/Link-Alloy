# Release readiness

release-ready: false

The readiness flag blocks signed/stable releases. The tags `v0.0.1`, `v0.0.2`, `v0.0.3`, and `v0.0.4` are explicitly permitted unsigned alphas and are published as portable prereleases only. Version 0.0.3 added TLS service grouping, family-specific allocation stability, improved UI, and bounded diagnostic tools. Version 0.0.4 adds a fixed-window dashboard and tested fixes for blocking UDP setup and retry-induced scheduling resets. The user authorized 0.0.4 on 6 November for local testing with the remaining issues documented. Publication does not satisfy the stable gates below.

The [5 November review](LOCAL_CODE_REVIEW_2026-11-05.md) identifies open watchdog/cleanup, TCP half-close, startup-policy, diagnostics, and overall dial-budget issues. These are not fixed by 0.0.4. Do not use this alpha for deliberate crash or adapter-loss testing; stop the tunnel normally.

Observed 4 November short samples: direct Ethernet 94–124 Mbps; direct Wi-Fi 30–31 Mbps; corrected timed test through the tunnel about 87 Mbps; Steam before pausing about 105–109 Mbps. These are sequential samples with variable network conditions, not a proven combined-speed increase or a conclusive measurement of engine overhead. The post-resume comparison was interrupted and remains incomplete. See [diagnosis and measurement record](DOWNLOAD_DIAGNOSIS_2026-11-04.md).

The Link Alloy Go engine and dashboard are implemented. Local tests cover stable user-configured allocation, lifecycle cancellation, protected-journal validation, IPv4/IPv6 flow translation, bounded TCP/UDP forwarding, packet parsing fuzzing, and PowerShell route-script parsing. A 15-second elevated code-only smoke confirmed Wintun startup, Windows route selection, eight HTTPS flows across two egress paths, and route/address cleanup on normal stop. Keep releases blocked until these remaining behaviors are verified:

- Crash-watcher recovery after killing the process while the tunnel is active.
- Link health failure and new-connection failover across independent adapters.
- A controlled real application download with the app's measured bytes and rates on each link.
- Windows code-signing certificate secrets configured for the protected public-release workflow.

The current prototype routes unfragmented IPv4/IPv6 TCP and UDP. ICMP and fragmented packets are not handled. Existing flows may need to reconnect when their public route changes; no speed increase is guaranteed.

After the first functional alpha, release automation retains only the two newest GitHub Release records.
