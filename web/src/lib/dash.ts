import { MongoServerError, ObjectId } from "mongodb";
import { auth } from "@/auth";
import { audit } from "@/lib/audit";
import { db } from "@/lib/db";
import { listDevices, removeDevice } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";
import { events, record } from "@/lib/events";
import { entityId, mountBody } from "@/lib/schemas";
import { mounts, services, users } from "@/lib/sync";
import { vaults } from "@/lib/vault";

const LOCAL = /\.local$/u;
const byName = (a: { name: string }, b: { name: string }) =>
  a.name.localeCompare(b.name);

export const sessionUser = async (): Promise<string | null> =>
  (await auth())?.user?.id ?? null;

export const unauthorized = () =>
  apiError(401, "unauthorized", "sign in to dokky first");

export const fresh = () =>
  apiError(403, "fresh_signin", "sign in to GitHub again first");

export const json = (data: unknown) =>
  Response.json(data, { headers: { "cache-control": "no-store" } });

export const validId = (id: string) => entityId.safeParse(id).success;

const bump = (userId: string) =>
  users().updateOne({ _id: new ObjectId(userId) }, { $inc: { rev: 1 } });

export async function listDrives(userId: string) {
  const [owned, drives, live] = await Promise.all([
    services()
      .find({ userId }, { projection: { name: 1, type: 1 } })
      .toArray(),
    mounts().find({ userId }).toArray(),
    listDevices(userId),
  ]);
  return {
    accounts: owned.sort(byName).map((s) => ({
      id: s._id,
      name: s.name,
      type: s.type,
      drives: drives
        .filter((m) => m.serviceId === s._id)
        .sort(byName)
        .map((m) => ({
          id: m._id,
          name: m.name,
          path: m.remotePath,
          addedAt: (m.createdAt ?? m.updatedAt).toISOString(),
          devices: live
            .filter((d) => d.driveIds?.includes(m._id))
            .map((d) => String(d._id)),
        })),
    })),
    devices: live.map((d) => ({
      id: String(d._id),
      name: d.name.replace(LOCAL, ""),
      lastSeenAt: d.lastSeenAt.toISOString(),
    })),
  };
}

export async function listDashDevices(userId: string) {
  const [live, drives, owned] = await Promise.all([
    listDevices(userId),
    mounts()
      .find({ userId }, { projection: { name: 1 } })
      .toArray(),
    services()
      .find({ userId }, { projection: { name: 1 } })
      .toArray(),
  ]);
  const names = new Map(drives.map((m) => [m._id, m.name]));
  return {
    accounts: owned.map((s) => s.name).sort(),
    devices: live.map((d) => ({
      id: String(d._id),
      name: d.name.replace(LOCAL, ""),
      os: d.osName || d.os,
      arch: d.arch,
      version: d.version,
      lastSeenAt: d.lastSeenAt.toISOString(),
      createdAt: d.createdAt.toISOString(),
      drives: (d.driveIds ?? [])
        .map((id) => names.get(id))
        .filter((n): n is string => Boolean(n))
        .sort(),
    })),
  };
}

export async function counts(userId: string) {
  const [drives, devices, accounts] = await Promise.all([
    mounts().countDocuments({ userId }),
    listDevices(userId).then((rows) => rows.length),
    services().countDocuments({ userId }),
  ]);
  return { drives, devices, accounts };
}

// never the wraps: the dashboard needs no secrets, sealed or not
export async function security(userId: string) {
  const [vault, accounts, legacy] = await Promise.all([
    vaults().findOne(
      { _id: userId },
      { projection: { keyId: 1, createdAt: 1, passwordChangedAt: 1 } }
    ),
    services().countDocuments({ userId }),
    services().countDocuments({
      userId,
      $or: [{ options: { $exists: true } }, { secrets: { $exists: true } }],
    }),
  ]);
  return {
    vault: vault
      ? {
          keyId: vault.keyId,
          since: vault.createdAt.toISOString(),
          passwordChangedAt: vault.passwordChangedAt.toISOString(),
        }
      : null,
    accounts,
    legacy,
  };
}

export type RenameResult = "ok" | "not_found" | "name_taken" | "bad_name";

export async function renameDrive(
  userId: string,
  id: string,
  raw: unknown,
  ip: string
): Promise<RenameResult> {
  const parsed = mountBody.shape.name.safeParse(raw);
  if (!parsed.success) {
    return "bad_name";
  }
  const name = parsed.data;
  const doc = await mounts().findOne({ _id: id, userId });
  if (!doc) {
    return "not_found";
  }
  if (doc.name === name) {
    return "ok";
  }
  try {
    const { matchedCount } = await mounts().updateOne(
      { _id: id, userId },
      {
        $set: { name, updatedAt: new Date(), updatedBy: "web" },
        $inc: { version: 1 },
      }
    );
    if (matchedCount === 0) {
      return "not_found";
    }
  } catch (error) {
    if (error instanceof MongoServerError && error.code === 11_000) {
      return "name_taken";
    }
    throw error;
  }
  await bump(userId);
  await audit(userId, "", "mount.put", id, ip);
  await record(userId, [
    {
      kind: "drive.renamed",
      deviceId: "",
      target: id,
      name,
      ok: true,
      reason: `Was ${doc.name}`,
      at: new Date(),
    },
  ]);
  return "ok";
}

export async function removeDrive(
  userId: string,
  id: string,
  ip: string
): Promise<boolean> {
  const doc = await mounts().findOneAndDelete({ _id: id, userId });
  if (!doc) {
    return false;
  }
  await bump(userId);
  await audit(userId, "", "mount.delete", id, ip);
  await record(userId, [
    {
      kind: "drive.removed",
      deviceId: "",
      target: id,
      name: doc.name,
      ok: true,
      reason: "",
      at: new Date(),
    },
  ]);
  return true;
}

// every device finishes its uploads and erases itself; their records stay until they report it
export async function deleteAccount(userId: string, ip: string) {
  for (const d of await listDevices(userId)) {
    // biome-ignore lint/performance/noAwaitInLoops: few devices, each audited
    await removeDevice(userId, String(d._id), "remove", "", ip);
  }
  const uid = new ObjectId(userId);
  await Promise.all([
    services().deleteMany({ userId }),
    mounts().deleteMany({ userId }),
    vaults().deleteOne({ _id: userId }),
    events().deleteMany({ userId }),
    db().collection("audit").deleteMany({ userId }),
    db().collection("cliLogins").deleteMany({ userId }),
    db().collection("accounts").deleteMany({ userId: uid }),
    users().deleteOne({ _id: uid }),
  ]);
  // last, so a failed step above leaves the user signed in to try again
  await db().collection("sessions").deleteMany({ userId: uid });
}
