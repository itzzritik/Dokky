import { randomBytes } from "node:crypto";
import { createServer } from "node:net";
import { join } from "node:path";
import { serve, sleep, spawn, stdin } from "bun";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, mongo } from "@/lib/db";
import { decideLogin } from "@/lib/device-auth";

const READY_TIMEOUT_MS = 180_000;
const STOP_TIMEOUT_MS = 10_000;

const freePort = () =>
  new Promise<number>((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const address = probe.address();
      const port = typeof address === "object" && address ? address.port : 0;
      probe.close(() => resolve(port));
    });
  });

const mongod = await MongoMemoryServer.create();
process.env.MONGODB_URI = mongod.getUri();
const { insertedId } = await db().collection("users").insertOne({
  name: "e2e",
  email: "e2e@example.com",
  image: null,
  emailVerified: null,
});
const userId = String(insertedId);

const port = await freePort();
const api = `http://127.0.0.1:${port}`;
const next = spawn(
  [
    "node_modules/.bin/next",
    "dev",
    "--port",
    String(port),
    "--hostname",
    "127.0.0.1",
  ],
  {
    cwd: join(import.meta.dir, ".."),
    env: {
      ...process.env,
      MONGODB_URI: mongod.getUri(),
      AUTH_SECRET: randomBytes(32).toString("base64"),
      AUTH_GITHUB_ID: "e2e",
      AUTH_GITHUB_SECRET: "e2e",
      AUTH_URL: api,
      AUTH_TRUST_HOST: "true",
      DOKKY_SECRET_KEYS: JSON.stringify({
        "1": randomBytes(32).toString("base64"),
      }),
      DOKKY_SECRET_KEY_CURRENT: "1",
      DOKKY_MIN_VERSION: "",
      NEXT_TELEMETRY_DISABLED: "1",
    },
    stdin: "ignore",
    // stdout carries only the ready line
    stdout: 2,
    stderr: "inherit",
  }
);

const helper = serve({
  hostname: "127.0.0.1",
  port: 0,
  routes: {
    "/approve": {
      POST: async (req) => {
        const { userCode } = (await req.json()) as { userCode?: unknown };
        const ok = await decideLogin(userId, userCode, "approved", new Date());
        return new Response(null, { status: ok ? 204 : 404 });
      },
    },
  },
});

let stopping: Promise<never> | undefined;
const shutdown = (code: number) => {
  stopping ??= (async () => {
    next.kill("SIGTERM");
    await Promise.race([next.exited, sleep(STOP_TIMEOUT_MS)]);
    next.kill("SIGKILL");
    await helper.stop(true);
    await mongo().close();
    await mongod.stop();
    process.exit(code);
  })();
  return stopping;
};
process.on("SIGTERM", () => shutdown(0));
process.on("SIGINT", () => shutdown(0));
stdin.text().then(() => shutdown(0));
next.exited.then((exitCode) => {
  if (!stopping) {
    console.error(`next dev exited early with code ${exitCode}`);
    shutdown(1);
  }
});

// first hits compile each route, so the daemons' requests never wait on it
const id = crypto.randomUUID();
const warm: [string, string][] = [
  ["GET", "/api/v1/sync"],
  ["POST", "/api/v1/cli/login/start"],
  ["POST", "/api/v1/cli/login/poll"],
  ["PUT", `/api/v1/services/${id}`],
  ["PUT", `/api/v1/mounts/${id}`],
];

async function warmUp(
  method: string,
  path: string,
  deadline = Date.now() + READY_TIMEOUT_MS
): Promise<void> {
  const res = await fetch(api + path, {
    method,
    body: method === "GET" ? undefined : "{}",
  }).catch(() => undefined);
  if (res && res.status >= 500) {
    throw new Error(`${method} ${path}: ${res.status} ${await res.text()}`);
  }
  if (res) {
    return;
  }
  if (Date.now() > deadline) {
    throw new Error(
      `${method} ${path}: no answer within ${READY_TIMEOUT_MS}ms`
    );
  }
  await sleep(250);
  return warmUp(method, path, deadline);
}

try {
  for (const [method, path] of warm) {
    // biome-ignore lint/performance/noAwaitInLoops: one compile at a time keeps next dev fast
    await warmUp(method, path);
  }
} catch (error) {
  console.error(error);
  await shutdown(1);
}
console.log(JSON.stringify({ api, helper: `http://127.0.0.1:${helper.port}` }));
