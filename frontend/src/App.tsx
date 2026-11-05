import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { NetworkService } from "../bindings/github.com/sayanpramanik2012/Link-Alloy";
import type { AppView, LinkStatus, RuntimeStatus, TrafficSample, AdapterSummary, TunnelLogEntry } from "./types";
import type { LinkPolicy } from "./components/SettingsView";
import { Sidebar } from "./components/Sidebar";
import { ThroughputChart } from "./components/ThroughputChart";
import { LinkCard } from "./components/LinkCard";
import { ConnectionsView } from "./components/ConnectionsView";
import { ActivityFeed } from "./components/ActivityFeed";
import { SettingsView } from "./components/SettingsView";
import { formatBytes, formatDurationSince } from "./format";
import { buildPreviewSnapshot, previewAdapters } from "./previewData";
import { appendRollingSample, errorReducer, isStale, pollInterval, withTimeout } from "./state-utils";
import { RateUnitControl } from "./rate-unit";
import { Icon } from "./components/Icon";

const views: Record<AppView, { title: string; eyebrow: string; description: string }> = {
  overview: { title: "Overview", eyebrow: "NETWORK CONTROL", description: "See which links are carrying your traffic and how the tunnel is behaving." },
  connections: { title: "Connections", eyebrow: "FLOW DISTRIBUTION", description: "Review live connection counts and traffic totals by link." },
  activity: { title: "Activity", eyebrow: "LOCAL AUDIT TRAIL", description: "Track tunnel, link-health, and connection events recorded on this device." },
  settings: { title: "Settings", eyebrow: "LINK ALLOY CONFIGURATION", description: "Set eligible links and relative connection weights." },
};

const emptyStatus: RuntimeStatus = { state: "stopped", links: [], transport: {}, logDropped: 0 };

function sumBy(links: LinkStatus[], field: "recvBytesPerSec" | "sendBytesPerSec" | "bytesReceived" | "bytesSent") {
  return links.reduce((total, link) => total + Number(link[field] || 0), 0);
}

