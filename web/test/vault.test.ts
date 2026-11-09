import { afterAll, beforeAll, expect, mock, test } from "bun:test";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { sleep } from "bun";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, ensureIndexes, mongo } from "@/lib/db";

let session: { user: { id: string } } | null = null;
mock.module("@/auth", () => ({
  auth: () => Promise.resolve(session),
  signIn: () => Promise.resolve(),
}));

const vaultRoute = await import("@/app/api/v1/vault/route");
const serviceRoute = await import("@/app/api/v1/services/[id]/route");
const tokenRoute = await import("@/app/api/v1/services/[id]/token/route");
const mountRoute = await import("@/app/api/v1/mounts/[id]/route");
const syncRoute = await import("@/app/api/v1/sync/route");
const securityRoute = await import("@/app/api/dash/security/route");
const { deleteAccount } = await import("@/lib/dash");
const { sealedServiceBody, sealedTokenBody, vaultBody } = await import(
  "@/lib/schemas"
);
const { aadFor, decrypt } = await import("@/lib/secrets");

let server: MongoMemoryServer;

beforeAll(async () => {
  (globalThis as { mongo?: unknown }).mongo = undefined;
  server = await MongoMemoryServer.create();
  process.env.DATABASE_URL = server.getUri();
  process.env.ENCRYPTION_KEY = randomBytes(32).toString("base64");
  await ensureIndexes();
}, 120_000);

afterAll(async () => {
  await mongo().close();
  await server.stop();
});

interface User {
  deviceId: string;
  token: string;
  userId: string;
}

async function newDevice(userId: string): Promise<User> {
  const token = `dky_${randomBytes(32).toString("base64url")}`;
  const now = new Date();
  const { insertedId } = await db()
    .collection("devices")
    .insertOne({
      userId,
      name: "mac",
      os: "darwin",
      arch: "arm64",
      version: "0.2.0",
      tokenHash: createHash("sha256").update(token).digest("hex"),
      createdAt: now,
      lastSeenAt: now,
      revokedAt: null,
    });
  return { deviceId: String(insertedId), token, userId };
}

async function newUser(): Promise<User> {
  const { insertedId } = await db()
    .collection("users")
    .insertOne({ name: "someone" });
  return await newDevice(String(insertedId));
}

const VAULT = { "x-dokky-vault": "1" };
const PADDING = /[=]+$/u;

