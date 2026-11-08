export interface Drive {
  addedAt: string;
  devices: string[];
  id: string;
  name: string;
  path: string;
}

export interface Account {
  drives: Drive[];
  id: string;
  name: string;
  type: string;
}

export interface DrivesData {
  accounts: Account[];
  devices: { id: string; lastSeenAt: string; name: string }[];
}

export interface Device {
  arch: string;
  createdAt: string;
  drives: string[];
  id: string;
  lastSeenAt: string;
  name: string;
  os: string;
  version: string;
}

export interface DevicesData {
  accounts: string[];
  devices: Device[];
}

export interface Entry {
  at: string;
  device: string;
  id: string;
  kind: string;
  name: string;
  ok: boolean;
  reason: string;
  target: string;
}

export interface Me {
  accounts: number;
  devices: number;
  drives: number;
  email: string;
  image: string | null;
  name: string;
}

// Seen within the 15 minute sync, with a minute of slack.
export const ONLINE_MS = 16 * 60_000;
export const isActive = (lastSeenAt: string, now: number) =>
  now - Date.parse(lastSeenAt) < ONLINE_MS;
