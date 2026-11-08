"use client";

import { type CSSProperties, useEffect, useRef, useState } from "react";
import { useNow } from "@/hooks/use-now";
import { ago, formatDate } from "@/lib/format";
import { send } from "../api";
import { Action } from "../kit/action";
import { say } from "../message";
import type { Account, Drive, DrivesData } from "../types";
import { isActive } from "../types";
import { typeLabel } from "./labels";

const line = (j: number) => ({ "--j": j }) as CSSProperties;

const RENAME_ERRORS: Record<string, string> = {
  bad_name: "Names can't have / or : and are at most 40 characters.",
  name_taken: "Another drive uses this name.",
};

interface InspectorProps {
  account: Account;
  devices: DrivesData["devices"];
  drive: Drive;
  onRemove: () => void;
  onRenaming: (on: boolean) => void;
}

export function DriveInspector({
  account,
  devices,
  drive,
  onRemove,
  onRenaming,
}: InspectorProps) {
  const now = useNow(60_000);
  const on = devices.filter((device) => drive.devices.includes(device.id));
  return (
    <div className="inspector">
      <dl className="inspector-kv" style={line(0)}>
        <dt>Account</dt>
        <dd className="truncate">
          {account.name}{" "}
          <span className="text-faint">{typeLabel(account.type)}</span>
        </dd>
        <dt>Path</dt>
        <dd className="truncate font-mono">{drive.path || "Whole account"}</dd>
        <dt>Added</dt>
        <dd>{formatDate(drive.addedAt)}</dd>
      </dl>
      <div className="inspector-section" style={line(1)}>
        <p className="text-muted-foreground mb-2 font-mono text-[12px]">
          Mounted on
        </p>
        {on.length ? (
          <ul className="grid gap-1.5">
            {on.map((device) => (
              <li
                className="flex justify-between gap-3 font-mono text-[12.5px]"
                key={device.id}
              >
                <span className="truncate">{device.name}</span>
                <span
                  className={
                    isActive(device.lastSeenAt, now)
                      ? "text-success"
                      : "text-muted-foreground"
                  }
                >
                  {isActive(device.lastSeenAt, now)
                    ? "Active now"
                    : ago(device.lastSeenAt, now)}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-faint">No device has it mounted.</p>
        )}
      </div>
      <div className="inspector-actions" style={line(2)}>
        <Action combo="r" onClick={() => onRenaming(true)}>
          Rename
        </Action>
        <Action combo="d" onClick={onRemove} tone="danger">
          Remove
        </Action>
      </div>
    </div>
  );
}

export function RenameField({
  drive,
  onDone,
}: {
  drive: Drive;
  onDone: (renamed: boolean) => void;
}) {
  const [name, setName] = useState(drive.name);
  const [saving, setSaving] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.select();
  }, []);

  const save = async () => {
    const trimmed = name.trim();
    if (trimmed === drive.name) {
      onDone(false);
      return;
    }
    setSaving(true);
    const sent = await send(
      `/api/dash/drives/${encodeURIComponent(drive.id)}`,
      "PATCH",
      { name: trimmed }
    );
    setSaving(false);
    if (!sent.ok) {
      say(RENAME_ERRORS[sent.code] ?? sent.message, "danger");
      input.current?.focus();
      return;
    }
    say(`Renamed to ${trimmed}. Every device picks it up at its next sync.`);
    onDone(true);
  };

  return (
    <input
      aria-label="Drive name"
      className="rename-input"
      maxLength={60}
      onBlur={() => {
        if (!saving) {
          onDone(false);
        }
      }}
      onChange={(event) => setName(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          void save();
        }
        if (event.key === "Escape") {
          event.preventDefault();
          onDone(false);
        }
      }}
      readOnly={saving}
      ref={input}
      spellCheck={false}
      value={name}
    />
  );
}
