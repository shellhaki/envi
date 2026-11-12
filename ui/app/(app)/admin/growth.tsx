/*
  The signup chart: an inline SVG area, drawn on the server.

  No chart library. Thirty points and one series does not justify shipping one,
  and a server-rendered SVG means the panel is complete in the first paint
  rather than popping in after hydration.
*/

type Point = { day: string; users: number };

const W = 760;
const H = 160;
const PAD = { top: 12, right: 4, bottom: 22, left: 4 };

export default function Growth({ points }: { points: Point[] }) {
  if (points.length < 2) return <p className="admin-foot">Not enough days yet.</p>;

  const peak = Math.max(1, ...points.map((p) => p.users));
  const plotW = W - PAD.left - PAD.right;
  const plotH = H - PAD.top - PAD.bottom;

  const x = (i: number) => PAD.left + (i / (points.length - 1)) * plotW;
  const y = (v: number) => PAD.top + plotH - (v / peak) * plotH;

  const line = points.map((p, i) => `${i ? "L" : "M"}${x(i).toFixed(1)} ${y(p.users).toFixed(1)}`).join(" ");
  const area = `${line} L${x(points.length - 1).toFixed(1)} ${PAD.top + plotH} L${x(0).toFixed(1)} ${PAD.top + plotH} Z`;

  const total = points.reduce((sum, p) => sum + p.users, 0);
  const first = points[0].day;
  const last = points[points.length - 1].day;

  return (
    <div className="admin-chart">
      <svg viewBox={`0 0 ${W} ${H}`} role="img"
           aria-label={`${total} signups over the last ${points.length} days, peaking at ${peak} in one day`}>
        <defs>
          <linearGradient id="growth-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent-line)" stopOpacity="0.38" />
            <stop offset="100%" stopColor="var(--accent-line)" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* A baseline, so a run of empty days reads as zero rather than as
            missing data. */}
        <line x1={PAD.left} y1={PAD.top + plotH} x2={W - PAD.right} y2={PAD.top + plotH}
              stroke="var(--line)" strokeWidth="1" />

        <path d={area} fill="url(#growth-fill)" />
        <path d={line} fill="none" stroke="var(--accent-line)" strokeWidth="2"
              strokeLinecap="round" strokeLinejoin="round" />

        {points.map((p, i) =>
          p.users > 0 ? (
            <circle key={p.day} cx={x(i)} cy={y(p.users)} r="3" fill="var(--accent-line)">
              <title>{`${p.day.slice(0, 10)}: ${p.users} signup${p.users === 1 ? "" : "s"}`}</title>
            </circle>
          ) : null,
        )}
      </svg>
      <div className="admin-chart-foot">
        <span>{first.slice(0, 10)}</span>
        <span>{total} total, peak {peak}</span>
        <span>{last.slice(0, 10)}</span>
      </div>
    </div>
  );
}
