import { afterAll, beforeAll, expect, mock, test } from "bun:test";
import { createHash } from "node:crypto";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, ensureIndexes, mongo } from "@/lib/db";

let session: { user: { id: string } } | null = null;
mock.module("@/auth", () => ({
  auth: () => Promise.resolve(session),
  signIn: () => Promise.resolve(),
}));

const { approve, deny } = await import("@/app/cli/approve/actions");
const start = await import("@/app/api/v1/cli/login/start/route");
const poll = await import("@/app/api/v1/cli/login/poll/route");
const { pollLogin, startLogin } = await import("@/lib/device-auth");
const { clientIp } = await import("@/lib/audit");
const { mountBody, serviceBody } = await import("@/lib/schemas");

const reUserCode = /^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$/u;
const reDeviceCode = /^[\w-]{43}$/u;
const reToken = /^dky_[\w-]{43}$/u;

const sha256 = (s: string) => createHash("sha256").update(s).digest("hex");

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

const post = (
  route: { POST: (req: Request) => Promise<Response> },
  body: unknown,
  headers: Record<string, string> = {}
) =>
  route.POST(
    new Request("http://localhost/api/v1/cli/login", {
      method: "POST",
      body: typeof body === "string" ? body : JSON.stringify(body),
      headers: { "content-type": "application/json", ...headers },
    })
  );

const device = (deviceName: string) => ({
  deviceName,
  os: "darwin",
  arch: "arm64",
  version: "0.1.0",
});

async function begin(deviceName: string) {
  const res = await post(start, device(deviceName));
  expect(res.status).toBe(200);
  return (await res.json()) as { deviceCode: string; userCode: string };
}

const pollStatus = async (deviceCode: string, headers = {}) =>
  (await (await post(poll, { deviceCode }, headers)).json()) as {
    status: string;
    token?: string;
    deviceId?: string;
  };

const form = (code: string) => {
  const data = new FormData();
  data.set("code", code);
  return data;
};

async function redirectOf(action: Promise<unknown>): Promise<string> {
  try {
    await action;
  } catch (error) {
    const digest = (error as { digest?: string }).digest ?? "";
    if (digest.startsWith("NEXT_REDIRECT;")) {
      return digest.split(";")[2] ?? "";
    }
    throw error;
  }
  throw new Error("action did not redirect");
}

test("full flow: start, pending, approve, approved with a token, then expired", async () => {
  const res = await post(start, device("flow-mac"));
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body).toEqual({
    deviceCode: expect.stringMatching(reDeviceCode),
    userCode: expect.stringMatching(reUserCode),
    verifyUrl: `https://dokky.ritik.me/cli/approve?code=${body.userCode}`,
    interval: 2,
    expiresIn: 600,
  });

  const row = await db()
    .collection("cliLogins")
    .findOne({ userCode: body.userCode });
  expect(row?.deviceCodeHash).toBe(sha256(body.deviceCode));
  expect(JSON.stringify(row)).not.toContain(body.deviceCode);

  expect(await pollStatus(body.deviceCode)).toEqual({ status: "pending" });

  session = { user: { id: "user-1" } };
  expect(await redirectOf(approve(form(body.userCode)))).toBe(
    "/cli/approve?done=approved"
  );

  const approved = await pollStatus(body.deviceCode, {
    "x-forwarded-for": "203.0.113.7, 10.0.0.1",
  });
  expect(approved).toEqual({
    status: "approved",
    token: expect.stringMatching(reToken),
    deviceId: expect.any(String),
  });

  const saved = await db()
    .collection("devices")
    .findOne({ tokenHash: sha256(approved.token ?? "") });
  expect(String(saved?._id)).toBe(approved.deviceId ?? "");
  expect(saved).toMatchObject({
    userId: "user-1",
    name: "flow-mac",
    os: "darwin",
    arch: "arm64",
    version: "0.1.0",
    revokedAt: null,
  });
  expect(saved?.createdAt).toBeInstanceOf(Date);
  expect(saved?.lastSeenAt).toBeInstanceOf(Date);
  expect(JSON.stringify(saved)).not.toContain(approved.token);

  const entry = await db()
    .collection("audit")
    .findOne({ deviceId: approved.deviceId });
  expect(entry).toMatchObject({
    userId: "user-1",
    action: "login",
    target: "flow-mac",
    ip: "203.0.113.7",
  });
  expect(entry?.at).toBeInstanceOf(Date);

  expect(await pollStatus(body.deviceCode)).toEqual({ status: "expired" });
  expect(
    await db()
      .collection("cliLogins")
      .countDocuments({ userCode: body.userCode })
  ).toBe(0);
});

