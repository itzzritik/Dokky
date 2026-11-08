import { createHash, randomBytes, randomInt } from "node:crypto";
import { ObjectId, type WithId } from "mongodb";
import { valid } from "semver";
import type { z } from "zod";
import { audit } from "@/lib/audit";
import { db } from "@/lib/db";
import { apiError } from "@/lib/errors";
import { record } from "@/lib/events";
import { type loginStartBody, USER_CODE } from "@/lib/schemas";

const LOGIN_TTL_MS = 600_000;
const SEEN_EVERY_MS = 60_000;
const CODE_CHARS = "BCDFGHJKLMNPQRSTVWXZ";
const BEARER = /^Bearer dky_[\w-]{43}$/u;
const OS_NAME = /^[^\p{Cc}]{1,64}$/u;

export type RemoveMode = "remove" | "erase";

export interface Device {
  arch: string;
  createdAt: Date;
  driveIds?: string[];
  erasedAt?: Date | null;
  lastSeenAt: Date;
  name: string;
  os: string;
  osName?: string;
  removedBy?: string;
  removeMode?: RemoveMode;
  revokedAt: Date | null;
  tokenHash: string;
  userId: string;
  version: string;
}

interface CliLogin {
  arch: string;
  createdAt: Date;
  deviceCodeHash: string;
  deviceName: string;
  expiresAt: Date;
  os: string;
  status: "pending" | "approved" | "denied";
  userCode: string;
  userId?: string;
  version: string;
}

export type PollResult =
  | { status: "pending" | "denied" | "expired" }
  | { status: "approved"; token: string; deviceId: string };

const sha256 = (value: string): string =>
  createHash("sha256").update(value).digest("hex");

export const devices = () => db().collection<Device>("devices");
const cliLogins = () => db().collection<CliLogin>("cliLogins");

const codePart = () =>
  Array.from(
    { length: 4 },
    () => CODE_CHARS[randomInt(CODE_CHARS.length)]
  ).join("");

export async function startLogin(
  info: z.infer<typeof loginStartBody>,
  now: Date
) {
  const deviceCode = randomBytes(32).toString("base64url");
  const userCode = `${codePart()}-${codePart()}`;
  await cliLogins().insertOne({
    ...info,
    deviceCodeHash: sha256(deviceCode),
    userCode,
    status: "pending",
    createdAt: now,
    expiresAt: new Date(now.getTime() + LOGIN_TTL_MS),
  });
  return {
    deviceCode,
    userCode,
    verifyUrl: `https://dokky.ritik.me/cli/approve?code=${userCode}`,
    interval: 2,
    expiresIn: LOGIN_TTL_MS / 1000,
  };
}

export async function pollLogin(
  deviceCode: string,
  ip: string,
  now: Date
): Promise<PollResult> {
  const deviceCodeHash = sha256(deviceCode);
  const login = await cliLogins().findOne({ deviceCodeHash });
  if (!login || login.expiresAt <= now) {
    return { status: "expired" };
  }
  if (login.status !== "approved") {
    return { status: login.status };
  }
  const claimed = await cliLogins().findOneAndDelete({
    deviceCodeHash,
    status: "approved",
    expiresAt: { $gt: now },
  });
  if (!claimed?.userId) {
    return { status: "expired" };
  }
  const token = `dky_${randomBytes(32).toString("base64url")}`;
  const { insertedId } = await devices().insertOne({
    userId: claimed.userId,
    name: claimed.deviceName,
    os: claimed.os,
    arch: claimed.arch,
    version: claimed.version,
    tokenHash: sha256(token),
    createdAt: now,
    lastSeenAt: now,
    revokedAt: null,
  });
  const deviceId = String(insertedId);
  await audit(claimed.userId, deviceId, "login", claimed.deviceName, ip);
  await record(claimed.userId, [
    {
      kind: "device.added",
      deviceId,
      target: deviceId,
      name: claimed.deviceName,
      ok: true,
      reason: "",
      at: now,
    },
  ]);
  return { status: "approved", token, deviceId };
}

export const pendingLogin = (userCode: string, now: Date) =>
  cliLogins().findOne({
    userCode,
    status: "pending",
    expiresAt: { $gt: now },
  });

export async function decideLogin(
  userId: string,
  userCode: unknown,
  status: "approved" | "denied",
  now: Date
): Promise<boolean> {
  if (typeof userCode !== "string" || !USER_CODE.test(userCode)) {
    return false;
  }
  const { modifiedCount } = await cliLogins().updateOne(
    { userCode, status: "pending", expiresAt: { $gt: now } },
    { $set: { status, userId } }
  );
  return modifiedCount === 1;
}

async function revoked(
  device: WithId<Device>,
  aware: boolean,
  now: Date
): Promise<Response> {
  if (!aware) {
    // an older dokky wipes itself on this answer
    await devices().updateOne(
      { _id: device._id, erasedAt: null },
      { $set: { erasedAt: now } }
    );
    return apiError(401, "device_revoked", "this device was revoked");
  }
  return Response.json(
    {
      error: { code: "device_revoked", message: "this device was removed" },
      mode: device.removeMode ?? "erase",
    },
    { status: 401 }
  );
}

