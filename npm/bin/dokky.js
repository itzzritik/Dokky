#!/usr/bin/env node
"use strict";
const { spawn } = require("node:child_process");
const os = require("node:os");
const path = require("node:path");
const { resolveBinary, recordInstall, stateDir } = require("../lib/binary");

(async () => {
  const pkgDir = path.resolve(__dirname, "..");
  const pkg = require("../package.json");
  const home = os.homedir();
  const platform = process.platform;
  const bin = await resolveBinary({ pkg, home, platform, arch: process.arch, say: console.error });
  try {
    recordInstall(stateDir({ env: process.env, home, platform }), pkgDir);
  } catch {}
  const child = spawn(bin, process.argv.slice(2), { stdio: "inherit" });
  for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => child.kill(sig));
  child.on("error", (err) => {
    console.error(`dokky: ${err.message}`);
    process.exit(1);
  });
  child.on("exit", (code, signal) => process.exit(code ?? (signal ? 1 : 0)));
})().catch((err) => {
  console.error(`dokky: ${err.message}`);
  process.exit(1);
});
