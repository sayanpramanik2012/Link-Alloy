import { useState } from "react";
import { NetworkService } from "../../bindings/github.com/sayanpramanik2012/Link-Alloy";
import { withTimeout } from "../state-utils";
import "./NoticesPanel.css";

export function NoticesPanel() {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  async function toggle() {
    setOpen(value => !value);
    if (text || busy) return;
    setBusy(true);
    try {
      setText(await withTimeout(NetworkService.GetDistributionNotices(), 8000, "License notices"));
      setError("");
    } catch { setError("Could not load the bundled notices. Try again."); }
    finally { setBusy(false); }
  }
  return <section className="panel settings-card">
    <p className="eyebrow">LINK ALLOY 0.0.3 · ALPHA</p><h2>Licenses and acknowledgements</h2>
    <p>This portable executable includes its license and third-party notices.</p>
    <button className="secondary-button" type="button" aria-expanded={open} aria-controls="distribution-notices" onClick={toggle}>{open ? "Hide notices" : "Read bundled notices"}</button>
    {open && <div id="distribution-notices">{busy ? <p role="status">Loading notices…</p> : error ? <p role="alert">{error}</p> : <pre className="distribution-notices">{text}</pre>}</div>}
  </section>;
}
