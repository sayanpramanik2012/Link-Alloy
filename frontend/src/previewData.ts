import type { AdapterSummary, LinkStatus, RuntimeStatus, TunnelLogEntry } from "./types";

export const previewAdapters: AdapterSummary[] = [
  { index: 12, name: "Ethernet", kind: "ethernet", kindLabel: "Ethernet", isUp: true, mtu: 1500 },
  { index: 17, name: "Wi-Fi", kind: "wifi", kindLabel: "Wi-Fi", isUp: true, mtu: 1500 },
  { index: 23, name: "USB tethering", kind: "cellular", kindLabel: "Mobile link", isUp: true, mtu: 1420 },
];

const previewLinks: LinkStatus[] = [
  { index: 12, name: "Ethernet", weight: 4, available: true, enabled: true, health: "healthy", ipv4Available: true, ipv6Available: true, activeConnections: 9, bytesReceived: 4_812_032_000, bytesSent: 96_215_040, recvBytesPerSec: 34_603_008, sendBytesPerSec: 524_288 },
  { index: 17, name: "Wi-Fi", weight: 2, available: true, enabled: true, health: "healthy", ipv4Available: true, ipv6Available: true, activeConnections: 4, bytesReceived: 2_171_273_216, bytesSent: 45_613_056, recvBytesPerSec: 12_582_912, sendBytesPerSec: 262_144 },
  { index: 23, name: "USB tethering", weight: 1, available: true, enabled: true, health: "healthy", ipv4Available: true, ipv6Available: false, activeConnections: 2, bytesReceived: 939_524_096, bytesSent: 18_874_368, recvBytesPerSec: 4_194_304, sendBytesPerSec: 131_072 },
];

export function buildPreviewSnapshot(): { status: RuntimeStatus; logs: TunnelLogEntry[]; logPath: string } {
  const now = Date.now();
	const pulse = 0.82 + 0.18 * Math.sin(now / 4300);
	const links = previewLinks.map((link, index) => ({
		...link,
		recvBytesPerSec: Math.round(link.recvBytesPerSec * (pulse + index * 0.025)),
		sendBytesPerSec: Math.round(link.sendBytesPerSec * (0.9 + 0.1 * Math.cos(now / 3500 + index))),
	}));
  const logs: TunnelLogEntry[] = [
    { at: new Date(now - 45_000).toISOString(), level: "info", category: "link-health", message: "Ethernet: healthy", linkIndex: 12 },
    { at: new Date(now - 31_000).toISOString(), level: "info", category: "link-health", message: "Wi-Fi: healthy", linkIndex: 17 },
    { at: new Date(now - 21_000).toISOString(), level: "info", category: "link-health", message: "USB tethering: IPv4 available", linkIndex: 23 },
    { at: new Date(now - 8_000).toISOString(), level: "info", category: "flow-start", message: "TCP flow assigned to Ethernet", linkIndex: 12 },
    { at: new Date(now - 3_000).toISOString(), level: "info", category: "flow-end", message: "UDP flow ended", linkIndex: 17, bytesReceived: 1_204_224, bytesSent: 18_432 },
  ];
  return {
    status: {
      state: "running",
      since: new Date(now - 7 * 60_000).toISOString(),
      links,
      transport: { capturedPackets: 184_220, replyPackets: 179_874, activeTCP: 11, activeUDP: 4, unsupportedDrops: 0, capacityDrops: 0, backpressureDrops: 0, unknownFlowDrops: 0 },
      logDropped: 0,
      flows: links.map((link, index) => ({ id: index + 1, processId: 1234, processName: "Sample downloader", protocol: "tcp", localPort: 50000 + index, remotePort: 443, linkIndex: link.index, upstreamLocal: `192.0.2.${index + 1}:51000`, state: "active", dialMilliseconds: 24 + index * 10, bytesSent: link.bytesSent, bytesReceived: link.bytesReceived, recvBytesPerSec: link.recvBytesPerSec, sendBytesPerSec: link.sendBytesPerSec })),
    },
    logs,
    logPath: "%ProgramData%\\Link Alloy\\logs\\engine.jsonl",
  };
}