function request(
  method: string,
  user: User,
  body?: unknown,
  headers: Record<string, string> = {}
) {
  return new Request("http://localhost/api/v1/x", {
    method,
    headers: {
      "content-type": "application/json",
      "x-dokky-version": "0.2.0",
      authorization: `Bearer ${user.token}`,
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

type Handler = (
  req: Request,
  ctx: { params: Promise<{ id: string }> }
) => Promise<Response>;

const onId = (
  handler: Handler,
  method: string,
  user: User,
  id: string,
  body?: unknown,
  headers: Record<string, string> = {}
) =>
  handler(request(method, user, body, headers), {
    params: Promise.resolve({ id }),
  });

const envelope = (bytes = 73) =>
  Buffer.concat([Buffer.from([1]), randomBytes(bytes - 1)]).toString("base64");

const kdf = (fields: Record<string, unknown> = {}) => ({
  alg: "argon2id",
  salt: randomBytes(32).toString("base64"),
  t: 3,
  m: 65_536,
  p: 4,
  ...fields,
});

const newVault = (fields: Record<string, unknown> = {}) => ({
  keyId: randomBytes(8).toString("hex"),
  kdf: kdf(),
  wrappedKey: envelope(),
  recoveryWrappedKey: envelope(),
  ...fields,
});

const getVault = (user: User) => vaultRoute.GET(request("GET", user));
const postVault = (user: User, body: unknown) =>
  vaultRoute.POST(request("POST", user, body, VAULT));
const putVault = (user: User, body: unknown) =>
  vaultRoute.PUT(request("PUT", user, body, VAULT));

const sync = (user: User, headers: Record<string, string> = {}) =>
  syncRoute.GET(request("GET", user, undefined, headers));

const legacyService = (fields: Record<string, unknown> = {}) => ({
  name: "Home NAS",
  type: "smb",
  options: { host: "nas.local", user: "ritik" },
  secrets: { pass: "hunter2" },
  baseVersion: 0,
  ...fields,
});

const sealedService = (
  keyId: string,
  fields: Record<string, unknown> = {}
) => ({
  name: "Home NAS",
  type: "smb",
  keyId,
  sealed: envelope(200),
  sealedToken: null,
  tokenExpiry: null,
  baseVersion: 0,
  ...fields,
});

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

async function errorOf(res: Response) {
  return { status: res.status, body: await res.json() };
}

const isoTime = expect.stringMatching(/^\d{4}-\d\d-\d\dT[\d:.]+Z$/u);

async function withVault(user: User) {
  const body = newVault();
  const res = await postVault(user, body);
  expect(res.status).toBe(201);
  return body;
}

test("schemas hold the kdf caps, envelope sizes and keyId format", () => {
  const ok = (fields: Record<string, unknown>) =>
    vaultBody.safeParse(newVault(fields)).success;
  expect(ok({})).toBe(true);
  for (const caps of [
    { t: 1 },
    { t: 10 },
    { m: 19_456 },
    { m: 262_144 },
    { p: 1 },
    { p: 16 },
  ]) {
    expect(ok({ kdf: kdf(caps) })).toBe(true);
  }
  for (const caps of [
    { t: 0 },
    { t: 11 },
    { t: 2.5 },
    { m: 19_455 },
    { m: 262_145 },
    { p: 0 },
    { p: 17 },
    { alg: "argon2i" },
    { salt: randomBytes(31).toString("base64") },
    { salt: randomBytes(33).toString("base64") },
    { salt: randomBytes(32).toString("hex") },
  ]) {
    expect(ok({ kdf: kdf(caps) })).toBe(false);
  }
  expect(ok({ wrappedKey: envelope(41) })).toBe(true);
  expect(ok({ wrappedKey: envelope(512) })).toBe(true);
  expect(ok({ wrappedKey: envelope(40) })).toBe(false);
  expect(ok({ wrappedKey: envelope(513) })).toBe(false);
  expect(
    ok({ wrappedKey: Buffer.alloc(73, 2).toString("base64") }) // wrong format byte
  ).toBe(false);
  expect(ok({ recoveryWrappedKey: "not base64!" })).toBe(false);
  // 73 bytes always pads with ==, which Go's StdEncoding sends
  expect(ok({ recoveryWrappedKey: envelope(73).replace(PADDING, "") })).toBe(
    false
  );
  for (const keyId of [
    "",
    "0123456789abcde",
    "0123456789ABCDEF",
    "z".repeat(16),
  ]) {
    expect(ok({ keyId })).toBe(false);
  }

  const id = "0123456789abcdef";
  const service = (fields: Record<string, unknown>) =>
    sealedServiceBody.safeParse(sealedService(id, fields));
  expect(service({}).success).toBe(true);
  const omitted = sealedServiceBody.safeParse({
    name: "x",
    type: "smb",
    keyId: id,
    sealed: envelope(),
    baseVersion: 0,
  });
  expect(omitted.data).toMatchObject({ sealedToken: null, tokenExpiry: null });
  expect(
    service({ sealedToken: envelope(), tokenExpiry: "2026-11-01T00:00:00Z" })
      .success
  ).toBe(true);
  expect(service({ tokenExpiry: "2026-11-01T00:00:00Z" }).success).toBe(false);
  expect(service({ sealed: envelope(32_810) }).success).toBe(false);
  expect(service({ sealed: null }).success).toBe(false);
  expect(
    sealedTokenBody.safeParse({
      keyId: id,
      sealedToken: null,
      expiry: "2026-11-01T00:00:00Z",
    }).success
  ).toBe(false);
});

test("create, read and rewrap the vault, with wraps encrypted at rest", async () => {
  const user = await newUser();
  expect(await errorOf(await getVault(user))).toEqual({
    status: 404,
    body: { error: { code: "no_vault", message: expect.any(String) } },
  });
  expect((await putVault(user, { ...newVault(), baseVersion: 1 })).status).toBe(
    404
  );

  const body = newVault();
  const created = await postVault(user, body);
  expect(created.status).toBe(201);
  const vault = {
    version: 1,
    keyId: body.keyId,
    kdf: body.kdf,
    wrappedKey: body.wrappedKey,
    recoveryWrappedKey: body.recoveryWrappedKey,
    createdAt: isoTime,
    passwordChangedAt: isoTime,
  };
  const createdJson = await created.json();
  expect(createdJson).toEqual({ vault });
  const read = await getVault(user);
  expect(read.headers.get("cache-control")).toBe("no-store");
  expect(await read.json()).toEqual({ vault });

  const raw = await db()
    .collection("vaults")
    .findOne({ _id: user.userId as never });
  expect(raw).toMatchObject({ createdBy: user.deviceId, version: 1 });
  expect(Object.keys(raw?.wrappedKey).sort()).toEqual(["c", "n"]);
  expect(JSON.stringify(raw)).not.toContain(body.wrappedKey);
  expect(JSON.stringify(raw)).not.toContain(body.recoveryWrappedKey);
  expect(decrypt(raw?.wrappedKey, `${user.userId}|vault|wrappedKey`)).toBe(
    body.wrappedKey
  );
  expect(
    decrypt(raw?.recoveryWrappedKey, `${user.userId}|vault|recoveryWrappedKey`)
  ).toBe(body.recoveryWrappedKey);

  const again = await postVault(user, newVault());
  expect(await errorOf(again)).toEqual({
    status: 409,
    body: {
      error: { code: "vault_exists", message: expect.any(String) },
      current: vault,
    },
  });

  const mismatch = await putVault(user, { ...newVault(), baseVersion: 1 });
  expect(await errorOf(mismatch)).toEqual({
    status: 409,
    body: { error: { code: "key_mismatch", message: expect.any(String) } },
  });

  const { passwordChangedAt } = (await (await getVault(user)).json()).vault;
  const recovery = envelope();
  const newRecovery = await putVault(user, {
    ...body,
    recoveryWrappedKey: recovery,
    baseVersion: 1,
  });
  expect(newRecovery.status).toBe(200);
  const afterRecovery = (await newRecovery.json()).vault;
  expect(afterRecovery).toMatchObject({
    version: 2,
    recoveryWrappedKey: recovery,
    passwordChangedAt,
  });

  const stale = await putVault(user, { ...body, baseVersion: 1 });
  expect(await errorOf(stale)).toEqual({
    status: 409,
    body: {
      error: { code: "conflict", message: expect.any(String) },
      current: afterRecovery,
    },
  });

  await sleep(5);
  const password = { ...body, kdf: kdf(), wrappedKey: envelope() };
  const rewrap = await putVault(user, {
    ...password,
    recoveryWrappedKey: recovery,
    baseVersion: 2,
  });
  const afterPassword = (await rewrap.json()).vault;
  expect(afterPassword).toMatchObject({
    version: 3,
    kdf: password.kdf,
    wrappedKey: password.wrappedKey,
  });
  expect(afterPassword.passwordChangedAt).not.toBe(passwordChangedAt);
  expect(afterPassword.createdAt).toBe(createdJson.vault.createdAt);

  expect(
    (await postVault(user, { ...newVault(), kdf: kdf({ m: 1024 }) })).status
  ).toBe(400);

  const actions = await db()
    .collection("audit")
    .find({ userId: user.userId })
    .map((a) => [a.action, a.target, a.deviceId])
    .toArray();
  expect(actions).toEqual([
    ["vault.create", body.keyId, user.deviceId],
    ["vault.rekey", body.keyId, user.deviceId],
    ["vault.rekey", body.keyId, user.deviceId],
  ]);
  expect((await (await sync(user, VAULT)).json()).rev).toBe(3);
});

test("vault reads are limited to 20 a minute per device", async () => {
  const user = await newUser();
  for (let i = 0; i < 20; i += 1) {
    // biome-ignore lint/performance/noAwaitInLoops: one request at a time
    expect((await getVault(user)).status).toBe(404);
  }
  const limited = await getVault(user);
  expect(limited.status).toBe(429);
  expect(Number(limited.headers.get("retry-after"))).toBeGreaterThan(0);
  expect((await limited.json()).error.code).toBe("rate_limited");
  const other = await newDevice(user.userId);
  expect((await getVault(other)).status).toBe(404);
});

test("old dokky syncs as before until the account has a vault, then gets 426", async () => {
  const user = await newUser();
  const id = randomUUID();
  expect(
    (await onId(serviceRoute.PUT, "PUT", user, id, legacyService())).status
  ).toBe(200);
  const mountId = randomUUID();
  const mount = {
    name: "nas",
    serviceId: id,
    remotePath: "",
    vfs,
    baseVersion: 0,
  };
  expect((await onId(mountRoute.PUT, "PUT", user, mountId, mount)).status).toBe(
    200
  );
  const before = await sync(user);
  expect(before.status).toBe(200);
  const etag = before.headers.get("etag") ?? "";
  expect(etag).toBe('"2"');

  await withVault(user);
  const blocked = [
    await sync(user),
    await sync(user, { "if-none-match": etag }),
    await onId(
      serviceRoute.PUT,
      "PUT",
      user,
      id,
      legacyService({ baseVersion: 1 })
    ),
    await onId(serviceRoute.DELETE, "DELETE", user, id),
    await onId(tokenRoute.POST, "POST", user, id, {
      field: "token",
      value: "t",
      expiry: "2026-11-01T00:00:00Z",
    }),
    await onId(mountRoute.PUT, "PUT", user, mountId, {
      ...mount,
      baseVersion: 1,
    }),
    await onId(mountRoute.DELETE, "DELETE", user, mountId),
  ];
  for (const res of blocked) {
    // biome-ignore lint/performance/noAwaitInLoops: one response at a time
    expect(await errorOf(res)).toEqual({
      status: 426,
      body: {
        error: {
          code: "update_required",
          message: "Update dokky to keep syncing: npm i -g dokky",
        },
      },
    });
  }
  expect(
    (
      await onId(
        mountRoute.PUT,
        "PUT",
        user,
        mountId,
        { ...mount, baseVersion: 1 },
        VAULT
      )
    ).status
  ).toBe(200);
});

test("vault-aware sync sends the sealed shape, with legacy fields until a device seals them", async () => {
  const user = await newUser();
  const id = randomUUID();
  await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    id,
    legacyService({ secrets: { pass: "hunter2", token: "t0" } })
  );
  const first = await sync(user, VAULT);
  expect(first.headers.get("etag")).toBe('"v1"');
  expect(await first.json()).toEqual({
    rev: 1,
    vault: null,
    services: [
      {
        id,
        name: "Home NAS",
        type: "smb",
        version: 1,
        updatedAt: isoTime,
        keyId: null,
        sealed: null,
        sealedToken: null,
        tokenExpiry: null,
        legacy: {
          options: { host: "nas.local", user: "ritik" },
          secrets: { pass: "hunter2", token: "t0" },
        },
      },
    ],
    mounts: [],
  });
  expect((await sync(user, { ...VAULT, "if-none-match": '"v1"' })).status).toBe(
    304
  );
  expect((await sync(user, { ...VAULT, "if-none-match": '"1"' })).status).toBe(
    200
  );

  const early = await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    id,
    sealedService("0123456789abcdef", { baseVersion: 1 }),
    VAULT
  );
  expect(await errorOf(early)).toEqual({
    status: 409,
    body: { error: { code: "no_vault", message: expect.any(String) } },
  });

  const { keyId } = await withVault(user);
  const wrong = await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    id,
    sealedService("0123456789abcdef", { baseVersion: 1 }),
    VAULT
  );
  expect((await errorOf(wrong)).body.error.code).toBe("key_mismatch");

  const body = sealedService(keyId, {
    sealedToken: envelope(120),
    tokenExpiry: "2026-11-01T02:00:00+05:30",
    baseVersion: 1,
  });
  const sealed = await onId(serviceRoute.PUT, "PUT", user, id, body, VAULT);
  expect(sealed.status).toBe(200);
  const item = {
    id,
    name: "Home NAS",
    type: "smb",
    version: 2,
    updatedAt: isoTime,
    keyId,
    sealed: body.sealed,
    sealedToken: body.sealedToken,
    tokenExpiry: "2026-10-31T20:30:00.000Z",
  };
  expect(await sealed.json()).toEqual(item);
  const raw = await db()
    .collection("services")
    .findOne({ _id: id as never });
  expect(raw).not.toHaveProperty("options");
  expect(raw).not.toHaveProperty("secrets");
  expect(raw?.tokenExpiry).toEqual({ token: new Date(item.tokenExpiry) });

  const snap = await (await sync(user, VAULT)).json();
  expect(snap).toEqual({
    rev: 3,
    vault: { keyId, version: 1 },
    services: [item],
    mounts: [],
  });

  const stale = await onId(serviceRoute.PUT, "PUT", user, id, body, VAULT);
  expect(await errorOf(stale)).toEqual({
    status: 409,
    body: {
      error: { code: "conflict", message: expect.any(String) },
      current: item,
    },
  });

  const noToken = await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    id,
    sealedService(keyId, { baseVersion: 2 }),
    VAULT
  );
  expect(await noToken.json()).toMatchObject({
    version: 3,
    sealedToken: null,
    tokenExpiry: null,
  });
  const cleared = await db()
    .collection("services")
    .findOne({ _id: id as never });
  expect(cleared).not.toHaveProperty("tokenExpiry");

  const fresh = randomUUID();
  const created = await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    fresh,
    sealedService(keyId, { name: "Cloud" }),
    VAULT
  );
  expect(await created.json()).toMatchObject({ id: fresh, version: 1, keyId });
  const clash = await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    randomUUID(),
    sealedService(keyId, { name: "Cloud" }),
    VAULT
  );
  expect((await errorOf(clash)).body.error.code).toBe("name_taken");
});

