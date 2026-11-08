import { type Filter, MongoBulkWriteError, ObjectId } from "mongodb";
import { db } from "@/lib/db";

export interface EventIn {
  at: Date;
  clientId?: string;
  deviceId: string;
  key?: string;
  kind: string;
  name: string;
  ok: boolean;
  reason: string;
  target: string;
}

interface EventDoc extends EventIn {
  _id?: ObjectId;
  deviceName: string;
  open?: boolean;
  userId: string;
}

export const events = () => db().collection<EventDoc>("events");

async function deviceNames(
  userId: string,
  ids: string[]
): Promise<Map<string, string>> {
  const valid = [...new Set(ids)].filter((id) => ObjectId.isValid(id));
  const rows = await db()
    .collection<{ _id: ObjectId; name: string }>("devices")
    .find(
      { _id: { $in: valid.map((id) => new ObjectId(id)) }, userId },
      { projection: { name: 1 } }
    )
    .toArray();
  return new Map(rows.map((r) => [String(r._id), r.name]));
}

// an entry with a key is kept once until its pair closes it, e.g. a sign-in expired until it is fixed
async function keyed(
  userId: string,
  e: EventIn,
  seen: Set<string>
): Promise<boolean> {
  if (e.kind === "signin.fixed") {
    await events().updateMany(
      { userId, key: `signin.expired:${e.target}`, open: true },
      { $set: { open: false } }
    );
    seen.delete(`signin.expired:${e.target}`);
  }
  if (!e.key) {
    return true;
  }
  if (seen.has(e.key)) {
    return false;
  }
  const open = await events().countDocuments(
    { userId, key: e.key, open: true },
    { limit: 1 }
  );
  if (open) {
    return false;
  }
  seen.add(e.key);
  return true;
}

// a resend after a lost reply carries the same ids; those are already saved
async function insertAll(docs: EventDoc[]): Promise<void> {
  try {
    await events().insertMany(docs, { ordered: false });
  } catch (error) {
    const dupOnly =
      error instanceof MongoBulkWriteError &&
      Object.values(error.writeErrors).every((w) => w.code === 11_000);
    if (!dupOnly) {
      throw error;
    }
  }
}

export async function record(
  userId: string,
  entries: EventIn[],
  now = new Date()
): Promise<void> {
  if (entries.length === 0) {
    return;
  }
  const names = await deviceNames(
    userId,
    entries.map((e) => e.deviceId)
  );
  await closeRecovered(userId, entries);
  const seen = new Set<string>();
  const docs: EventDoc[] = [];
  for (const e of entries) {
    // biome-ignore lint/performance/noAwaitInLoops: entries are applied in order
    if (!(await keyed(userId, e, seen))) {
      continue;
    }
    docs.push({
      ...e,
      userId,
      at: e.at > now ? now : e.at,
      deviceName: names.get(e.deviceId) ?? (e.deviceId ? "" : "Web"),
      ...(e.key ? { open: true } : {}),
    });
  }
  if (docs.length > 0) {
    await insertAll(docs);
  }
}

// a drive that mounts again shows its sign-in works, so an expired note for it is over
async function closeRecovered(userId: string, entries: EventIn[]) {
  const ids = entries
    .filter((e) => e.kind === "drive.mounted" || e.kind === "drive.recovered")
    .map((e) => e.target);
  if (ids.length === 0) {
    return;
  }
  const rows = await db()
    .collection<{ _id: string; serviceId: string }>("mounts")
    .find({ _id: { $in: ids }, userId }, { projection: { serviceId: 1 } })
    .toArray();
  if (rows.length === 0) {
    return;
  }
  await events().updateMany(
    {
      userId,
      key: { $in: rows.map((r) => `signin.expired:${r.serviceId}`) },
      open: true,
    },
    { $set: { open: false } }
  );
}

export async function listEvents(
  userId: string,
  before: Date | null,
  limit: number
) {
  const q: Filter<EventDoc> = { userId };
  if (before) {
    q.at = { $lt: before };
  }
  const rows = await events()
    .find(q)
    .sort({ at: -1, _id: -1 })
    .limit(limit)
    .toArray();
  return rows.map((d) => ({
    id: String(d._id),
    kind: d.kind,
    device: d.deviceName,
    target: d.target,
    name: d.name,
    reason: d.reason,
    ok: d.ok,
    open: d.open === true,
    at: d.at.toISOString(),
  }));
}
