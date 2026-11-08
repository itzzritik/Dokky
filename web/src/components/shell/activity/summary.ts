const SAY: Record<string, (name: string) => string> = {
  "device.added": (n) => `Added device ${n}`,
  "device.erased": (n) => `${n} erased`,
  "device.removed": (n) => `Removed device ${n}`,
  "device.renamed": (n) => `Renamed a device to ${n}`,
  "drive.added": (n) => `Added ${n}`,
  "drive.cache": (n) => `Changed the cache of ${n}`,
  "drive.folder": (n) => `Changed the path of ${n}`,
  "drive.mounted": (n) => `Mounted ${n}`,
  "drive.mountFailed": (n) => `Couldn't mount ${n}`,
  "drive.recovered": (n) => `${n} mounted again`,
  "drive.removed": (n) => `Removed ${n}`,
  "drive.removedOn": (n) => `Finished removing ${n}`,
  "drive.renamed": (n) => `Renamed ${n}`,
  "drive.unmounted": (n) => `Unmounted ${n}`,
  "signin.added": (n) => `Signed in to ${n}`,
  "signin.expired": (n) => `Sign-in expired for ${n}`,
  "signin.fixed": (n) => `Signed in again to ${n}`,
};

// The TUI's Activity wording (cli/internal/tui/screen/activity/activity.go Summary).
export const summary = (kind: string, name: string) =>
  SAY[kind]?.(name) ?? name;

export const tone = (kind: string, ok: boolean, open?: boolean) => {
  if (kind === "signin.expired") {
    return open ? "text-warning" : "";
  }
  return ok ? "" : "text-destructive";
};
