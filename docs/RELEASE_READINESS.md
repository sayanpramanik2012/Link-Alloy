# Release readiness

release-ready: false

The readiness flag blocks signed/stable releases. The tag `v0.0.1` is an explicitly permitted unsigned functional alpha and is published as a portable prerelease only.

The Link Alloy Go engine and dashboard are implemented. Local tests cover stable user-configured allocation, lifecycle cancellation, protected-journal validation, IPv4/IPv6 flow translation, bounded TCP/UDP forwarding, packet parsing fuzzing, and PowerShell route-script parsing. A 15-second elevated code-only smoke confirmed Wintun startup, Windows route selection, eight HTTPS flows across two egress paths, and route/address cleanup on normal stop. Keep releases blocked until these remaining behaviors are verified:

- Crash-watcher recovery after killing the process while the tunnel is active.
- Link health failure and new-connection failover across independent adapters.
- A controlled real application download with the app's measured bytes and rates on each link.
- Windows code-signing certificate secrets configured for the protected public-release workflow.

The current prototype routes unfragmented IPv4/IPv6 TCP and UDP. ICMP and fragmented packets are not handled. Existing flows may need to reconnect when their public route changes; no speed increase is guaranteed.

After the first functional alpha, release automation retains only the two newest GitHub Release records.