test("concurrent polls of an approved login mint exactly one device", async () => {
  const { deviceCode, userCode } = await begin("race-mac");
  session = { user: { id: "user-1" } };
  await redirectOf(approve(form(userCode)));
  const results = await Promise.all([
    pollStatus(deviceCode),
    pollStatus(deviceCode),
  ]);
  expect(results.filter((r) => r.status === "approved")).toHaveLength(1);
  expect(results.filter((r) => r.status === "expired")).toHaveLength(1);
  expect(
    await db().collection("devices").countDocuments({ name: "race-mac" })
  ).toBe(1);
});

test("deny", async () => {
  const { deviceCode, userCode } = await begin("deny-mac");
  session = { user: { id: "user-1" } };
  expect(await redirectOf(deny(form(userCode)))).toBe(
    "/cli/approve?done=denied"
  );
  expect(await pollStatus(deviceCode)).toEqual({ status: "denied" });
  expect(await pollStatus(deviceCode)).toEqual({ status: "denied" });
  expect(
    await db().collection("devices").countDocuments({ name: "deny-mac" })
  ).toBe(0);
});

test("approve and deny need a session", async () => {
  const { deviceCode, userCode } = await begin("nosession-mac");
  session = null;
  expect(await redirectOf(approve(form(userCode)))).toBe("/");
  expect(await redirectOf(deny(form(userCode)))).toBe("/");
  expect(await pollStatus(deviceCode)).toEqual({ status: "pending" });
});

test("a user code is single-use", async () => {
  const { deviceCode, userCode } = await begin("once-mac");
  session = { user: { id: "user-1" } };
  expect(await redirectOf(approve(form(userCode)))).toBe(
    "/cli/approve?done=approved"
  );
  session = { user: { id: "user-2" } };
  expect(await redirectOf(approve(form(userCode)))).toBe(
    "/cli/approve?done=invalid"
  );
  expect(await redirectOf(deny(form(userCode)))).toBe(
    "/cli/approve?done=invalid"
  );

  const approved = await pollStatus(deviceCode);
  expect(approved.status).toBe("approved");
  const saved = await db()
    .collection("devices")
    .findOne({ tokenHash: sha256(approved.token ?? "") });
  expect(saved?.userId).toBe("user-1");
});

test("approving an expired code fails cleanly", async () => {
  const { userCode } = await begin("late-mac");
  await db()
    .collection("cliLogins")
    .updateOne({ userCode }, { $set: { expiresAt: new Date(Date.now() - 1) } });
  session = { user: { id: "user-1" } };
  expect(await redirectOf(approve(form(userCode)))).toBe(
    "/cli/approve?done=invalid"
  );
  const row = await db().collection("cliLogins").findOne({ userCode });
  expect(row?.status).toBe("pending");
  expect(row?.userId).toBeUndefined();
});

test("malformed user codes never match a login", async () => {
  const { deviceCode } = await begin("inject-mac");
  session = { user: { id: "attacker" } };
  for (const code of ["BCDF-GHJ", "bcdf-ghjk", "", "BCDF-GHJK|"]) {
    // biome-ignore lint/performance/noAwaitInLoops: one action at a time
    expect(await redirectOf(approve(form(code)))).toBe(
      "/cli/approve?done=invalid"
    );
  }
  const crafted = { get: () => ({ $ne: null }) } as unknown as FormData;
  expect(await redirectOf(approve(crafted))).toBe("/cli/approve?done=invalid");
  expect(await redirectOf(approve(new FormData()))).toBe(
    "/cli/approve?done=invalid"
  );
  expect(await pollStatus(deviceCode)).toEqual({ status: "pending" });
});

