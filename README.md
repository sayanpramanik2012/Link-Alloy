# Link Alloy

**Independent Windows development preview — routing is not implemented yet.** This repository contains a new Link Alloy codebase. The current build reads adapter metadata only; it does not start a tunnel, change routes, or modify the system proxy.

The product goal is a simple Windows app for people with several independent links, such as Ethernet, Wi-Fi, and USB-tethered mobile data. A future engine is planned to distribute new application connections across selected links and fail over new connections when a link drops. Existing TCP sessions may still need to reconnect if their public route changes; faster downloads are not guaranteed.

## Current build

- Wails v3 desktop shell with a React and TypeScript interface.
- Read-only adapter inventory: interface name, type classification, OS link-up state, interface index, and MTU.
- The tunnel control is disabled. This build has no TUN capture, connection scheduler, proxy listener, throughput counters, or failover.

## Inspiration and source boundary

Link Alloy is informed by publicly described approaches from [HypoMux](https://github.com/Hypostasis-Cat/HypoMux), [HypoMuxPlus](https://github.com/pmh1314520/HypoMuxPlus), and [Speedify](https://speedify.com/how-it-works/). Those projects are not affiliated with Link Alloy. No application source files from them are included in this new repository; the implementation here is written independently. Multi-link routing itself is an established concept, not a Link Alloy invention.

## Build

Prerequisites: Go 1.26.10, Node.js 26, pnpm 10, and Wails v3 CLI `v3.0.0-alpha2.119`.

```powershell
go mod tidy
pnpm --dir frontend install --frozen-lockfile
wails3 generate bindings -clean=true -ts -i
pnpm --dir frontend build
wails3 task windows:build
```

The website source is in `site/index.html`. The public Pages site will publish from the `main` branch.

## Versions and releases

`VERSION` reserves `0.0.1` for the first functional alpha; it is not released yet. The tag workflow verifies the version, release notes, and `docs/RELEASE_READINESS.md` before packaging. Once releases begin, the workflow keeps only the two newest GitHub Release records and leaves older source tags intact. The next patch after the first functional release will be `0.0.2`.

A versioned alpha must wait until the original routing engine, traffic cleanup, and failure recovery are implemented and validated.

## License

Original Link Alloy source is proprietary and all rights are reserved. Third-party dependencies retain their own licenses; see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). No license can guarantee that code will never be copied; this notice states the permitted use of Link Alloy's original source.