test("sealed token write-back keeps the later expiry and drops the server-encrypted copy", async () => {
  const user = await newUser();
  const id = randomUUID();
  await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    id,
    legacyService({ secrets: { pass: "hunter2", token: "t0" } })
  );
  const early = await onId(
    tokenRoute.POST,
    "POST",
    user,
    id,
    {
      keyId: "0123456789abcdef",
      sealedToken: envelope(),
      expiry: "2026-11-01T02:00:00Z",
    },
    VAULT
  );
  expect((await errorOf(early)).body.error.code).toBe("no_vault");

  const { keyId } = await withVault(user);
  const write = (expiry: string, sealedToken = envelope(), key = keyId) =>
    onId(
      tokenRoute.POST,
      "POST",
      user,
      id,
      { keyId: key, sealedToken, expiry },
      VAULT
    );

  expect(
    (
      await errorOf(
        await write("2026-11-01T02:00:00Z", envelope(), "0123456789abcdef")
      )
    ).body.error.code
  ).toBe("key_mismatch");
  const newest = envelope();
  expect(await (await write("2026-11-01T02:00:00Z", newest)).json()).toEqual({
    kept: "new",
  });
  for (const expiry of ["2026-11-01T01:00:00Z", "2026-11-01T02:00:00Z"]) {
    // biome-ignore lint/performance/noAwaitInLoops: one request at a time
    expect(await (await write(expiry)).json()).toEqual({ kept: "existing" });
  }

  const raw = await db()
    .collection("services")
    .findOne({ _id: id as never });
  expect(Object.keys(raw?.secrets)).toEqual(["pass"]);
  expect(raw).toMatchObject({ sealedToken: newest, keyId, version: 2 });

  const [service] = (await (await sync(user, VAULT)).json()).services;
  expect(service).toMatchObject({
    version: 2,
    keyId,
    sealed: null,
    sealedToken: newest,
    tokenExpiry: "2026-11-01T02:00:00.000Z",
    legacy: { secrets: { pass: "hunter2" } },
  });
  expect(decrypt(raw?.secrets.pass, aadFor(user.userId, id, "pass"))).toBe(
    "hunter2"
  );

  // a removed device finishing its uploads still writes back
  await db()
    .collection("devices")
    .updateOne(
      { tokenHash: createHash("sha256").update(user.token).digest("hex") },
      { $set: { revokedAt: new Date(), removeMode: "remove", erasedAt: null } }
    );
  const removed = await onId(
    tokenRoute.POST,
    "POST",
    user,
    id,
    { keyId, sealedToken: envelope(), expiry: "2026-11-01T03:00:00Z" },
    { ...VAULT, "x-dokky-removal": "1" }
  );
  expect(await removed.json()).toEqual({ kept: "new" });
});

