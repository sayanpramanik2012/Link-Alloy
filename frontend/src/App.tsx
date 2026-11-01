import { useCallback, useEffect, useMemo, useState } from "react";
import { NetworkService } from "../bindings/github.com/sayanpramanik2012/Link-Alloy";

type AdapterList = NonNullable<Awaited<ReturnType<typeof NetworkService.ListAdapters>>>;
type Adapter = AdapterList[number];

function adapterGlyph(kind: string) {
  if (kind === "wifi") return "≈";
  if (kind === "ethernet") return "⇄";
  if (kind === "cellular") return "▥";
  return "⌁";
}

function App() {
  const [adapters, setAdapters] = useState<Adapter[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const activeCount = useMemo(() => adapters.filter((adapter) => adapter.isUp).length, [adapters]);

  const refreshAdapters = useCallback(async () => {
    setRefreshing(true);
    try {
      setAdapters((await NetworkService.ListAdapters()) ?? []);
      setError("");
      setLastUpdated(new Date());
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Adapter discovery is unavailable.");
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void refreshAdapters();
    const timer = window.setInterval(() => void refreshAdapters(), 5000);
    return () => window.clearInterval(timer);
  }, [refreshAdapters]);

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Link Alloy">
        <div className="brand-mark" aria-hidden="true"><span>L</span><i /></div>
        <div className="sidebar-divider" />
        <div className="sidebar-current" aria-current="page" title="Overview">⌂</div>
        <div className="sidebar-foot">0.0.1</div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div className="breadcrumb"><span>LINK ALLOY</span><b>/</b><strong>Overview</strong></div>
          <span className="build-pill"><i /> Independent development build</span>
        </header>

        <div className="page-content">
          <section className="intro">
            <div>
              <p className="eyebrow">MULTI-LINK DESKTOP · DEVELOPMENT PREVIEW</p>
              <h1>Bring your links<br /><span>into one view.</span></h1>
              <p className="intro-copy">Link Alloy is being rebuilt as an independent Windows application. This preview reads adapter status only; it does not route or alter network traffic.</p>
            </div>
            <div className="status-orb" aria-label="Routing engine not available in this build">
              <div className="orb-ring ring-one" />
              <div className="orb-ring ring-two" />
              <div className="orb-core">LA</div>
              <span className="orb-label">ENGINE IN DEVELOPMENT</span>
            </div>
          </section>

          <section className="engine-panel" aria-labelledby="engine-title">
            <div className="engine-panel-main">
              <div className="engine-title-row">
                <div className="engine-icon">⇄</div>
                <div>
                  <p className="eyebrow">ROUTING ENGINE</p>
                  <h2 id="engine-title">Not active in this build</h2>
                </div>
              </div>
              <p className="engine-description">The independent routing engine has not been implemented yet. No traffic is being redirected, and no tunnel or system proxy is started.</p>
              <button className="primary-button" type="button" disabled title="Routing is not implemented in this development preview">
                <span className="button-dot" /> Tunnel unavailable
              </button>
            </div>
            <div className="engine-panel-side">
              <span className="side-label">CURRENT MODE</span>
              <strong>Adapter discovery only</strong>
              <span className="side-note">Read-only · local device</span>
            </div>
          </section>

          <section className="adapter-section" aria-labelledby="adapter-title">
            <div className="section-heading">
              <div>
                <p className="eyebrow">YOUR WINDOWS DEVICE</p>
                <h2 id="adapter-title">Network adapters <span className="count-badge">{adapters.length}</span></h2>
              </div>
              <div className="section-actions">
                {lastUpdated && <span className="updated-label">Updated {lastUpdated.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>}
                <button className="refresh-button" type="button" onClick={() => void refreshAdapters()} disabled={refreshing} aria-label="Refresh adapter list">
                  <span className={refreshing ? "refresh-icon spinning" : "refresh-icon"}>↻</span> Refresh
                </button>
              </div>
            </div>

            {error && <div className="error-banner" role="alert">{error}</div>}
            {loading ? (
              <div className="empty-state"><div className="loader" /><p>Reading adapter state…</p></div>
            ) : adapters.length === 0 ? (
              <div className="empty-state"><div className="empty-icon">⌁</div><h3>No non-loopback adapters detected</h3><p>Connect Ethernet, Wi-Fi, or a tethered device, then refresh.</p></div>
            ) : (
              <div className="adapter-grid">
                {adapters.map((adapter) => (
                  <article className={`adapter-card ${adapter.isUp ? "is-up" : "is-down"}`} key={adapter.index}>
                    <div className="adapter-card-top">
                      <div className={`adapter-glyph ${adapter.kind}`}>{adapterGlyph(adapter.kind)}</div>
                      <span className={`interface-state ${adapter.isUp ? "up" : "down"}`}><i />{adapter.isUp ? "Interface up" : "Interface down"}</span>
                    </div>
                    <h3 title={adapter.name}>{adapter.name}</h3>
                    <p className="adapter-kind">{adapter.kindLabel}</p>
                    <div className="adapter-meta"><span>Interface index</span><strong>{adapter.index}</strong></div>
                    <div className="adapter-meta"><span>MTU</span><strong>{adapter.mtu || "—"}</strong></div>
                    <div className="telemetry-placeholder"><span className="telemetry-line" /><span>Traffic telemetry will arrive with the routing engine</span></div>
                  </article>
                ))}
              </div>
            )}
            <p className="adapter-count-note">{activeCount} interface(s) currently report as up. This does not confirm internet reachability.</p>
          </section>

          <section className="roadmap" aria-labelledby="roadmap-title">
            <div>
              <p className="eyebrow">BUILD STATUS</p>
              <h2 id="roadmap-title">A separate implementation, in stages.</h2>
            </div>
            <div className="roadmap-items">
              <div className="roadmap-item complete"><span>01</span><div><strong>Windows desktop shell</strong><small>Complete</small></div></div>
              <div className="roadmap-item complete"><span>02</span><div><strong>Read-only adapter inventory</strong><small>Complete</small></div></div>
              <div className="roadmap-item planned"><span>03</span><div><strong>Independent multi-link connection engine</strong><small>In development</small></div></div>
              <div className="roadmap-item planned"><span>04</span><div><strong>All-app tunnel and new-flow failover</strong><small>Planned · existing sessions may reconnect</small></div></div>
            </div>
          </section>

          <footer className="footer">
            <span>Link Alloy · development preview · no routing active</span>
            <span>Inspired by publicly described multi-link approaches; no upstream source code is included in this project.</span>
          </footer>
        </div>
      </main>
    </div>
  );
}

export default App;
