import type { AppView } from "../types";

const items: { id: AppView; label: string; icon: string }[] = [
  { id: "overview", label: "Overview", icon: "◫" },
  { id: "connections", label: "Connections", icon: "↔" },
  { id: "activity", label: "Activity", icon: "▤" },
  { id: "settings", label: "Settings", icon: "⚙" },
];

type Props = {
  active: AppView;
  onChange: (view: AppView) => void;
};

export function Sidebar({ active, onChange }: Props) {
  return (
    <aside className="sidebar" aria-label="Link Alloy navigation">
      <a className="sidebar-brand" href="#overview" onClick={(event) => { event.preventDefault(); onChange("overview"); }}>
        <span className="brand-mark" aria-hidden="true"><span>LA</span><i /></span>
        <span className="brand-copy"><strong>Link Alloy</strong><small>NETWORK CONTROL</small></span>
      </a>
      <div className="sidebar-divider" />
      <nav className="sidebar-nav" aria-label="Primary navigation">
        {items.map((item) => (
          <button
            className={`nav-item ${active === item.id ? "nav-item-active" : ""}`}
            type="button"
            key={item.id}
            onClick={() => onChange(item.id)}
            aria-current={active === item.id ? "page" : undefined}
            title={item.label}
          >
            <span className="nav-icon" aria-hidden="true">{item.icon}</span>
            <span>{item.label}</span>
          </button>
        ))}
      </nav>
      <div className="sidebar-bottom">
        <span className="sidebar-version"><i aria-hidden="true" /> Alpha development</span>
        <span className="sidebar-foot">0.0.2</span>
      </div>
    </aside>
  );
}
