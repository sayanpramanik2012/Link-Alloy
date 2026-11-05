import type { AdapterSummary, LinkStatus } from "../types";
import type { TransportStats } from "../types";
import { NoticesPanel } from "./NoticesPanel";
import { Icon } from "./Icon";
import { useState } from "react";

export type LinkPolicy = { enabled: boolean; weight: number; dirty: boolean };

type Props = {
  adapters: AdapterSummary[];
  links: LinkStatus[];
  policies: Record<number, LinkPolicy>;
  tunnelState: string;
  logPath: string;
  transport?: TransportStats;
  preview?: boolean;
  configureAvailable: boolean;
  savingIndex: number | null;
  onPolicyChange: (index: number, policy: LinkPolicy) => void;
  onApplyPolicy: (index: number) => void;
  onCopyLogPath: () => void;
};

export function SettingsView({ adapters, links, policies, tunnelState, logPath, transport, preview = false, configureAvailable, savingIndex, onPolicyChange, onApplyPolicy, onCopyLogPath }: Props) {
  const [section, setSection] = useState("links");
  const linksByIndex = new Map(links.map((link) => [link.index, link]));
  const checkedLinks = links.filter((link) => link.health && link.health !== "idle" && link.health !== "probing");
  const ipv4State = preview ? "Preview sample" : tunnelState !== "running" ? "Checked when tunnel starts" : checkedLinks.length === 0 ? "Checking…" : checkedLinks.some((link) => link.ipv4Available) ? "Available" : "Unavailable";
  const ipv6State = preview ? "Preview sample" : tunnelState !== "running" ? "Checked when tunnel starts" : checkedLinks.length === 0 ? "Checking…" : checkedLinks.some((link) => link.ipv6Available) ? "Available" : "Unavailable";

  return (
    <div className="page-stack">
      <div className="section-switcher" role="group" aria-label="Settings section">
        {[["links", "Link policy"], ["monitoring", "Health & logs"], ["diagnostics", "Diagnostics"], ["about", "About & notices"]].map(([id, label]) => <button key={id} type="button" aria-pressed={section === id} onClick={() => setSection(id)}>{label}</button>)}
      </div>
      <div className="settings-content" tabIndex={0} role="region" aria-label="Settings details">
      {section === "links" && <>
      <section className="panel settings-panel" aria-labelledby="link-policy-title">
        <div className="panel-header">
          <div><p className="eyebrow">FLOW ALLOCATION</p><h2 id="link-policy-title">Network link policy</h2></div>
          {!configureAvailable && <span className="sample-state">Controls unavailable</span>}
        </div>
        {adapters.length === 0 ? (
          <div className="empty-panel"><strong>No adapters found</strong><p>Connect a network link, then refresh the overview.</p></div>
        ) : (
          <div className="policy-list">
            {adapters.map((adapter) => {
              const link = linksByIndex.get(adapter.index);
              const current = policies[adapter.index] ?? { enabled: link?.enabled ?? adapter.isUp, weight: link?.weight ?? 1, dirty: false };
              return (
                <article className={`policy-row ${current.enabled ? "" : "policy-row-disabled"}`} key={adapter.index}>
                  <div className="policy-link">
                    <span className={`link-glyph ${adapter.kind}`}><Icon name={adapter.kind === "wifi" ? "wifi" : adapter.kind === "cellular" ? "cellular" : "ethernet"} /></span>
                    <div><strong>{adapter.name}</strong><small>{adapter.kindLabel} · {link?.health ?? (link?.available ? "Online" : adapter.isUp ? "Connected" : "Offline")}</small></div>
                  </div>
                  <label className="policy-toggle">
                    <input
                      type="checkbox"
                      checked={current.enabled}
                      disabled={!configureAvailable || savingIndex === adapter.index}
                      onChange={(event) => onPolicyChange(adapter.index, { ...current, enabled: event.target.checked, dirty: true })}
                    />
                    <span>Use for new flows</span>
                  </label>
                  <label className="weight-control">
                    <span className="weight-label">Relative weight <strong>{current.weight}</strong></span>
                    <input
                      type="range"
                      min={1}
                      max={4}
                      step={1}
                      value={Math.max(1, Math.min(4, Math.round(current.weight)))}
                      disabled={!configureAvailable || savingIndex === adapter.index}
                      aria-label={`Relative flow weight for ${adapter.name}`}
                      onChange={(event) => onPolicyChange(adapter.index, { ...current, weight: Number(event.target.value), dirty: true })}
                    />
                    <span className="weight-hint">Higher values make this link more likely for new flows; they do not predict throughput.</span>
                  </label>
                  <button className="secondary-button policy-save" type="button" disabled={!configureAvailable || !current.dirty || savingIndex === adapter.index} onClick={() => onApplyPolicy(adapter.index)}>
                    {savingIndex === adapter.index ? "Saving…" : "Apply"}
                  </button>
                </article>
              );
            })}
          </div>
        )}
        {!configureAvailable && <p className="settings-note">{preview ? "Settings are disabled in visual preview." : "Per-link settings are not available in this backend build yet."}</p>}
        {tunnelState === "running" && <p className="settings-note settings-note-active">Policy changes affect new connections. Existing connections remain on their assigned link.</p>}
      </section>
      </>}

      {section === "monitoring" && <section className="settings-grid">
        <article className="panel settings-card">
          <p className="eyebrow">HEALTH CHECKS</p>
          <h2>Internet path monitoring</h2>
          <p>Link Alloy checks each adapter with short IPv4 and IPv6 HTTPS probes at startup and while routing. It uses the result only for link health and does not store a returned public IP.</p>
          <div className="family-status-grid"><span>IPv4 <strong>{ipv4State}</strong></span><span>IPv6 <strong>{ipv6State}</strong></span></div>
        </article>
        <article className="panel settings-card">
          <p className="eyebrow">LOCAL LOGS</p>
          <h2>Engine event file</h2>
          <p className="log-path-value">{logPath || "%ProgramData%\\Link Alloy\\logs\\engine.jsonl"}</p>
          <button className="secondary-button" type="button" onClick={onCopyLogPath}>Copy log location</button>
        </article>
      </section>}

      {section === "diagnostics" && <section className="panel diagnostics-panel" aria-labelledby="transport-diagnostics-title">
        <div className="panel-header">
          <div><p className="eyebrow">ENGINE DIAGNOSTICS</p><h2 id="transport-diagnostics-title">Packet counters</h2></div>
          <span className="panel-caption">No packet contents are recorded</span>
        </div>
        {transport ? (
          <div className="diagnostic-grid">
            <div><span>Captured</span><strong>{(transport.capturedPackets ?? 0).toLocaleString()}</strong></div>
            <div><span>Replies</span><strong>{(transport.replyPackets ?? 0).toLocaleString()}</strong></div>
            <div><span>Unsupported</span><strong className={(transport.unsupportedDrops ?? 0) > 0 ? "diagnostic-warning" : ""}>{(transport.unsupportedDrops ?? 0).toLocaleString()}</strong></div>
            <div><span>Capacity drops</span><strong className={(transport.capacityDrops ?? 0) > 0 ? "diagnostic-warning" : ""}>{(transport.capacityDrops ?? 0).toLocaleString()}</strong></div>
            <div><span>Queue backpressure</span><strong className={(transport.backpressureDrops ?? 0) > 0 ? "diagnostic-warning" : ""}>{(transport.backpressureDrops ?? 0).toLocaleString()}</strong></div>
            <div><span>Unknown flows</span><strong className={(transport.unknownFlowDrops ?? 0) > 0 ? "diagnostic-warning" : ""}>{(transport.unknownFlowDrops ?? 0).toLocaleString()}</strong></div>
          </div>
        ) : (
          <p className="settings-note">Start the tunnel to see transport counters from the active session.</p>
        )}
      </section>}

      {section === "about" && <>
      <section className="panel support-panel">
        <p className="eyebrow">PROTOCOL COVERAGE</p>
        <h2>Current prototype boundaries</h2>
        <div className="support-list">
          <div><span className="support-check">✓</span><span>IPv4/IPv6 TCP and UDP on healthy links</span></div>
          <div><span className="support-pending">·</span><span>ICMP and fragmented packets are not handled yet</span></div>
          <div><span className="support-pending">·</span><span>Existing connections may reconnect when their public route changes</span></div>
        </div>
      </section>
      {!preview && <NoticesPanel />}
      </>}
      </div>
    </div>
  );
}
