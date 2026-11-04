export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  const digits = unit > 0 && amount < 10 ? 1 : 0;
  return `${amount.toFixed(digits)} ${units[unit]}`;
}

export function formatRate(bytesPerSecond: number, unit: "bits" | "bytes" = "bytes") {
  if (unit === "bits") {
    const mbps = Math.max(0, Number.isFinite(bytesPerSecond) ? bytesPerSecond : 0) * 8 / 1_000_000;
    return `${mbps.toFixed(mbps > 0 && mbps < 100 ? 1 : 0)} Mbps`;
  }
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) return "0 B/s";
  return `${formatBytes(bytesPerSecond)}/s`;
}

export function formatTime(value: string | undefined) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function formatDurationSince(value: string | undefined, now = Date.now()) {
  if (!value) return "—";
  const start = new Date(value).getTime();
  if (!Number.isFinite(start)) return "—";
  const seconds = Math.max(0, Math.floor((now - start) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainder = seconds % 60;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${remainder}s`;
  return `${remainder}s`;
}
