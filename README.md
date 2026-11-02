# Link Alloy

**An independent Windows multi-link networking application.** Link Alloy is designed to route supported Windows application traffic across Ethernet, Wi-Fi, USB tethering, and other active network links. It is general-purpose and does not require app-specific launcher configuration.

## Development status

The desktop dashboard is connected to a new Link Alloy Go engine. It uses stable, user-configurable connection-allocation weights, IPv4/IPv6 flow translation, interface-bound socket dialing, and bounded TCP/UDP forwarding. The engine, UI state, packet parser, and Windows build pass local checks. A 15-second elevated code-only smoke started Wintun, verified Windows route selection, sent eight HTTPS connections through two distinct egress paths, and confirmed the routes and tunnel addresses were restored. A real application download has not yet been measured under the hardened build, so no download-speed increase is claimed.

## How routing works

- A Windows TUN adapter captures supported system traffic while the tunnel is enabled.
- Link Alloy assigns each new TCP or UDP connection to one healthy network link and keeps that flow on the selected link.
- The scheduler opens the upstream socket bound to the selected Windows interface. Link eligibility is checked independently for IPv4 and IPv6; a repeated failed check changes only new-connection selection.
- When a link drops, new connections use another healthy link. Existing connections may need to reconnect when the public route changes; Link Alloy does not stripe one TCP connection across multiple links.
- The dashboard displays per-link bytes, fixed-window rates, active flow counts, health, diagnostics, and recent events. Its protected JSON Lines log is stored under `%ProgramData%\Link Alloy\logs\engine.jsonl`.

The initial prototype supports unfragmented IPv4/IPv6 TCP and UDP. ICMP and fragmented IP packets are not handled yet and can fail while the system tunnel is active. The app uses short IPv4/IPv6 HTTPS probes for link health; returned public addresses are not stored in Link Alloy logs.

Starting the tunnel requires administrator access because it creates a Wintun interface and installs system routes. Link Alloy holds a system-wide session lock, writes its validated cleanup record in an administrator-protected state directory, removes only its own routes on stop, and starts a recovery watcher for an unexpected GUI exit.

## Independent implementation and third-party components

The scheduler, packet-flow mapping, per-interface dialing, route lifecycle, health orchestration, and Link Alloy UI are authored in this repository. No application source files from another multi-link product are included. Link Alloy uses the gVisor TCP/IP stack and Wintun's public driver API as separately licensed components; see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).

## Build on Windows

Prerequisites: Go 1.27.3, Node.js 26, pnpm 10, and Wails v3 CLI `v3.0.0-alpha2.119`.

```powershell
go mod tidy
pnpm --dir frontend install --frozen-lockfile
wails3 generate bindings -clean=true -ts -i
pnpm --dir frontend build
wails3 task windows:build
```

The Windows build embeds the verified Wintun runtime and extracts it beside the executable on first tunnel start. The handoff is a single EXE; the NSIS installer and portable bundle include the required third-party license notices.

## Code-only engine smoke test

`cmd/linkalloy-engine-smoke` starts the same controller without the Wails GUI or installer, makes eight new HTTPS connections through the TUN, prints per-link counters, and stops automatically after 15 seconds to 2 minutes. It requires an elevated PowerShell window and temporarily changes the Windows IPv4/IPv6 routes while active:

```powershell
go build -mod=mod -o bin\linkalloy-engine-smoke.exe .\cmd\linkalloy-engine-smoke
.\bin\linkalloy-engine-smoke.exe -duration 30s
```

Run `.\bin\linkalloy-engine-smoke.exe -recover-only` from an elevated PowerShell window to process a saved cleanup journal without starting a tunnel.

Use the smoke command only when a brief system-wide route handoff is acceptable. It verifies the tunnel path and rollback; it does not replace a controlled application download test.

## Release status

Version `0.0.1` is reserved for the first functional alpha and has not been released. The release workflow stays blocked until live tunnel operation, route rollback, failover, and a controlled multi-link application test have been validated. Once releases start, automation keeps the two newest GitHub Release records.

## License

Link Alloy-authored source is proprietary and all rights are reserved. Third-party components retain their own licenses. The Link Alloy license applies only to source code whose rights Link Alloy controls.
