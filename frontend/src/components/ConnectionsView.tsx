import { formatBytes, formatRate } from "../format";
import type { AdapterSummary, FlowDiagnostic, LinkStatus, TransportStats } from "../types";
import { useState } from "react";
import { useRateUnit } from "../rate-unit";
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
  const [linkFilter, setLinkFilter] = useState("all");
  const [stateFilter, setStateFilter] = useState("active");
  const { unit } = useRateUnit();
  const normalizedQuery = query.trim().toLowerCase();
  const visibleFlows = (flows ?? []).filter(flow => {
    const stateMatches = stateFilter === "all" || (stateFilter === "active" ? flow.state === "active" || flow.state === "dialing" : flow.state === stateFilter);
    return stateMatches && (linkFilter === "all" || String(flow.linkIndex) === linkFilter) && `${flow.processName} ${flow.serviceName ?? ""} ${flow.processId} ${flow.id}`.toLowerCase().includes(normalizedQuery);
  });
  const hasFilter = Boolean(query || linkFilter !== "all" || stateFilter !== "active");
  function clearFilters() { setQuery(""); setLinkFilter("all"); setStateFilter("active"); }

  return (
    <div className="page-stack" role="region" aria-label="Connection details" tabIndex={0}>
      <section className="connection-summary-grid" aria-label="Connection summary">
        <div className="summary-tile"><span>Active connections</span><strong>{active ? activeConnections : 0}</strong><small>{preview ? "Preview sample" : active && transport ? `${transport.activeTCP ?? 0} TCP · ${transport.activeUDP ?? 0} UDP` : active ? "Current tunnel session" : "Tunnel stopped"}</small></div>
        <div className="summary-tile"><span>Eligible links</span><strong>{active ? links.filter((link) => link.available && link.enabled !== false).length : 0}</strong><small>Available for new flows</small></div>
        <div className="summary-tile"><span>Captured packets</span><strong>{active && transport ? (transport.capturedPackets ?? 0).toLocaleString() : "—"}</strong><small>{active ? "Tunnel session" : "Tunnel stopped"}</small></div>
      </section>

      <section className="panel connection-table-panel" aria-labelledby="connection-table-title">
        <div className="panel-header">
          <div><p className="eyebrow">PATH ASSIGNMENTS</p><h2 id="connection-table-title">Traffic by link</h2></div>
          <span className="table-caption">{preview ? "Sample data · not live" : "Current tunnel session"}</span>
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
                      <td><span className={`health-pill ${link.health === "degraded" ? "health-pill-warning" : healthy ? "health-pill-up" : "health-pill-down"}`}><i />{link.health ?? (healthy ? "Online" : "Offline")}</span></td>
                      <td className="numeric-cell">{link.activeConnections}</td>
                      <td className="numeric-cell">{formatRate(link.recvBytesPerSec, unit)}</td>
                      <td className="numeric-cell">{formatRate(link.sendBytesPerSec, unit)}</td>
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
          <div><p className="eyebrow">APPLICATION TRAFFIC</p><h2 id="flow-diagnostics-title">Applications & connections</h2></div>
          <span className="table-caption">{visibleFlows.length} shown · {(flows ?? []).length} available</span>
        </div>
          <div className="connection-filters">
            <label className="sr-only" htmlFor="flow-search">Filter by application, service, process ID, or connection ID</label>
            <input id="flow-search" type="search" placeholder="Application, service or ID" value={query} onChange={event => setQuery(event.target.value)} />
            <select aria-label="Filter by network link" value={linkFilter} onChange={event => setLinkFilter(event.target.value)}><option value="all">All network links</option>{links.map(link => <option key={link.index} value={String(link.index)}>{link.name}</option>)}</select>
            <select aria-label="Filter by connection state" value={stateFilter} onChange={event => setStateFilter(event.target.value)}><option value="active">Active connections</option><option value="all">All connections</option><option value="completed">Completed</option><option value="dial failed">Failed to connect</option></select>
            {hasFilter && <button type="button" className="secondary-button" onClick={clearFilters}>Clear filters</button>}
          </div>
        {active && visibleFlows.length > 0 ? (
          <div className="table-scroll">
            <table className="data-table">
              <thead><tr><th scope="col">Application / connection</th><th scope="col">Link / bound address</th><th scope="col">State</th><th scope="col">Download</th><th scope="col">Upload</th><th scope="col">Received / sent</th></tr></thead>
              <tbody>{visibleFlows.map(flow => (
                <tr key={flow.id}>
                  <th scope="row"><span className="table-link-name">{flow.processName || "Unknown"}</span><small>{flow.serviceName || "Service unclassified"}</small><small>{flow.processId ? `PID ${flow.processId} · ` : ""}#{flow.id} · {flow.protocol.toUpperCase()} · :{flow.localPort} → :{flow.remotePort}</small></th>
                  <td><span>{links.find(link => link.index === flow.linkIndex)?.name ?? "Unassigned"}</span><small>{flow.upstreamLocal || "Awaiting connection"}</small></td>
                  <td><span className="flow-state" data-state={flow.state}>{flow.state}</span><small>{flow.dialMilliseconds} ms connect</small></td>
                  <td className="numeric-cell">{formatRate(flow.recvBytesPerSec, unit)}</td>
                  <td className="numeric-cell">{formatRate(flow.sendBytesPerSec, unit)}</td>
                  <td className="numeric-cell">{formatBytes(flow.bytesReceived)} / {formatBytes(flow.bytesSent)}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        ) : (
          <div className="empty-panel" role="status">
            <strong>{!active ? "Tunnel is stopped" : hasFilter ? "No matching connections" : "No active connections"}</strong>
            <p>{hasFilter ? "Use Clear filters above, or choose another application, network link, or connection state." : "Start the tunnel, then start or resume your download to see its connections."}</p>
          </div>
        )}
        <p className="section-footnote">Up to 256 active or recent connections. Process names and bound addresses stay on this device in memory. Unknown means ownership could not be resolved.</p>
      </section>
    </div>
  );
}
