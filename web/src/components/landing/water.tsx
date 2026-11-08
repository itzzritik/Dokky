const BLOBS = [
  { left: "8%", top: "18%", size: 520, duration: 26, delay: 0 },
  { left: "46%", top: "52%", size: 640, duration: 34, delay: -9 },
  { left: "70%", top: "8%", size: 460, duration: 30, delay: -17 },
];

// light through water, drifting slowly over the hero grid
export function Caustics({ className = "" }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={`pointer-events-none absolute inset-0 overflow-hidden ${className}`}
      data-water
    >
      {BLOBS.map((b) => (
        <span
          className="absolute rounded-full"
          key={b.left}
          style={{
            left: b.left,
            top: b.top,
            width: b.size,
            height: b.size * 0.6,
            background:
              "radial-gradient(ellipse at center, rgba(34,184,204,0.07) 0%, rgba(14,127,153,0.03) 45%, transparent 70%)",
            animation: `drift ${b.duration}s ease-in-out ${b.delay}s infinite`,
          }}
        />
      ))}
    </div>
  );
}

const LINE = (() => {
  let d = "M0 6";
  for (let x = 0; x < 2400; x += 120) {
    d += ` Q ${x + 30} 1 ${x + 60} 6 T ${x + 120} 6`;
  }
  return d;
})();

// a gentle surface line along a section's top edge
export function Waterline({ className = "" }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={`pointer-events-none absolute inset-x-0 top-0 h-3 overflow-hidden ${className}`}
      data-water
    >
      <svg
        aria-hidden
        className="h-3 w-[200%]"
        preserveAspectRatio="none"
        style={{ animation: "tide 22s linear infinite" }}
        viewBox="0 0 2400 12"
      >
        <path
          d={LINE}
          fill="none"
          stroke="rgba(34,184,204,0.28)"
          strokeWidth="1"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
    </div>
  );
}
