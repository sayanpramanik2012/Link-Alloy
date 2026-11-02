# Wintun runtime binary provenance

The `amd64/wintun.dll` and `arm64/wintun.dll` files are unmodified binaries extracted from the official Wintun 0.14.1 archive downloaded from [wintun.net](https://www.wintun.net/builds/wintun-0.14.1.zip).

The official archive SHA-256 is `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`. Both DLLs were Authenticode-verified after extraction. The exact Wintun Prebuilt Binaries License is included in `third-party/licenses/LICENSE-Wintun-Prebuilt.txt` and is packaged with the installer.

Link Alloy uses the DLL only through the Wintun API. The project is not affiliated with or endorsed by WireGuard LLC or Wintun.
