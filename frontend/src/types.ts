import type { AdapterSummary, TunnelLinkStatus, TunnelLogEntry, TunnelStatus } from "../bindings/github.com/sayanpramanik2012/Link-Alloy/models";

export type AppView = "overview" | "connections" | "activity" | "settings";

// These fields are optional until the backend emits the new policy/health DTO.
export type LinkStatus = TunnelLinkStatus & {
  enabled?: boolean;
  health?: string;
  lastProbeError?: string;
  ipv4Available?: boolean;
  ipv6Available?: boolean;
};

export type TransportStats = {
  unsupportedDrops?: number;
  capacityDrops?: number;
  backpressureDrops?: number;
  unknownFlowDrops?: number;
  capturedPackets?: number;
  replyPackets?: number;
  activeTCP?: number;
  activeUDP?: number;
};

export type RuntimeStatus = Omit<TunnelStatus, "links" | "transport" | "logDropped" | "logError"> & {
  links: LinkStatus[] | null;
  transport?: TransportStats;
	logDropped?: number;
	logError?: string;
};

export type TrafficSample = {
  at: number;
  download: number;
  upload: number;
};

export type { AdapterSummary, TunnelLogEntry };
