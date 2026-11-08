"use client";

import { MonitorSmartphoneIcon } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  type CSSProperties,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { PHONE, useMedia, WIDE } from "@/hooks/use-media";
import { useNow } from "@/hooks/use-now";
import { useResource } from "@/hooks/use-resource";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { ago, compareText, formatDate } from "@/lib/format";
import { cn } from "@/lib/utils";
import { send } from "../api";
import { useHints } from "../frame/frame";
import type { Hint } from "../frame/key-bar";
import { Action } from "../kit/action";
import { Count } from "../kit/count";
import { type Column, DataTable } from "../kit/data-table";
import { FilterPrompt } from "../kit/filter-prompt";
import { Panel } from "../kit/panel";
import { say } from "../message";
import { drain } from "../motion";
import { signInFresh } from "../session";
import { type Device, type DevicesData, isActive } from "../types";
import { system } from "./labels";
import { RemoveDeviceDialog } from "./remove-device-dialog";

const SKELETON_ROWS = [0, 1, 2, 3];
const NAME_SORT = { desc: false, id: "name" };
const deviceKey = (device: Device) => device.id;
const seenAt = (device: Device) => Date.parse(device.lastSeenAt) || 0;
const seen = (device: Device, now: number) =>
  isActive(device.lastSeenAt, now)
    ? "Active now"
    : (ago(device.lastSeenAt, now) ?? "never");
const line = (j: number) => ({ "--j": j }) as CSSProperties;
const rowElement = (id: string) =>
  document.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(id)}"]`);

function deviceColumns(now: number): Column<Device>[] {
  return [
    {
      cell: (device) => (
        <>
          <span>{device.name}</span>
          <small className="text-muted-foreground block font-sans text-[12px] md:hidden">
            {system(device)}, {device.version || "unknown version"},{" "}
            {seen(device, now)}
          </small>
        </>
      ),
      header: "Device",
      id: "name",
      sort: (a, b) => compareText(a.name, b.name),
      width: "minmax(0,1.2fr)",
    },
    {
      cell: (device) => (
        <span className="text-muted-foreground">{system(device)}</span>
      ),
      header: "System",
      id: "system",
      sort: (a, b) => compareText(system(a), system(b)),
      wide: true,
      width: "minmax(0,1fr)",
    },
    {
      cell: (device) => (
        <span className="text-muted-foreground">
          {device.version || "Unknown"}
        </span>
      ),
      header: "dokky",
      id: "version",
      wide: true,
      width: "84px",
    },
    {
      cell: (device) => (
        <span
          className={
            device.drives.length ? "text-muted-foreground" : "text-faint"
          }
        >
          {device.drives.length}
        </span>
      ),
      header: "Drives",
      id: "drives",
      sort: (a, b) => b.drives.length - a.drives.length,
      wide: true,
      width: "64px",
    },
    {
      cell: (device) => (
        <span
          className={
            isActive(device.lastSeenAt, now)
              ? "text-success"
              : "text-muted-foreground"
          }
        >
          {seen(device, now)}
        </span>
      ),
      header: "Last seen",
      id: "seen",
      sort: (a, b) => seenAt(b) - seenAt(a),
      wide: true,
      width: "120px",
    },
    {
      align: "end",
      cell: (device) => (
        <span className="text-faint">{formatDate(device.createdAt)}</span>
      ),
      header: "Added",
      id: "added",
      wide: true,
      width: "140px",
    },
  ];
}

