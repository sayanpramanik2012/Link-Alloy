import { formatBytes, formatRate } from "../format";
import type { AdapterSummary, LinkStatus } from "../types";

type Props = {
  adapter: AdapterSummary;
  link?: LinkStatus;
  tunnelActive: boolean;
};

function adapterGlyph(kind: string) {
  if (kind === "wifi") return "≈";
  if (kind === "ethernet") return "⇄";
  if (kind === "cellular") return "▥";
  return "⌁";
}

function healthText(adapter: AdapterSummary, link: LinkStatus | undefined, tunnelActive: boolean) {
  if (link?.enabled === false) return "Excluded";
  if (link?.health) return link.health.replace(/_/g, " ");
  if (tunnelActive) return link?.available ? "Online" : "Check failed";
  return adapter.isUp ? "Connected" : "Offline";
}

export function LinkCard({ adapter, link, tunnelActive }: Props) {
  const healthy = tunnelActive ? Boolean(link?.available && link.enabled !== false) : adapter.isUp;
  const label = healthText(adapter, link, tunnelActive);

  return (
    <article className={`link-card ${healthy ? "link-card-healthy" : "link-card-down"}`}>
      <div className="link-card-head">
        <span className={`link-glyph ${adapter.kind}`} aria-hidden="true">{adapterGlyph(adapter.kind)}</span>
        <div className="link-title">
          <h3 title={adapter.name}>{adapter.name}</h3>
          <span>{adapter.kindLabel}{link && ` · weight ${link.weight}`}</span>
        </div>
        <span className={`health-pill ${healthy ? "health-pill-up" : "health-pill-down"}`}>
          <i aria-hidden="true" />{label}
        </span>
      </div>

      {tunnelActive && link ? (
        <>
          {link.lastProbeError && !link.available && <p className="link-probe-error" title={link.lastProbeError}>{link.lastProbeError}</p>}
          <div className="link-metrics">
            <div className="link-metric">
              <span className="metric-label metric-download"><i aria-hidden="true" />Download</span>
              <strong>{formatRate(link.recvBytesPerSec)}</strong>
              <small>{formatBytes(link.bytesReceived)} received</small>
            </div>
            <div className="link-metric">
              <span className="metric-label metric-upload"><i aria-hidden="true" />Upload</span>
              <strong>{formatRate(link.sendBytesPerSec)}</strong>
              <small>{formatBytes(link.bytesSent)} sent</small>
            </div>
          </div>
          <div className="link-card-foot">
            <span>{link.activeConnections} active {link.activeConnections === 1 ? "connection" : "connections"}</span>
            <span>{adapter.index} · MTU {adapter.mtu || "—"}</span>
          </div>
        </>
      ) : (
        <div className="link-card-foot link-card-foot-idle">
          <span>{adapter.isUp ? "Available when tunnel starts" : "Connect this adapter to use it"}</span>
          <span>{adapter.index} · MTU {adapter.mtu || "—"}</span>
        </div>
      )}
    </article>
  );
}
