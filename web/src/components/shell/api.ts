"use client";

import { signedOut } from "@/hooks/use-resource";
import { busy } from "./busy";

export interface Sent {
  code: string;
  message: string;
  ok: boolean;
}

const OFFLINE: Sent = {
  code: "offline",
  message: "Couldn't reach dokky. Check your connection and try again.",
  ok: false,
};

export async function send(
  url: string,
  method: "PATCH" | "DELETE",
  body?: unknown
): Promise<Sent> {
  const init: RequestInit =
    body === undefined
      ? { method }
      : {
          body: JSON.stringify(body),
          headers: { "content-type": "application/json" },
          method,
        };
  const res = await busy(fetch(url, init)).catch(() => null);
  if (!res) {
    return OFFLINE;
  }
  if (res.ok) {
    return { code: "", message: "", ok: true };
  }
  if (res.status === 401) {
    signedOut();
  }
  const data = (await res.json().catch(() => null)) as {
    error?: { code?: string; message?: string };
  } | null;
  return {
    code: data?.error?.code ?? String(res.status),
    message:
      data?.error?.message ??
      `The server answered ${res.status}. Try again in a moment.`,
    ok: false,
  };
}