test("expiry after 10 minutes (fake clock)", async () => {
  const t0 = new Date();
  const at = (seconds: number) => new Date(t0.getTime() + seconds * 1000);
  const { deviceCode } = await startLogin(device("clock-mac"), t0);
  expect(await pollLogin(deviceCode, "", at(599))).toEqual({
    status: "pending",
  });
  expect(await pollLogin(deviceCode, "", at(600))).toEqual({
    status: "expired",
  });
});

test("bodies are validated and capped at 64 KB", async () => {
  const bad = await post(start, { ...device("x"), os: "Mac OS" });
  expect(bad.status).toBe(400);
  expect(await bad.json()).toEqual({
    error: { code: "bad_request", message: expect.any(String) },
  });

  expect((await post(start, "{nope")).status).toBe(400);
  expect((await post(poll, { deviceCode: "short" })).status).toBe(400);

  const huge = await post(start, {
    ...device("x"),
    pad: "a".repeat(64 * 1024),
  });
  expect(huge.status).toBe(413);
  expect((await huge.json()).error.code).toBe("body_too_large");

  expect(await pollStatus("A".repeat(43))).toEqual({ status: "expired" });
});

test("client IP comes from the first forwarded hop, then x-real-ip", () => {
  const ip = (headers: Record<string, string>) =>
    clientIp(new Request("http://localhost/", { headers }));
  expect(ip({ "x-forwarded-for": " 198.51.100.4 , 10.0.0.1" })).toBe(
    "198.51.100.4"
  );
  expect(ip({ "x-real-ip": "198.51.100.9" })).toBe("198.51.100.9");
  expect(ip({})).toBe("");
});

test("service and mount schemas accept what the device sends", () => {
  const id = "0b7e2f4c-5d1a-4c3b-9e8f-1a2b3c4d5e6f";
  const vfs = {
    cacheMode: "full",
    dirCacheTime: "30s",
    readAhead: "2G",
    readChunkStreams: 4,
    cacheMaxSize: "50G",
    cacheMaxAge: "5m",
    cachePollInterval: "10s",
    cacheMinFreeSpace: "10G",
    fastFingerprint: true,
  };
  const mount = {
    name: "nas-media",
    serviceId: id,
    remotePath: "media/Films 2026",
    vfs,
    baseVersion: 0,
  };
  expect(mountBody.safeParse(mount).success).toBe(true);
  expect(mountBody.safeParse({ ...mount, name: "Nas" }).success).toBe(false);
  expect(mountBody.safeParse({ ...mount, name: "a".repeat(41) }).success).toBe(
    false
  );
  expect(mountBody.safeParse({ ...mount, serviceId: `${id}|x` }).success).toBe(
    false
  );
  expect(
    mountBody.safeParse({ ...mount, vfs: { ...vfs, cacheMaxAge: "2h" } })
      .success
  ).toBe(false);

  const service = {
    name: "Home NAS",
    type: "smb",
    options: { host: "nas.local", user: "ritik" },
    secrets: { pass: "hunter2" },
    baseVersion: 3,
  };
  expect(serviceBody.safeParse(service).success).toBe(true);
  expect(
    serviceBody.safeParse({ ...service, secrets: { "pass|x": "a" } }).success
  ).toBe(false);
  expect(
    serviceBody.safeParse({ ...service, options: { host: "a".repeat(4097) } })
      .success
  ).toBe(false);
  expect(serviceBody.safeParse({ ...service, type: "S3" }).success).toBe(false);
  for (const type of ["google photos", "google cloud storage"]) {
    expect(serviceBody.safeParse({ ...service, type }).success).toBe(true);
  }
  for (const type of [" smb", "smb ", "google  photos"]) {
    expect(serviceBody.safeParse({ ...service, type }).success).toBe(false);
  }
  const token = (n: number) => ({
    ...service,
    secrets: { token: "t".repeat(n) },
  });
  expect(serviceBody.safeParse(token(16_384)).success).toBe(true);
  expect(serviceBody.safeParse(token(16_385)).success).toBe(false);
  expect(serviceBody.safeParse({ ...service, name: "" }).success).toBe(false);
});
