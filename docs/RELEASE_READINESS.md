# Release readiness

release-ready: false

The readiness flag blocks signed/stable releases. The tags `v0.0.1` and `v0.0.2` are explicitly permitted unsigned functional alphas and are published as portable prereleases only. Version 0.0.2 distributes application/service connection cohorts, adds bounded per-flow diagnostics, and provides a standalone EXE with embedded notices. Its real launcher comparison remains pending; publishing an alpha does not satisfy the stable gates below.

The Link Alloy Go engine and dashboard are implemented. Local tests cover stable user-configured allocation, lifecycle cancellation, protected-journal validation, IPv4/IPv6 flow translation, bounded TCP/UDP forwarding, packet parsing fuzzing, and PowerShell route-script parsing. A 15-second elevated code-only smoke confirmed Wintun startup, Windows route selection, eight HTTPS flows across two egress paths, and route/address cleanup on normal stop. Keep releases blocked until these remaining behaviors are verified:

- Crash-watcher recovery after killing the process while the tunnel is active.
- Link health failure and new-connection failover across independent adapters.
- A controlled real application download with the app's measured bytes and rates on each link.
- Windows code-signing certificate secrets configured for the protected public-release workflow.

The current prototype routes unfragmented IPv4/IPv6 TCP and UDP. ICMP and fragmented packets are not handled. Existing flows may need to reconnect when their public route changes; no speed increase is guaranteed.

After the first functional alpha, release automation retains only the two newest GitHub Release records.
