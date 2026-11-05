"use strict";
const os = require("node:os");
const path = require("node:path");
const { ensureBinary, recordInstall, stateDir } = require("./lib/binary");

(async () => {
  const pkg = require("./package.json");
  const home = os.homedir();
  const platform = process.platform;
  await ensureBinary({ pkg, home, platform, arch: process.arch, exact: true });
  recordInstall(stateDir({ env: process.env, home, platform }), path.resolve(__dirname));
})().catch((err) => {
  console.warn(`dokky: ${err.message}`);
});