test("the dashboard shows the vault without wraps, and deleting the account removes it", async () => {
  const user = await newUser();
  session = { user: { id: user.userId } };
  await onId(serviceRoute.PUT, "PUT", user, randomUUID(), legacyService());
  await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    randomUUID(),
    legacyService({ name: "Cloud" })
  );
  expect(await (await securityRoute.GET()).json()).toEqual({
    vault: null,
    accounts: 2,
    legacy: 2,
  });

  const body = await withVault(user);
  const id = randomUUID();
  await onId(
    serviceRoute.PUT,
    "PUT",
    user,
    id,
    sealedService(body.keyId, { name: "Sealed" }),
    VAULT
  );
  const res = await securityRoute.GET();
  const text = await res.text();
  expect(JSON.parse(text)).toEqual({
    vault: { keyId: body.keyId, since: isoTime, passwordChangedAt: isoTime },
    accounts: 3,
    legacy: 2,
  });
  expect(text).not.toContain(body.wrappedKey);
  expect(text).not.toContain(body.recoveryWrappedKey);

  await deleteAccount(user.userId, "");
  expect(
    await db()
      .collection("vaults")
      .countDocuments({ _id: user.userId as never })
  ).toBe(0);

  session = null;
  expect((await securityRoute.GET()).status).toBe(401);
});
