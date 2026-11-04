import { formatBytes, formatRate } from "../format";
import type { AdapterSummary, FlowDiagnostic, LinkStatus, TransportStats } from "../types";
import { useState } from "react";
import "./ConnectionsView.css";

type Props = {
  adapters: AdapterSummary[];
  links: LinkStatus[];
  active: boolean;
  transport?: TransportStats;
  preview?: boolean;
  flows?: FlowDiagnostic[] | null;
};

export function ConnectionsView({ adapters, links, active, transport, preview = false, flows }: Props) {
  const activeConnections = links.reduce((total, link) => total + link.activeConnections, 0);
  const [query, setQuery] = useState("");
  const visibleFlows = (flows ?? []).filter(flow => `${flow.processName} ${flow.processId} ${flow.id}`.toLowerCase().includes(query.trim().toLowerCase()));

  return (
    <div className="page-stack">
      <section className="page-intro">
        <p className="eyebrow">LIVE FLOW DISTRIBUTION</p>
        <h1>Connections</h1>
        <p>{preview ? "Sample path assignments for layout review; no live connections are being read." : "New connections are distributed within each application and service. Use the table below to check which links carry the actual download. Existing connections keep their assigned path."}</p>
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

      <section className="panel connection-table-panel" aria-labelledby="flow-diagnostics-title">
        <div className="panel-header">
          <div><p className="eyebrow">APPLICATION TRAFFIC</p><h2 id="flow-diagnostics-title">Connection diagnostics</h2></div>
          <div className="activity-filters">
            <label className="sr-only" htmlFor="flow-search">Filter by application, process ID, or connection ID</label>
            <input id="flow-search" type="search" placeholder="Filter application or ID" value={query} onChange={event => setQuery(event.target.value)} />
          </div>
        </div>
        <p className="section-footnote">Up to 256 active or recent connections. Application names and local addresses stay in memory. Unresolved ownership is shown as Unknown.</p>
        {active && visibleFlows.length > 0 ? (
          <div className="table-scroll">
            <table className="data-table">
              <thead><tr><th scope="col">Application / connection</th><th scope="col">Link / bound address</th><th scope="col">State</th><th scope="col">Download</th><th scope="col">Upload</th><th scope="col">Received / sent</th></tr></thead>
              <tbody>{visibleFlows.map(flow => (
                <tr key={flow.id}>
                  <th scope="row"><span className="table-link-name">{flow.processName || "Unknown"}</span><small>{flow.processId ? `PID ${flow.processId} · ` : ""}#{flow.id} · {flow.protocol.toUpperCase()} · :{flow.localPort} → :{flow.remotePort}</small></th>
                  <td><span>{links.find(link => link.index === flow.linkIndex)?.name ?? "Unassigned"}</span><small>{flow.upstreamLocal || "Awaiting connection"}</small></td>
                  <td><span>{flow.state}</span><small>{flow.dialMilliseconds} ms connect</small></td>
                  <td className="numeric-cell">{formatRate(flow.recvBytesPerSec)}</td>
                  <td className="numeric-cell">{formatRate(flow.sendBytesPerSec)}</td>
                  <td className="numeric-cell">{formatBytes(flow.bytesReceived)} / {formatBytes(flow.bytesSent)}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        ) : <div className="empty-panel" role="status"><strong>{!active ? "Start the tunnel to inspect connections" : query ? "No matching connections" : "Waiting for new connections"}</strong><p>{preview ? "Live diagnostics are unavailable in visual preview." : "Start or resume a download after the tunnel is active."}</p></div>}
      </section>
    </div>
  );
}
