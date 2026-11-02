# Third-party notices

Link Alloy uses third-party tools and runtime packages. They remain under their respective upstream licenses; the proprietary Link Alloy notice does not relicense those components.

- Wails v3 and its generated framework build templates: MIT License; text included as [`third-party/licenses/LICENSE-Wails.txt`](third-party/licenses/LICENSE-Wails.txt).
- React and React DOM: MIT License; texts included in `third-party/licenses/`.
- gVisor TCP/IP stack (`v0.0.0-20261010064756-cd110d9dec04`): Apache License 2.0; upstream license text is included as [`third-party/licenses/LICENSE-gVisor.txt`](third-party/licenses/LICENSE-gVisor.txt). The Link Alloy scheduler, NAT mapping, adapter selection, route lifecycle, and application integration are authored in this repository.
- WireGuard's Go Wintun API wrapper (`v0.0.0-20230126152724-0fa3db229ce2`): MIT License; text included as [`third-party/licenses/LICENSE-Wintun-Go-Wrapper.txt`](third-party/licenses/LICENSE-Wintun-Go-Wrapper.txt).
- Wintun `wintun.dll` v0.14.1 for amd64 and arm64: official signed binaries, copied unchanged from the release archive and used through the public Wintun API. The separate Wintun Prebuilt Binaries License is included as [`third-party/licenses/LICENSE-Wintun-Prebuilt.txt`](third-party/licenses/LICENSE-Wintun-Prebuilt.txt). Link Alloy is not affiliated with or endorsed by WireGuard LLC or Wintun.
- The Wails JavaScript runtime, Vite, TypeScript, and other npm packages retain the licenses declared in their published packages and `frontend/pnpm-lock.yaml`.
- Go modules and npm packages are recorded in `go.sum` and `frontend/pnpm-lock.yaml`.