export async function authDevice(
  req: Request,
  now = new Date(),
  opts: { removed?: "report" | "writeback" } = {}
): Promise<{ userId: string; deviceId: string; removed: boolean } | Response> {
  const header = req.headers.get("authorization") ?? "";
  if (!BEARER.test(header)) {
    return apiError(401, "unauthorized", "a device token is required");
  }
  const device = await devices().findOne({
    tokenHash: sha256(header.slice("Bearer ".length)),
  });
  if (!device) {
    return apiError(401, "unauthorized", "unknown device token");
  }
  if (device.revokedAt) {
    const aware = req.headers.get("x-dokky-removal") === "1";
    // a lost or stolen device can still say it erased, but never write tokens other devices use
    const allowed =
      opts.removed === "report" ||
      (opts.removed === "writeback" && device.removeMode === "remove");
    if (aware && allowed && !device.erasedAt) {
      return {
        userId: device.userId,
        deviceId: String(device._id),
        removed: true,
      };
    }
    return await revoked(device, aware, now);
  }
  const version = req.headers.get("x-dokky-version") ?? "";
  const osName = (req.headers.get("x-dokky-os") ?? "").trim();
  const cutoff = new Date(now.getTime() - SEEN_EVERY_MS);
  if (device.lastSeenAt <= cutoff) {
    await devices().updateOne(
      { _id: device._id, lastSeenAt: { $lte: cutoff } },
      { $set: { lastSeenAt: now } }
    );
  }
  const set: Partial<Device> = {};
  if (valid(version) && version !== device.version) {
    set.version = version;
  }
  if (OS_NAME.test(osName) && osName !== device.osName) {
    set.osName = osName;
  }
  if (Object.keys(set).length > 0) {
    await devices().updateOne({ _id: device._id }, { $set: set });
  }
  return {
    userId: device.userId,
    deviceId: String(device._id),
    removed: false,
  };
}

export const listDevices = (userId: string) =>
  devices().find({ userId, revokedAt: null }).sort({ createdAt: 1 }).toArray();

// the device leaves every list now and erases itself on its next call
export async function removeDevice(
  userId: string,
  id: string,
  mode: RemoveMode,
  actorDeviceId: string,
  ip: string,
  now = new Date()
): Promise<boolean> {
  if (!ObjectId.isValid(id)) {
    return false;
  }
  const _id = new ObjectId(id);
  const device = await devices().findOne({ _id, userId });
  if (!device) {
    return false;
  }
  const { modifiedCount } = await devices().updateOne(
    { _id, userId, revokedAt: null },
    {
      $set: {
        revokedAt: now,
        removeMode: mode,
        removedBy: actorDeviceId || "Web",
        erasedAt: null,
      },
    }
  );
  if (modifiedCount === 0 && mode === "erase") {
    await devices().updateOne(
      { _id, userId, removeMode: "remove", erasedAt: null },
      { $set: { removeMode: "erase" } }
    );
  }
  if (modifiedCount === 1) {
    await audit(userId, actorDeviceId, "device.revoke", device.name, ip);
    await record(userId, [
      {
        kind: "device.removed",
        deviceId: actorDeviceId,
        target: id,
        name: device.name,
        ok: true,
        reason: "",
        at: now,
      },
    ]);
  }
  return true;
}

export async function markErased(
  userId: string,
  id: string,
  now = new Date()
): Promise<void> {
  const device = await devices().findOneAndUpdate(
    {
      _id: new ObjectId(id),
      userId,
      revokedAt: { $ne: null },
      erasedAt: null,
    },
    { $set: { erasedAt: now } }
  );
  // devices of a deleted account still report their erase
  const owner = await db()
    .collection("users")
    .countDocuments({ _id: new ObjectId(userId) }, { limit: 1 });
  if (device && owner) {
    await record(userId, [
      {
        kind: "device.erased",
        deviceId: id,
        target: id,
        name: device.name,
        ok: true,
        reason: "",
        at: now,
      },
    ]);
  }
}

export async function renameDevice(
  userId: string,
  id: string,
  name: string,
  actorDeviceId: string,
  ip: string
): Promise<boolean> {
  if (!ObjectId.isValid(id)) {
    return false;
  }
  const { matchedCount } = await devices().updateOne(
    { _id: new ObjectId(id), userId, revokedAt: null },
    { $set: { name } }
  );
  if (matchedCount === 1) {
    await audit(userId, actorDeviceId, "device.rename", name, ip);
    await record(userId, [
      {
        kind: "device.renamed",
        deviceId: actorDeviceId,
        target: id,
        name,
        ok: true,
        reason: "",
        at: new Date(),
      },
    ]);
  }
  return matchedCount === 1;
}

export async function setDeviceDrives(
  userId: string,
  id: string,
  mountIds: string[]
): Promise<void> {
  await devices().updateOne(
    { _id: new ObjectId(id), userId, revokedAt: null },
    { $set: { driveIds: mountIds } }
  );
}
