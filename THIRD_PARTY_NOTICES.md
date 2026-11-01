# Third-party notices

Link Alloy uses third-party tools and runtime packages. They remain under their respective upstream licenses; the proprietary Link Alloy notice does not relicense those components.

- Wails v3 and its generated framework build templates: MIT License; text included as [`third-party/licenses/LICENSE-Wails.txt`](third-party/licenses/LICENSE-Wails.txt).
- React and React DOM: MIT License; texts included in `third-party/licenses/`.
- The Wails JavaScript runtime, Vite, TypeScript, and other npm packages retain the licenses declared in their published packages and `frontend/pnpm-lock.yaml`.
- Go modules and npm packages are recorded in `go.sum` and `frontend/pnpm-lock.yaml`.

Before bundling additional drivers or networking libraries, add their exact license terms here and package any required notices with the installer.
