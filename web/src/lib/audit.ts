import { db } from "@/lib/db";

export type AuditAction =
  | "login"
  | "service.put"
  | "service.delete"
  | "mount.put"
  | "mount.delete"
  | "token.writeback"
  | "device.revoke";

export const clientIp = (req: Request): string =>
  req.headers.get("x-forwarded-for")?.split(",")[0]?.trim() ||
  req.headers.get("x-real-ip")?.trim() ||
  "";

export async function audit(
  userId: string,
  deviceId: string,
  action: AuditAction,
  target: string,
  ip: string
): Promise<void> {
  await db()
    .collection("audit")
    .insertOne({ userId, deviceId, action, target, ip, at: new Date() });
}
