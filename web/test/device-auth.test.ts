import { afterAll, beforeAll, expect, test } from "bun:test";
import { createHash, randomBytes } from "node:crypto";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, ensureIndexes, mongo } from "@/lib/db";
import { authDevice } from "@/lib/device-auth";

let server: MongoMemoryServer;

beforeAll(async () => {
  (globalThis as { mongo?: unknown }).mongo = undefined;
  server = await MongoMemoryServer.create();
  process.env.MONGODB_URI = server.getUri();
  await ensureIndexes();
}, 120_000);

afterAll(async () => {
  await mongo().close();
  await server.stop();
});

const t0 = new Date("2026-10-31T10:00:00Z");
const minutes = (n: number) => new Date(t0.getTime() + n * 60_000);

async function seed(fields: Record<string, unknown> = {}) {
  const token = `dky_${randomBytes(32).toString("base64url")}`;
  const { insertedId } = await db()
    .collection("devices")
    .insertOne({
      userId: "user-1",
      name: "mac",
      os: "darwin",
      arch: "arm64",
      version: "0.1.0",
      tokenHash: createHash("sha256").update(token).digest("hex"),
      createdAt: t0,
      lastSeenAt: t0,
      revokedAt: null,
      ...fields,
    });
  return { token, deviceId: String(insertedId) };
}

const request = (headers: Record<string, string>) =>
  new Request("http://localhost/api/v1/sync", { headers });

const bearer = (token: string, version = "0.1.0") =>
  request({ authorization: `Bearer ${token}`, "x-dokky-version": version });

async function errorOf(result: unknown) {
  expect(result).toBeInstanceOf(Response);
  const res = result as Response;
  return { status: res.status, body: await res.json() };
}

test("a valid token returns the device and its user", async () => {
  const { token, deviceId } = await seed();
  expect(await authDevice(bearer(token), minutes(1))).toEqual({
    userId: "user-1",
    deviceId,
  });
});

test("missing or malformed bearer is unauthorized", async () => {
  const { token } = await seed();
  for (const authorization of [
    undefined,
    "",
    token,
    `Basic ${token}`,
    `Bearer ${token.slice(0, -1)}`,
    `Bearer ${token}x`,
    `Bearer ${token.replace("dky_", "dkx_")}`,
  ]) {
    const headers: Record<string, string> =
      authorization === undefined ? {} : { authorization };
    // biome-ignore lint/performance/noAwaitInLoops: one case at a time
    expect(await errorOf(await authDevice(request(headers)))).toEqual({
      status: 401,
      body: { error: { code: "unauthorized", message: expect.any(String) } },
    });
  }
});

test("an unknown token is unauthorized", async () => {
  const unknown = `dky_${randomBytes(32).toString("base64url")}`;
  const { status, body } = await errorOf(await authDevice(bearer(unknown)));
  expect(status).toBe(401);
  expect(body.error.code).toBe("unauthorized");
});

test("a revoked device gets device_revoked", async () => {
  const { token } = await seed({ revokedAt: minutes(1) });
  const { status, body } = await errorOf(
    await authDevice(bearer(token), minutes(2))
  );
  expect(status).toBe(401);
  expect(body.error.code).toBe("device_revoked");
});

test("an old or missing version is never refused", async () => {
  const { token } = await seed();
  for (const version of ["0.0.1", "", "garbage"]) {
    // biome-ignore lint/performance/noAwaitInLoops: one case at a time
    const result = await authDevice(bearer(token, version), minutes(1));
    expect(result).not.toBeInstanceOf(Response);
  }
  const noHeader = request({ authorization: `Bearer ${token}` });
  expect(await authDevice(noHeader)).not.toBeInstanceOf(Response);
});

test("lastSeenAt is written at most once per 5 minutes", async () => {
  const { token, deviceId } = await seed();
  const lastSeen = async () =>
    (
      await db()
        .collection("devices")
        .findOne({
          tokenHash: createHash("sha256").update(token).digest("hex"),
        })
    )?.lastSeenAt;

  await authDevice(bearer(token), minutes(1));
  await authDevice(bearer(token), minutes(4));
  expect(await lastSeen()).toEqual(t0);

  expect(await authDevice(bearer(token), minutes(5))).toEqual({
    userId: "user-1",
    deviceId,
  });
  expect(await lastSeen()).toEqual(minutes(5));

  await authDevice(bearer(token), minutes(9));
  expect(await lastSeen()).toEqual(minutes(5));

  await authDevice(bearer(token), minutes(11));
  expect(await lastSeen()).toEqual(minutes(11));
});