function DeviceInspector({
  device,
  now,
  onRemove,
}: {
  device: Device;
  now: number;
  onRemove: () => void;
}) {
  return (
    <div className="inspector">
      <dl className="inspector-kv" style={line(0)}>
        <dt>System</dt>
        <dd>{system(device)}</dd>
        <dt>dokky</dt>
        <dd>{device.version || "Unknown"}</dd>
        <dt>Last seen</dt>
        <dd
          className={
            isActive(device.lastSeenAt, now) ? "text-success" : undefined
          }
        >
          {seen(device, now)}
        </dd>
        <dt>Added</dt>
        <dd>{formatDate(device.createdAt)}</dd>
      </dl>
      <div className="inspector-section" style={line(1)}>
        <p className="text-muted-foreground mb-2 font-mono text-[12px]">
          Drives mounted
        </p>
        {device.drives.length ? (
          <ul className="grid gap-1.5 font-mono text-[12.5px]">
            {device.drives.map((name) => (
              <li className="truncate" key={name}>
                {name}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-faint">None right now.</p>
        )}
      </div>
      <div className="inspector-actions" style={line(2)}>
        <Action combo="d" onClick={onRemove} tone="danger">
          Remove
        </Action>
      </div>
    </div>
  );
}

export function DevicesPage() {
  const router = useRouter();
  const params = useSearchParams();
  const devices = useResource<DevicesData>("/api/dash/devices");
  const wide = useMedia(WIDE, true);
  const phone = useMedia(PHONE);
  const now = useNow(60_000);
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  const list = useMemo(
    () =>
      (devices.data?.devices ?? []).filter((device) => !gone.has(device.id)),
    [devices.data, gone]
  );
  const columns = useMemo(() => deviceColumns(now), [now]);
  const [query, setQuery] = useState("");
  const deferred = useDeferredValue(query.trim().toLowerCase());
  const [activeId, setActiveId] = useState<string | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [lostAtOpen, setLostAtOpen] = useState(false);
  const [target, setTarget] = useState<Device | null>(null);
  const filter = useRef<HTMLInputElement>(null);
  const rows = useMemo(
    () =>
      deferred
        ? list.filter((device) =>
            [device.name, system(device), device.version, ...device.drives]
              .join(" ")
              .toLowerCase()
              .includes(deferred)
          )
        : list,
    [list, deferred]
  );
  const active = list.find((device) => device.id === activeId) ?? null;

  useEffect(() => {
    if (
      !wide ||
      list.length === 0 ||
      list.some((device) => device.id === activeId)
    ) {
      return;
    }
    setActiveId(
      list.reduce((first, device) =>
        compareText(device.name, first.name) < 0 ? device : first
      ).id
    );
  }, [wide, list, activeId]);

  useEffect(() => {
    if (wide) {
      setSheetOpen(false);
    }
  }, [wide]);

  // the erase dialog opens only for a device that is still listed, never a stand-in
  useEffect(() => {
    const erase = params.get("erase");
    const id = params.get("device") ?? erase;
    if (!(id && devices.data)) {
      return;
    }
    router.replace("/dashboard/devices", { scroll: false });
    const found = devices.data.devices.find((device) => device.id === id);
    if (!found) {
      say("That device is already removed.", "warn");
      return;
    }
    setActiveId(id);
    if (erase) {
      setTarget(found);
      setLostAtOpen(true);
      setRemoving(true);
    } else {
      setSheetOpen(!window.matchMedia(WIDE).matches);
    }
  }, [params, router, devices.data]);

  const choose = (id: string) => setActiveId(id);
  const open = (id: string) => {
    choose(id);
    if (!wide) {
      setSheetOpen(true);
    }
  };
  const ask = () => {
    if (!active) {
      return;
    }
    setTarget(active);
    setLostAtOpen(false);
    setRemoving(true);
  };

  const remove = async (lost: boolean) => {
    setRemoving(false);
    if (!target) {
      return;
    }
    const device = target;
    const sent = await send(
      `/api/dash/devices/${encodeURIComponent(device.id)}?mode=${lost ? "erase" : "remove"}`,
      "DELETE"
    );
    if (sent.code === "fresh_signin") {
      say("Sign in to GitHub again to erase it.", "warn");
      await signInFresh(
        `/dashboard/devices?erase=${encodeURIComponent(device.id)}`
      );
      return;
    }
    if (!sent.ok) {
      say(sent.message, "danger");
      return;
    }
    const row = rowElement(device.id);
    await drain(row ? [row] : []).finished;
    setGone((current) => new Set(current).add(device.id));
    setActiveId(null);
    say(
      lost
        ? `Removed ${device.name}. It erases itself as soon as it's online.`
        : `Removed ${device.name}. It erases itself after its uploads finish.`,
      "warn"
    );
    devices.retry();
  };

  useShortcuts({
    "/": () => filter.current?.focus(),
    d: ask,
    esc: () => setQuery(""),
    ...(devices.error ? { r: devices.retry } : {}),
  });
  const hints: Hint[] = devices.error
    ? [{ combo: "r", label: "Retry" }]
    : [
        ...(active ? [{ combo: "d", label: "Remove" }] : []),
        { combo: "/", label: "Filter" },
        ...(wide ? [] : [{ combo: "enter", label: "Open" }]),
      ];
  useHints(hints);

  let body = (
    <>
      <FilterPrompt
        label="Filter devices"
        onChange={setQuery}
        placeholder="filter by name, system, version or drive"
        ref={filter}
        value={query}
      />
      <DataTable
        activeId={activeId}
        columns={columns}
        defaultSort={NAME_SORT}
        empty={
          <p className="text-muted-foreground px-[18px] py-10 font-sans">
            {list.length
              ? "No devices match the filter."
              : "Devices show up here after you sign in on them."}
          </p>
        }
        getId={deviceKey}
        label="Devices"
        onActiveChange={choose}
        onOpen={open}
        rows={rows}
      />
    </>
  );
  if (devices.loading) {
    body = (
      <div aria-busy className="flex flex-col gap-2 px-4 pt-2">
        {SKELETON_ROWS.map((row) => (
          <Skeleton className="h-7" key={row} />
        ))}
      </div>
    );
  }
  if (devices.error) {
    body = (
      <div className="grid justify-items-start gap-3 px-[18px] py-8 font-sans">
        <p>Couldn't load your devices. {devices.error}</p>
        <Action combo="r" onClick={devices.retry} tone="primary">
          Retry
        </Action>
      </div>
    );
  }

  const inspector = active && (
    <Panel
      aside={active.version ? `dokky ${active.version}` : undefined}
      className="flex min-h-0 flex-col"
      delay={110}
      icon={MonitorSmartphoneIcon}
      title={
        <span className="title-roll" key={active.id}>
          {active.name}
        </span>
      }
    >
      <DeviceInspector
        device={active}
        key={active.id}
        now={now}
        onRemove={ask}
      />
    </Panel>
  );

  return (
    <div className="split-layout" data-solo={wide && active ? undefined : ""}>
      <Panel
        aside={
          devices.data ? <Count value={list.length} word="device" /> : undefined
        }
        className="flex min-h-0 flex-col"
        icon={MonitorSmartphoneIcon}
        title="Devices"
      >
        {body}
      </Panel>
      {wide && inspector}
      {!wide && (
        <Sheet onOpenChange={setSheetOpen} open={sheetOpen && Boolean(active)}>
          <SheetContent
            className={cn(
              "bg-background gap-0 p-3",
              phone
                ? "max-h-[88dvh] rounded-t-2xl pt-7"
                : "w-[min(420px,100vw)] pt-6 sm:max-w-[420px]"
            )}
            showCloseButton={false}
            side={phone ? "bottom" : "right"}
          >
            {phone && <i aria-hidden className="sheet-grab" />}
            <SheetTitle className="sr-only">
              {active?.name ?? "Device details"}
            </SheetTitle>
            {inspector}
          </SheetContent>
        </Sheet>
      )}
      <RemoveDeviceDialog
        accounts={devices.data?.accounts ?? []}
        lostAtOpen={lostAtOpen}
        name={target?.name ?? "this device"}
        offline={target ? !isActive(target.lastSeenAt, now) : false}
        onConfirm={(lost) => void remove(lost)}
        onOpenChange={setRemoving}
        open={removing}
      />
    </div>
  );
}