function App() {
  const previewMode = import.meta.env.DEV && new URLSearchParams(window.location.search).get("preview") === "1";
  const [view, setView] = useState<AppView>("overview");
  const [adapters, setAdapters] = useState<AdapterSummary[]>([]);
  const [status, setStatus] = useState<RuntimeStatus>(emptyStatus);
  const [logs, setLogs] = useState<TunnelLogEntry[]>([]);
  const [history, setHistory] = useState<TrafficSample[]>([]);
  const [policies, setPolicies] = useState<Record<number, LinkPolicy>>({});
  const [logPath, setLogPath] = useState("");
  const [initialLoading, setInitialLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [actionPending, setActionPending] = useState(false);
  const [savingIndex, setSavingIndex] = useState<number | null>(null);
  const [errors, dispatchErrors] = useReducer(errorReducer, { actionError: "", refreshError: "" });
  const [notice, setNotice] = useState("");
  const [lastUpdated, setLastUpdated] = useState<number | null>(null);
  const inFlight = useRef(false);
  const statusRef = useRef<RuntimeStatus>(emptyStatus);

  const tunnelActive = status.state === "running";
  const tunnelBusy = actionPending || status.state === "starting" || status.state === "stopping";
  const links = status.links ?? [];
  const totalDownloadRate = sumBy(links, "recvBytesPerSec");
  const totalUploadRate = sumBy(links, "sendBytesPerSec");
  const totalReceived = sumBy(links, "bytesReceived");
  const totalSent = sumBy(links, "bytesSent");
  const activeConnections = links.reduce((total, link) => total + link.activeConnections, 0);
  const usableLinks = links.filter((link) => link.available && link.enabled !== false).length;
  const viewInfo = views[view];
  const serviceWithPolicy = NetworkService as unknown as {
    ConfigureLink?: (index: number, enabled: boolean, weight: number) => PromiseLike<void>;
  };
  const configureAvailable = !previewMode && typeof serviceWithPolicy.ConfigureLink === "function";

  const refreshSnapshot = useCallback(async (manual = false) => {
    if (inFlight.current) {
      if (manual) setRefreshing(false);
      return;
    }
    inFlight.current = true;
    if (manual) setRefreshing(true);
    if (previewMode) {
      const preview = buildPreviewSnapshot();
      const previous = statusRef.current;
      if (previous.state !== "running") setHistory([]);
      statusRef.current = preview.status;
      setAdapters(previewAdapters);
      setStatus(preview.status);
      setLogs(preview.logs);
      setLogPath(preview.logPath);
      const sample: TrafficSample = {
        at: Date.now(),
        download: sumBy(preview.status.links ?? [], "recvBytesPerSec"),
        upload: sumBy(preview.status.links ?? [], "sendBytesPerSec"),
      };
      setHistory((current) => appendRollingSample(current, sample));
      setLastUpdated(Date.now());
      dispatchErrors({ type: "poll-result", message: "" });
      setInitialLoading(false);
      if (manual) setRefreshing(false);
      inFlight.current = false;
      return;
    }
    const results = await Promise.allSettled([
      withTimeout(NetworkService.ListAdapters(), 8000, "Adapter refresh"),
      withTimeout(NetworkService.GetTunnelStatus(), 8000, "Tunnel status refresh"),
      withTimeout(NetworkService.GetTunnelLogs(), 8000, "Activity log refresh"),
      withTimeout(NetworkService.GetLogFilePath(), 8000, "Log path refresh"),
    ] as const);

    const failures: string[] = [];
    const adapterResult = results[0];
    const statusResult = results[1];
    const logsResult = results[2];
    const pathResult = results[3];

    if (adapterResult.status === "fulfilled") setAdapters(adapterResult.value ?? []);
    else failures.push(adapterResult.reason instanceof Error ? adapterResult.reason.message : "Adapter list unavailable");

    if (statusResult.status === "fulfilled") {
      const nextStatus = (statusResult.value ?? emptyStatus) as RuntimeStatus;
      const previous = statusRef.current;
      if (nextStatus.state === "running" && previous.state !== "running") {
        setHistory([]);
      }
      statusRef.current = nextStatus;
      setStatus(nextStatus);
      setLastUpdated(Date.now());
      if (nextStatus.state === "running") {
        const point: TrafficSample = {
          at: Date.now(),
          download: sumBy(nextStatus.links ?? [], "recvBytesPerSec"),
          upload: sumBy(nextStatus.links ?? [], "sendBytesPerSec"),
        };
        setHistory((current) => appendRollingSample(current, point));
      }
    } else failures.push(statusResult.reason instanceof Error ? statusResult.reason.message : "Tunnel state unavailable");

    if (logsResult.status === "fulfilled") setLogs(logsResult.value ?? []);
    else failures.push(logsResult.reason instanceof Error ? logsResult.reason.message : "Activity log unavailable");

    if (pathResult.status === "fulfilled") setLogPath(pathResult.value ?? "");
    else failures.push(pathResult.reason instanceof Error ? pathResult.reason.message : "Log path unavailable");

    dispatchErrors({ type: "poll-result", message: failures.length ? failures[0] : "" });
    setInitialLoading(false);
    if (manual) setRefreshing(false);
    inFlight.current = false;
  }, [previewMode]);

  useEffect(() => {
    let stopped = false;
    let timer: number | undefined;
    const poll = async () => {
      if (stopped) return;
      await refreshSnapshot();
      if (!stopped) {
        const active = statusRef.current.state === "running";
        timer = window.setTimeout(poll, pollInterval(active ? "running" : "stopped"));
      }
    };
    void poll();
    return () => {
      stopped = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [refreshSnapshot]);

  useEffect(() => {
    const byIndex = new Map((status.links ?? []).map((link) => [link.index, link]));
    setPolicies((current) => {
      let changed = false;
      const next = { ...current };
      for (const adapter of adapters) {
        const link = byIndex.get(adapter.index);
        const existing = next[adapter.index];
        if (!existing) {
          next[adapter.index] = { enabled: link?.enabled ?? adapter.isUp, weight: link?.weight ?? 1, dirty: false };
          changed = true;
        } else if (!existing.dirty && link && (existing.enabled !== link.enabled || existing.weight !== link.weight)) {
          next[adapter.index] = { enabled: link.enabled ?? existing.enabled, weight: link.weight, dirty: false };
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [adapters, status.links]);

  const handleTunnelToggle = async () => {
    if (tunnelBusy || previewMode) return;
    setActionPending(true);
    dispatchErrors({ type: "action-started" });
    setNotice("");
    try {
      if (tunnelActive) {
        await withTimeout(NetworkService.StopTunnel(), 45000, "Tunnel stop");
      } else {
        await withTimeout(NetworkService.StartTunnel(), 60000, "Tunnel start");
      }
      await refreshSnapshot(true);
    } catch (cause) {
      dispatchErrors({ type: "action-failed", message: cause instanceof Error ? cause.message : "The tunnel action failed." });
      await refreshSnapshot();
    } finally {
      setActionPending(false);
    }
  };

  const updatePolicy = (index: number, policy: LinkPolicy) => {
    setPolicies((current) => ({ ...current, [index]: policy }));
  };

  const applyPolicy = async (index: number) => {
    const policy = policies[index];
    const configure = serviceWithPolicy.ConfigureLink;
    if (!configure || !policy || savingIndex !== null) return;
    setSavingIndex(index);
    dispatchErrors({ type: "action-started" });
    try {
      await withTimeout(configure(index, policy.enabled, policy.weight), 10000, "Link settings save");
      setPolicies((current) => ({ ...current, [index]: { ...policy, dirty: false } }));
      await refreshSnapshot(true);
    } catch (cause) {
      dispatchErrors({ type: "action-failed", message: cause instanceof Error ? cause.message : "Could not save link settings." });
    } finally {
      setSavingIndex(null);
    }
  };

  const copyLogPath = async () => {
    if (!logPath) return;
    try {
      await navigator.clipboard.writeText(logPath);
      setNotice("Log location copied to clipboard.");
    } catch {
      setNotice("Clipboard access is unavailable. Select the log location in Settings to copy it manually.");
    }
  };

  const clearActionError = () => dispatchErrors({ type: "action-dismissed" });
  const connectedInterfaces = adapters.filter((adapter) => adapter.isUp).length;
  const stale = isStale(lastUpdated);
  const tunnelLabel = previewMode ? "Visual preview" : tunnelBusy ? "Working" : tunnelActive ? "Tunnel active" : status.state === "error" ? "Needs attention" : "Tunnel off";
  const pageTitle = viewInfo.title;

  return (
    <div className="app-shell">
      <Sidebar active={view} onChange={setView} />
      <main className="workspace">
        <header className="topbar">
          <div className="breadcrumb"><span>LINK ALLOY</span><b>/</b><strong>{pageTitle}</strong></div>
          <div className="topbar-status">
            <span className={`build-pill ${previewMode ? "build-pill-preview" : tunnelActive ? "build-pill-live" : status.state === "error" ? "build-pill-error" : ""}`} aria-live="polite">
              <i aria-hidden="true" />{tunnelLabel}
            </span>
            <span className={`sync-label ${stale ? "sync-label-stale" : ""}`}>{previewMode ? "Sample values" : lastUpdated ? stale ? "Status delayed" : "Live status" : "Connecting"}</span>
          </div>
        </header>

        <div className="page-content">
          <section className="page-title-row">
            <div>
              <p className="eyebrow">{viewInfo.eyebrow}</p>
              <h1>{pageTitle}</h1>
              <p className="page-description">{viewInfo.description}</p>
            </div>
            <div className="page-actions"><RateUnitControl /><button className="icon-button refresh-action" type="button" onClick={() => void refreshSnapshot(true)} disabled={refreshing} aria-label="Refresh network status" title="Refresh status"><Icon name="refresh" /></button></div>
          </section>

          {previewMode && <div className="preview-banner" role="note"><strong>Visual preview</strong><span>Sample values only · not live telemetry · controls are disabled · no network routes are changed</span></div>}

          {errors.actionError && <div className="toast-banner toast-error" role="alert"><span>{errors.actionError}</span><button type="button" onClick={clearActionError} aria-label="Dismiss error">×</button></div>}
          {status.state === "error" && status.lastError && status.lastError !== errors.actionError && <div className="toast-banner toast-error" role="alert"><span>{status.lastError}</span></div>}
          {errors.refreshError && <div className="refresh-warning" role="status">Some live data is delayed: {errors.refreshError}</div>}
          {notice && <div className="toast-banner toast-success" role="status"><span>{notice}</span><button type="button" onClick={() => setNotice("")} aria-label="Dismiss message">×</button></div>}

          <div className={`view-content view-${view}`} key={view}>
          {view === "overview" && (
            <OverviewView
              adapters={adapters}
              links={links}
              active={tunnelActive}
              busy={tunnelBusy}
              initialLoading={initialLoading}
              onToggle={() => void handleTunnelToggle()}
              preview={previewMode}
              status={status}
              history={history}
              downloadRate={totalDownloadRate}
              uploadRate={totalUploadRate}
              totalReceived={totalReceived}
              totalSent={totalSent}
              activeConnections={activeConnections}
              transport={status.transport}
              usableLinks={usableLinks}
              connectedInterfaces={connectedInterfaces}
            />
          )}
          {view === "connections" && <ConnectionsView adapters={adapters} links={links} active={tunnelActive} transport={status.transport} flows={status.flows} preview={previewMode} />}
          {view === "activity" && <ActivityFeed entries={logs} path={logPath} />}
          {view === "settings" && (
            <SettingsView
              adapters={adapters}
              links={links}
              policies={policies}
              tunnelState={status.state}
              logPath={logPath}
              transport={status.transport}
              preview={previewMode}
              configureAvailable={configureAvailable}
              savingIndex={savingIndex}
              onPolicyChange={updatePolicy}
              onApplyPolicy={(index) => void applyPolicy(index)}
              onCopyLogPath={() => void copyLogPath()}
            />
          )}
          </div>

          <footer className="app-footer">
            <span>Link Alloy · Windows network control</span>
            <span>{previewMode ? "Sample values only · no system changes" : tunnelActive ? `${formatDurationSince(status.since)} active` : "Local adapter telemetry"} · No speed gain is guaranteed</span>
          </footer>
        </div>
      </main>
    </div>
  );
}

function OverviewView(props: {
  adapters: AdapterSummary[];
  links: LinkStatus[];
  active: boolean;
  busy: boolean;
  initialLoading: boolean;
  onToggle: () => void;
  preview: boolean;
  status: RuntimeStatus;
  history: TrafficSample[];
  downloadRate: number;
  uploadRate: number;
  totalReceived: number;
  totalSent: number;
  activeConnections: number;
  transport?: RuntimeStatus["transport"];
  usableLinks: number;
  connectedInterfaces: number;
}) {
  const [linkPage, setLinkPage] = useState(0);
  const lastLinkPage = Math.max(0, Math.ceil(props.adapters.length / 2) - 1);
  const currentPage = Math.min(linkPage, lastLinkPage);
  const visibleAdapters = props.adapters.slice(currentPage * 2, currentPage * 2 + 2);
  const byIndex = new Map(props.links.map((link) => [link.index, link]));
  const stateText = props.preview ? "Dashboard layout preview" : props.busy ? "Updating routes" : props.active ? "Network routing is on" : props.status.state === "error" ? "The tunnel needs attention" : "System routing is paused";
  const stateHelp = props.preview
    ? "Example values are shown to review the layout. No Windows routes are installed and no network traffic is being measured."
    : props.busy
    ? "Link Alloy is preparing or restoring Windows routes. Keep this window open until the status settles."
    : props.active
      ? "Supported new TCP/UDP connections are assigned to healthy links. Existing flows stay on their selected path."
      : props.status.state === "error"
        ? "Review the error above and the activity log before retrying."
        : "Start the tunnel to route supported traffic from Windows applications across eligible links.";
  const currentLinkCount = props.active ? props.usableLinks : props.connectedInterfaces;
  const currentLinkLabel = props.active ? "available for new flows" : "interfaces up";

  return (
    <div className="overview-layout">
        <section className={`tunnel-hero ${props.active ? "tunnel-hero-active" : ""}`} aria-labelledby="tunnel-status-title" aria-busy={props.busy}>
          <div className="tunnel-hero-copy">
            <div className="tunnel-state-line"><span className={`state-marker ${props.active ? "state-marker-active" : ""}`} /><span>{props.preview ? "VISUAL PREVIEW" : props.active ? "TUNNEL ACTIVE" : props.busy ? "IN PROGRESS" : props.status.state === "error" ? "ACTION REQUIRED" : "READY"}</span></div>
            <h2 id="tunnel-status-title">{stateText}</h2>
            <p>{stateHelp}</p>
            <button className={`primary-button ${props.active ? "primary-button-stop" : "primary-button-start"}`} type="button" onClick={props.onToggle} disabled={props.preview || props.busy || props.initialLoading} aria-label={props.preview ? "Tunnel controls are disabled in visual preview" : props.active ? "Stop Link Alloy tunnel" : "Start Link Alloy tunnel"}>
              <Icon name="power" />{props.preview ? "Preview only" : props.busy ? "Please wait…" : props.active ? "Stop tunnel" : "Start tunnel"}
            </button>
            {!props.active && !props.preview && <span className="hero-permission">Administrator access is required to install system routes.</span>}
          </div>
          <div className="tunnel-hero-side">
            <div className="hero-stat"><span>Eligible links</span><strong>{currentLinkCount}</strong><small>{currentLinkLabel}</small></div>
            <div className="hero-stat"><span>Active connections</span><strong>{props.active ? props.activeConnections : 0}</strong><small>{props.active && props.transport ? `${props.transport.activeTCP ?? 0} TCP · ${props.transport.activeUDP ?? 0} UDP` : "Current tunnel session"}</small></div>
            <div className="hero-stat"><span>Session duration</span><strong>{props.active ? formatDurationSince(props.status.since) : "—"}</strong><small>{props.active ? "Since start" : "Tunnel stopped"}</small></div>
          </div>
        </section>

      <div className="overview-main-column">
        <div className="overview-metric-grid">
          <div className="overview-metric"><span className="metric-label">Downloaded this session</span><strong>{formatBytes(props.totalReceived)}</strong><small>Across participating links</small></div>
          <div className="overview-metric"><span className="metric-label">Uploaded this session</span><strong>{formatBytes(props.totalSent)}</strong><small>Across participating links</small></div>
        </div>

        <ThroughputChart samples={props.history} download={props.downloadRate} upload={props.uploadRate} loading={props.busy} preview={props.preview} />
      </div>

      <aside className="overview-rail">
        <section className="panel network-panel" aria-labelledby="network-links-title">
          <div className="panel-header">
            <div><p className="eyebrow">AVAILABLE PATHS</p><h2 id="network-links-title">Network links</h2></div>
            <div className="link-pagination">
              <span className="panel-caption">{props.adapters.length} links</span>
              {lastLinkPage > 0 && <>
                <button className="secondary-button" type="button" aria-label="Previous network links" disabled={currentPage === 0} onClick={() => setLinkPage(currentPage - 1)}>‹</button>
                <span className="panel-caption">{currentPage + 1} / {lastLinkPage + 1}</span>
                <button className="secondary-button" type="button" aria-label="Next network links" disabled={currentPage === lastLinkPage} onClick={() => setLinkPage(currentPage + 1)}>›</button>
              </>}
            </div>
          </div>
          {props.initialLoading ? (
            <div className="skeleton-list" aria-label="Loading network links"><span /><span /></div>
          ) : props.adapters.length === 0 ? (
            <div className="empty-panel"><strong>No network links detected</strong><p>Connect Ethernet, Wi-Fi, or a tethered device and refresh.</p></div>
          ) : (
            <div className="link-card-list" tabIndex={0} role="region" aria-label="Network link details">
              {visibleAdapters.map((adapter) => <LinkCard key={adapter.index} adapter={adapter} link={byIndex.get(adapter.index)} tunnelActive={props.active} downloadShare={props.downloadRate > 0 ? (byIndex.get(adapter.index)?.recvBytesPerSec ?? 0) / props.downloadRate * 100 : 0} />)}
            </div>
          )}
          <p className="section-footnote">Traffic totals include Link Alloy flows only. Link health is measured by an egress check.</p>
        </section>
      </aside>
    </div>
  );
}

export default App;
