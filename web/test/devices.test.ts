import { afterAll, beforeAll, expect, mock, test } from "bun:test";
import { createHash, randomBytes } from "node:crypto";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, ensureIndexes, mongo } from "@/lib/db";

let session: { user: { id: string } } | null = null;
mock.module("@/auth", () => ({
  auth: () => Promise.resolve(session),
  signIn: () => Promise.resolve(),
}));
mock.module("next/cache", () => ({ revalidatePath: () => undefined }));

const list = await import("@/app/api/v1/devices/route");
const one = await import("@/app/api/v1/devices/[id]/route");
const { remove } = await import("@/app/devices/actions");
const { authDevice } = await import("@/lib/device-auth");

let server: MongoMemoryServer;

beforeAll(async () => {
  (globalThis as { mongo?: unknown }).mongo = undefined;
  server = await MongoMemoryServer.create();
  process.env.DATABASE_URL = server.getUri();
  await ensureIndexes();
}, 120_000);

afterAll(async () => {
  await mongo().close();
  await server.stop();
});

async function seed(userId: string, name: string) {
  const token = `dky_${randomBytes(32).toString("base64url")}`;
  const { insertedId } = await db()
    .collection("devices")
    .insertOne({
      userId,
      name,
      os: "darwin",
      arch: "arm64",
      version: "0.1.0",
      tokenHash: createHash("sha256").update(token).digest("hex"),
      createdAt: new Date(),
      lastSeenAt: new Date(),
      revokedAt: null,
    });
  return { token, id: String(insertedId) };
}

const call = (token: string, version = "0.1.0") =>
  new Request("http://localhost/api/v1/devices", {
    headers: { authorization: `Bearer ${token}`, "x-dokky-version": version },
  });

const del = (token: string, id: string) =>
  one.DELETE(
    new Request(`http://localhost/api/v1/devices/${id}`, {
      method: "DELETE",
      headers: { authorization: `Bearer ${token}`, "x-dokky-version": "0.1.0" },
    }),
    { params: Promise.resolve({ id }) }
  );

test("list marks the calling device as current", async () => {
  const a = await seed("u-list", "a");
  const b = await seed("u-list", "b");
  await seed("u-other", "x");
  const rows = await (await list.GET(call(a.token))).json();
  expect(rows).toHaveLength(2);
  const byId = Object.fromEntries(rows.map((r: { id: string }) => [r.id, r]));
  expect(byId[a.id].current).toBe(true);
  expect(byId[b.id].current).toBe(false);
  expect(Object.keys(byId[a.id]).sort()).toEqual(
    [
      "arch",
      "createdAt",
      "current",
      "drives",
      "id",
      "lastSeenAt",
      "mounted",
      "name",
      "os",
      "osName",
      "version",
    ].sort()
  );
});

test("revoking another device makes its next call device_revoked", async () => {
  const a = await seed("u-rev", "a");
  const b = await seed("u-rev", "b");
  expect((await del(a.token, b.id)).status).toBe(204);
  expect((await del(a.token, b.id)).status).toBe(204);
  const res = await list.GET(call(b.token));
  expect(res.status).toBe(401);
  expect((await res.json()).error.code).toBe("device_revoked");
  const rows = await (await list.GET(call(a.token))).json();
  expect(rows.map((r: { id: string }) => r.id)).toEqual([a.id]);
  const logs = await db()
    .collection("audit")
    .find({ userId: "u-rev", action: "device.revoke" })
    .toArray();
  expect(logs).toHaveLength(1);
  expect(logs[0]?.deviceId).toBe(a.id);
});

test("self revokes the caller", async () => {
  const a = await seed("u-self", "a");
  expect((await del(a.token, "self")).status).toBe(204);
  const res = await list.GET(call(a.token));
  expect(res.status).toBe(401);
  expect((await res.json()).error.code).toBe("device_revoked");
});

test("another user's device is not found", async () => {
  const a = await seed("u-mine", "a");
  const other = await seed("u-theirs", "x");
  for (const id of [other.id, "nope"]) {
    // biome-ignore lint/performance/noAwaitInLoops: one case at a time
    const res = await del(a.token, id);
    expect(res.status).toBe(404);
    expect((await res.json()).error.code).toBe("not_found");
  }
  expect((await list.GET(call(other.token))).status).toBe(200);
});

test("the page action revokes only the session user's device", async () => {
  const mine = await seed("u-page", "a");
  const theirs = await seed("u-page-other", "x");
  session = { user: { id: "u-page" } };
  const form = (id: string) => {
    const f = new FormData();
    f.set("id", id);
    return f;
  };
  await remove(form(theirs.id));
  expect((await list.GET(call(theirs.token))).status).toBe(200);
  await remove(form(mine.id));
  expect((await list.GET(call(mine.token))).status).toBe(401);
});

test("authDevice keeps the stored version current", async () => {
  const a = await seed("u-ver", "a");
  await authDevice(call(a.token, "0.2.0"));
  const row = await db().collection("devices").findOne({ userId: "u-ver" });
  expect(row?.version).toBe("0.2.0");
});
