import { createContext, useContext, useState, type ReactNode } from "react";

export type RateUnit = "bits" | "bytes";
const RateUnitContext = createContext<{ unit: RateUnit; setUnit: (unit: RateUnit) => void }>({ unit: "bits", setUnit: () => {} });

export function RateUnitProvider({ children }: { children: ReactNode }) {
  const [unit, updateUnit] = useState<RateUnit>(() => {
    try { return localStorage.getItem("link-alloy.rate-unit") === "bytes" ? "bytes" : "bits"; }
    catch { return "bits"; }
  });
  function setUnit(value: RateUnit) {
    updateUnit(value);
    try { localStorage.setItem("link-alloy.rate-unit", value); } catch { /* The preference still works for this window. */ }
  }
  return <RateUnitContext.Provider value={{ unit, setUnit }}>{children}</RateUnitContext.Provider>;
}

export function useRateUnit() { return useContext(RateUnitContext); }

export function RateUnitControl() {
  const { unit, setUnit } = useRateUnit();
  return <div className="unit-control" role="group" aria-label="Traffic rate units">
    <button type="button" aria-pressed={unit === "bits"} onClick={() => setUnit("bits")}>Mbps</button>
    <button type="button" aria-pressed={unit === "bytes"} onClick={() => setUnit("bytes")}>MiB/s</button>
  </div>;
}
