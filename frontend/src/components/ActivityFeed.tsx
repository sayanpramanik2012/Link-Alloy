import { useMemo, useState } from "react";
import { formatBytes, formatTime } from "../format";
import type { TunnelLogEntry } from "../types";

type Props = {
  entries: TunnelLogEntry[];
  compact?: boolean;
  path: string;
};

function trafficSummary(entry: TunnelLogEntry) {
  const parts: string[] = [];
  if (entry.bytesReceived) parts.push(`↓ ${formatBytes(entry.bytesReceived)}`);
  if (entry.bytesSent) parts.push(`↑ ${formatBytes(entry.bytesSent)}`);
  return parts.join(" · ");
}

export function ActivityFeed({ entries, compact = false, path }: Props) {
  const [filter, setFilter] = useState("all");
  const [query, setQuery] = useState("");
  const visible = useMemo(() => {
    let result = [...entries].reverse();
    if (filter !== "all") result = result.filter((entry) => entry.level === filter);
    const normalized = query.trim().toLowerCase();
    if (normalized) result = result.filter((entry) => `${entry.category} ${entry.message}`.toLowerCase().includes(normalized));
    return compact ? result.slice(0, 5) : result;
  }, [compact, entries, filter, query]);

  return (
    <section className={`panel activity-panel ${compact ? "activity-panel-compact" : ""}`} aria-labelledby="activity-heading">
      <div className="panel-header activity-panel-header">
        <div>
          <p className="eyebrow">AUDIT TRAIL</p>
          <h2 id="activity-heading">{compact ? "Recent activity" : "Activity log"}</h2>
        </div>
        {!compact && (
          <div className="activity-filters">
            <label className="sr-only" htmlFor="activity-level">Filter by severity</label>
            <select id="activity-level" value={filter} onChange={(event) => setFilter(event.target.value)}>
              <option value="all">All levels</option>
              <option value="info">Information</option>
              <option value="warning">Warnings</option>
              <option value="error">Errors</option>
            </select>
            <label className="sr-only" htmlFor="activity-search">Search activity</label>
            <input id="activity-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search log" />
          </div>
        )}
      </div>

      {visible.length === 0 ? (
        <div className="activity-empty" role="status">
          {entries.length === 0 ? "Tunnel state, link health and connection events will be recorded here." : "No log entries match this filter."}
        </div>
      ) : (
        <ol className="log-list" aria-live="polite">
          {visible.map((entry, index) => (
            <li className={`log-entry log-${entry.level}`} key={`${entry.at}-${entry.category}-${index}`}>
              <time dateTime={entry.at} title={new Date(entry.at).toLocaleString()}>{formatTime(entry.at)}</time>
              <span className="log-level">{entry.level}</span>
              <span className="log-message" title={entry.message}>{entry.message}</span>
              {trafficSummary(entry) && <span className="log-traffic">{trafficSummary(entry)}</span>}
            </li>
          ))}
        </ol>
      )}

      <div className="activity-footer">
        <span>{path || "%ProgramData%\\Link Alloy\\logs\\engine.jsonl"}</span>
        <span>Events are stored locally; payloads and destination addresses are not logged.</span>
      </div>
    </section>
  );
}
