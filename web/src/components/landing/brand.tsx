const GLYPHS: Record<string, string[]> = {
  d: [
    "......##",
    "......##",
    ".#######",
    "########",
    "##....##",
    "##....##",
    "########",
    ".#######",
  ],
  o: [
    "",
    "",
    ".######.",
    "########",
    "##....##",
    "##....##",
    "########",
    ".######.",
  ],
  k: [
    "##.....",
    "##.....",
    "##...##",
    "##..##.",
    "#####..",
    "##.##..",
    "##..##.",
    "##...##",
  ],
  y: [
    "",
    "",
    "##....##",
    "##....##",
    "##....##",
    "##....##",
    "########",
    ".#######",
    "......##",
    ".######.",
  ],
};

// the TUI's pixel wordmark, 46 by 10
const PIXELS = (() => {
  const out: [number, number][] = [];
  let left = 0;
  for (const ch of "dokky") {
    const rows = GLYPHS[ch];
    for (const [y, row] of rows.entries()) {
      for (const [i, c] of [...row].entries()) {
        if (c === "#") {
          out.push([left + i, y]);
        }
      }
    }
    left += Math.max(...rows.map((r) => r.length)) + 2;
  }
  return out;
})();

const rects = PIXELS.map(([x, y]) => (
  <rect height="1" key={`${x}-${y}`} width="1" x={x} y={y} />
));

const WAVE =
  "M0 0.6 Q 5.75 -0.4 11.5 0.6 T 23 0.6 T 34.5 0.6 T 46 0.6 T 57.5 0.6 T 69 0.6 T 80.5 0.6 T 92 0.6 V 14 H 0 Z";

export function DokkyMark({
  width,
  water = false,
  className,
}: {
  width: number;
  water?: boolean;
  className?: string;
}) {
  if (!water) {
    return (
      <svg
        aria-label="dokky"
        className={className}
        fill="currentColor"
        role="img"
        viewBox="0 0 46 10"
        width={width}
      >
        {rects}
      </svg>
    );
  }
  // fills once like the TUI splash, then rests about two thirds full
  return (
    <svg
      aria-label="dokky"
      className={className}
      data-water
      role="img"
      style={{ overflow: "visible" }}
      viewBox="0 0 46 10"
      width={width}
    >
      <defs>
        <clipPath id="dk-water-clip">{rects}</clipPath>
        <linearGradient id="dk-water-fill" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" stopColor="#74E8E1" />
          <stop offset="0.35" stopColor="#22B8CC" />
          <stop offset="1" stopColor="#0A4E66" />
        </linearGradient>
      </defs>
      <g clipPath="url(#dk-water-clip)">
        <rect fill="#ECEFF0" height="10" width="46" />
        <g
          style={{
            transform: "translateY(3.2px)",
            animation: "dk-rise 3.2s cubic-bezier(0.2, 0.8, 0.2, 1) both",
          }}
        >
          <path
            d={WAVE}
            fill="url(#dk-water-fill)"
            style={{ animation: "dk-wave 4s linear 3" }}
          />
        </g>
      </g>
    </svg>
  );
}
