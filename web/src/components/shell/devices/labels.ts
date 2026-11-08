import type { Device } from "../types";

const PLATFORMS: Record<string, string> = {
  darwin: "macOS",
  linux: "Linux",
  windows: "Windows",
};
const ARCHES: Record<string, string> = {
  "386": "x86",
  amd64: "x64",
  arm64: "ARM64",
};

type Platform = Pick<Device, "arch" | "os">;

export const systemLabel = ({ os }: Platform) => PLATFORMS[os] ?? os;

export const archLabel = ({ arch, os }: Platform) => {
  if (os === "darwin" || os.startsWith("macOS")) {
    return arch === "arm64" ? "Apple Silicon" : "Intel";
  }
  return ARCHES[arch] ?? arch;
};

export const system = (device: Platform) =>
  [systemLabel(device), archLabel(device)].filter(Boolean).join(", ");
