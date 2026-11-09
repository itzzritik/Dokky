const MAX_KEYS = 10_000;

// counts live in this server instance only, so each instance allows the full rate on its own
export function rateLimit(limit: number, windowMs = 60_000) {
  const windows = new Map<string, { count: number; resetAt: number }>();
  // seconds to wait, 0 when this call is allowed
  return (key: string, now = Date.now()): number => {
    let slot = windows.get(key);
    if (!slot || slot.resetAt <= now) {
      if (windows.size >= MAX_KEYS) {
        for (const [k, w] of windows) {
          if (w.resetAt <= now) {
            windows.delete(k);
          }
        }
      }
      slot = { count: 0, resetAt: now + windowMs };
      windows.set(key, slot);
    }
    slot.count += 1;
    return slot.count > limit
      ? Math.max(1, Math.ceil((slot.resetAt - now) / 1000))
      : 0;
  };
}

export const tooMany = (wait: number): Response =>
  Response.json(
    {
      error: {
        code: "rate_limited",
        message: `too many requests, try again in ${wait}s`,
      },
    },
    { status: 429, headers: { "retry-after": String(wait) } }
  );
