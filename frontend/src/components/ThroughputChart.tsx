import { formatRate } from "../format";
import type { TrafficSample } from "../types";
import { useRateUnit } from "../rate-unit";

type Props = {
  samples: TrafficSample[];
  download: number;
  upload: number;
  loading: boolean;
  preview?: boolean;
};

const width = 720;
const height = 190;
const inset = 14;

function pathFor(samples: TrafficSample[], key: "download" | "upload", maximum: number) {
  if (samples.length < 2) return "";
  const plotWidth = width - inset * 2;
  const plotHeight = height - inset * 2;
  const windowStart = samples[samples.length - 1].at - 60_000;
  return samples.map((sample, index) => {
    const x = inset + Math.max(0, Math.min(1, (sample.at - windowStart) / 60_000)) * plotWidth;
    const y = height - inset - (Math.max(0, sample[key]) / maximum) * plotHeight;
    return `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
}

function areaFor(line: string) {
  if (!line) return "";
  const coords = line.replace(/^M/, "").split(" L");
  const firstX = Number(coords[0]?.split(",")[0] ?? inset);
  const lastX = Number(coords[coords.length - 1]?.split(",")[0] ?? width - inset);
  return `${line} L${lastX.toFixed(1)},${height - inset} L${firstX.toFixed(1)},${height - inset} Z`;
}

export function ThroughputChart({ samples, download, upload, loading, preview = false }: Props) {
  const { unit } = useRateUnit();
  const maximum = Math.max(1, ...samples.flatMap((sample) => [sample.download, sample.upload]));
  const downloadPath = pathFor(samples, "download", maximum);
  const uploadPath = pathFor(samples, "upload", maximum);
  const enoughSamples = samples.length >= 2;

  return (
    <section className="panel throughput-panel" aria-labelledby="throughput-title">
      <div className="panel-header">
        <div>
          <p className="eyebrow">LIVE TRAFFIC</p>
          <h2 id="throughput-title">Combined throughput</h2>
        </div>
        <span className={`sample-state ${enoughSamples ? "sample-state-live" : ""} ${preview ? "sample-state-preview" : ""}`}>
          <i aria-hidden="true" />{preview ? "Sample values · not live" : loading ? "Updating" : enoughSamples ? "Live samples" : "Waiting for traffic"}
        </span>
      </div>

      <div className="throughput-current">
        <div className="throughput-value throughput-download">
          <span className="metric-label"><i aria-hidden="true" /> Download</span>
          <strong>{formatRate(download, unit)}</strong>
        </div>
        <div className="throughput-value throughput-upload">
          <span className="metric-label"><i aria-hidden="true" /> Upload</span>
          <strong>{formatRate(upload, unit)}</strong>
        </div>
        <span className="chart-window">Rolling 60 seconds</span>
      </div>

      <div className="chart-wrap">
        <div className="chart-scale" aria-hidden="true"><span>{formatRate(maximum, unit)}</span><span>{formatRate(maximum / 2, unit)}</span><span>0</span></div>
        {enoughSamples ? (
          <svg className="throughput-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={preview ? "Sample combined upload and download rates for layout preview, not live" : "Combined upload and download rates sampled from Link Alloy link counters"}>
            <title>{preview ? "Sample throughput values for visual layout review" : "Combined throughput from real per-link byte counters"}</title>
            <desc>Blue is download; gray is upload. {preview ? "Values are preview samples and are not live." : "Points are sampled while the tunnel is active."}</desc>
            <line x1={inset} y1={height - inset} x2={width - inset} y2={height - inset} className="chart-axis" />
            <line x1={inset} y1={height / 2} x2={width - inset} y2={height / 2} className="chart-gridline" />
            <path d={areaFor(downloadPath)} className="chart-area chart-area-download" />
            <path d={areaFor(uploadPath)} className="chart-area chart-area-upload" />
            <path d={downloadPath} className="chart-line chart-line-download" />
            <path d={uploadPath} className="chart-line chart-line-upload" />
          </svg>
        ) : (
          <div className="chart-empty" role="status">
            <div className="chart-empty-line" aria-hidden="true"><span /><span /><span /><span /><span /></div>
            <p>{samples.length === 1 ? "Collecting the next real sample…" : "Start the tunnel and pass traffic to build the live history."}</p>
          </div>
        )}
      </div>
      <div className="chart-timeline" aria-hidden="true"><span>60 seconds ago</span><span>Now</span></div>

      <div className="chart-legend" aria-hidden="true">
        <span className="legend-download"><i />Download</span>
        <span className="legend-upload"><i />Upload</span>
        <span className="legend-source">Observed bytes per active link</span>
      </div>
    </section>
  );
}
