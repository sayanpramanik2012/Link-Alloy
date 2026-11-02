import { formatBytes, formatRate } from "../format";
import type { AdapterSummary, LinkStatus, TransportStats } from "../types";

type Props = {
  adapters: AdapterSummary[];
  links: LinkStatus[];
  active: boolean;
  transport?: TransportStats;
  preview?: boolean;
};

export function ConnectionsView({ adapters, links, active, transport, preview = false }: Props) {
  const activeConnections = links.reduce((total, link) => total + link.activeConnections, 0);

  return (
    <div className="page-stack">
      <section className="page-intro">
        <p className="eyebrow">LIVE FLOW DISTRIBUTION</p>
        <h1>Connections</h1>
        <p>{preview ? "Sample path assignments for layout review; no live connections are being read." : "Link Alloy assigns each supported new flow to one network path. Existing flows stay on their selected link until they close or need to reconnect."}</p>
      </section>

      <section className="connection-summary-grid" aria-label="Connection summary">
        <div className="summary-tile"><span>Active connections</span><strong>{active ? activeConnections : 0}</strong><small>{preview ? "Preview sample" : active && transport ? `${transport.activeTCP ?? 0} TCP · ${transport.activeUDP ?? 0} UDP` : active ? "Current tunnel session" : "Tunnel stopped"}</small></div>
        <div className="summary-tile"><span>Healthy links</span><strong>{active ? links.filter((link) => link.available && link.enabled !== false).length : 0}</strong><small>Eligible for new flows</small></div>
        <div className="summary-tile"><span>Captured packets</span><strong>{active && transport ? (transport.capturedPackets ?? 0).toLocaleString() : "—"}</strong><small>{active ? "Tunnel session" : "Tunnel stopped"}</small></div>
      </section>

      <section className="panel connection-table-panel" aria-labelledby="connection-table-title">
        <div className="panel-header">
          <div><p className="eyebrow">PATH ASSIGNMENTS</p><h2 id="connection-table-title">Traffic by link</h2></div>
          <span className="table-caption">{preview ? "Sample data · not live" : "Destination details are intentionally not collected."}</span>
        </div>
        {active && links.length > 0 ? (
          <div className="table-scroll">
            <table className="data-table">
              <thead><tr><th scope="col">Network link</th><th scope="col">Health</th><th scope="col">Active</th><th scope="col">Download</th><th scope="col">Upload</th><th scope="col">Received / sent</th></tr></thead>
              <tbody>
                {links.map((link) => {
                  const adapter = adapters.find((item) => item.index === link.index);
                  const healthy = link.available && link.enabled !== false;
                  return (
                    <tr key={link.index}>
                      <th scope="row"><span className="table-link-name">{adapter?.name ?? link.name}</span><small>{adapter?.kindLabel ?? "Network adapter"} · weight {link.weight}</small></th>
                      <td><span className={`health-pill ${healthy ? "health-pill-up" : "health-pill-down"}`}><i />{link.health ?? (healthy ? "Online" : "Offline")}</span></td>
                      <td className="numeric-cell">{link.activeConnections}</td>
                      <td className="numeric-cell">{formatRate(link.recvBytesPerSec)}</td>
                      <td className="numeric-cell">{formatRate(link.sendBytesPerSec)}</td>
                      <td className="numeric-cell">{formatBytes(link.bytesReceived)} / {formatBytes(link.bytesSent)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="empty-panel" role="status">
            <span className="empty-panel-icon" aria-hidden="true">↔</span>
            <strong>{active ? "No eligible links yet" : "Tunnel is stopped"}</strong>
            <p>{active ? "Connect an internet link and wait for its health check." : "Start the tunnel to see live connections and per-link traffic."}</p>
          </div>
        )}
      </section>
    </div>
  );
}
