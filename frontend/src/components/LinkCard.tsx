import { formatBytes, formatRate } from "../format";
import type { AdapterSummary, LinkStatus } from "../types";
import { Icon } from "./Icon";
import { useRateUnit } from "../rate-unit";

type Props = {
  adapter: AdapterSummary;
  link?: LinkStatus;
  tunnelActive: boolean;
  downloadShare?: number;
};

function healthText(adapter: AdapterSummary, link: LinkStatus | undefined, tunnelActive: boolean) {
  if (link?.enabled === false) return "Excluded";
  if (!tunnelActive) return adapter.isUp ? "Connected" : "Offline";
  if (link?.health === "healthy") return "Healthy";
  if (link?.health === "probing") return "Checking";
  if (link?.health === "degraded") return "Degraded";
  if (link?.health) return link.health.replace(/_/g, " ");
  if (tunnelActive) return link?.available ? "Online" : "Check failed";
  return adapter.isUp ? "Connected" : "Offline";
}

export function LinkCard({ adapter, link, tunnelActive, downloadShare = 0 }: Props) {
  const { unit } = useRateUnit();
  const healthy = tunnelActive ? Boolean(link?.available && link.enabled !== false) : adapter.isUp;
  const label = healthText(adapter, link, tunnelActive);

  return (
    <article className={`link-card ${healthy ? "link-card-healthy" : "link-card-down"}`}>
      <div className="link-card-head">
        <span className={`link-glyph ${adapter.kind}`}><Icon name={adapter.kind === "wifi" ? "wifi" : adapter.kind === "cellular" ? "cellular" : "ethernet"} /></span>
        <div className="link-title">
          <h3 title={adapter.name}>{adapter.name}</h3>
          <span>{adapter.kindLabel}{link && ` · weight ${link.weight}`}</span>
        </div>
        <span className={`health-pill ${tunnelActive && link?.health === "degraded" ? "health-pill-warning" : healthy ? "health-pill-up" : "health-pill-down"}`}>
          <i aria-hidden="true" />{label}
        </span>
      </div>

      {tunnelActive && link ? (
        <>
          {link.lastProbeError && !link.available && <p className="link-probe-error" title={link.lastProbeError}>{link.lastProbeError}</p>}
          <div className="link-metrics">
            <div className="link-metric">
              <span className="metric-label metric-download"><i aria-hidden="true" />Download</span>
              <strong>{formatRate(link.recvBytesPerSec, unit)}</strong>
              <small>{formatBytes(link.bytesReceived)} received</small>
            </div>
            <div className="link-metric">
              <span className="metric-label metric-upload"><i aria-hidden="true" />Upload</span>
              <strong>{formatRate(link.sendBytesPerSec, unit)}</strong>
              <small>{formatBytes(link.bytesSent)} sent</small>
            </div>
          </div>
          <div className="link-share"><span>Current download share</span><strong>{Math.round(downloadShare)}%</strong><div className="link-share-track" aria-hidden="true"><span style={{ width: `${Math.max(0, Math.min(100, downloadShare))}%` }} /></div></div>
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
