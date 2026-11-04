type IconName = "overview" | "connections" | "activity" | "settings" | "wifi" | "ethernet" | "cellular" | "refresh" | "power";

const paths: Record<IconName, string[]> = {
  overview: ["M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z"],
  connections: ["M3 7h17m-4-4 4 4-4 4M21 17H4m4-4-4 4 4 4"],
  activity: ["M3 12h4l3-8 4 16 3-8h4"],
  settings: ["M4 6h16M4 12h16M4 18h16M8 3v6M16 9v6M10 15v6"],
  wifi: ["M3 8a15 15 0 0 1 18 0M6 12a10 10 0 0 1 12 0M9 16a5 5 0 0 1 6 0M12 20h.01"],
  ethernet: ["M5 3h14v10l-4 4H9l-4-4zM9 3v5m3-5v5m3-5v5M12 17v4M8 21h8"],
  cellular: ["M5 19v2M10 14v7M15 9v12M20 3v18"],
  refresh: ["M20 7v5h-5M4 17v-5h5M5.5 8a7 7 0 0 1 11.8-3L20 8M4 16l2.7 3A7 7 0 0 0 18.5 16"],
  power: ["M12 3v9M6 6a9 9 0 1 0 12 0"],
};

export function Icon({ name, className = "" }: { name: IconName; className?: string }) {
  return <svg className={`ui-icon ${className}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">{paths[name].map((path, index) => <path d={path} key={index} />)}</svg>;
}
