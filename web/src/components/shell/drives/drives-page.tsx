"use client";

import { HardDriveIcon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { PHONE, useMedia, WIDE } from "@/hooks/use-media";
import { useResource } from "@/hooks/use-resource";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { formatDate, plural } from "@/lib/format";
import { cn } from "@/lib/utils";
import { send } from "../api";
import { useHints } from "../frame/frame";
import type { Hint } from "../frame/key-bar";
import { Action } from "../kit/action";
import { type Column, DataTable } from "../kit/data-table";
import { FilterPrompt } from "../kit/filter-prompt";
import { Panel } from "../kit/panel";
import { say } from "../message";
import { drain } from "../motion";
import type { Account, Drive, DrivesData } from "../types";
import { DriveInspector, RenameField } from "./drive-inspector";
import { typeLabel } from "./labels";
import { RemoveDriveDialog } from "./remove-drive-dialog";

type Row = { account: Account; drive: Drive | null; id: string };

const SKELETON_ROWS = [0, 1, 2, 3];
const rowId = (row: Row) => row.id;
const rowElement = (id: string) =>
  document.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(id)}"]`);

function groupLabel({ account, drive }: Row) {
  if (drive) {
    return null;
  }
  const count = account.drives.length
    ? plural(account.drives.length, "drive")
    : "no drives yet, add one in the app";
  return (
    <>
      <b>{account.name}</b>
      {typeLabel(account.type)} · {count}
    </>
  );
}

function driveColumns(names: Map<string, string>): Column<Row>[] {
  const mounted = (drive: Drive) =>
    drive.devices.map((id) => names.get(id)).filter(Boolean);
  return [
    {
      cell: ({ drive }) =>
        drive && (
          <>
            <span>{drive.name}</span>
            <small className="text-muted-foreground block font-sans text-[12px] md:hidden">
              {drive.path || "Whole account"},{" "}
              {mounted(drive).length
                ? `on ${mounted(drive).join(", ")}`
                : "not mounted"}
            </small>
          </>
        ),
      header: "Drive",
      id: "name",
      width: "minmax(0,1fr)",
    },
    {
      cell: ({ drive }) =>
        drive && (
          <span className="text-muted-foreground">
            {drive.path || "whole account"}
          </span>
        ),
      header: "Path",
      id: "path",
      wide: true,
      width: "minmax(0,0.9fr)",
    },
    {
      cell: ({ drive }) => {
        if (!drive) {
          return null;
        }
        const on = mounted(drive);
        return on.length ? (
          <span>{on.join(", ")}</span>
        ) : (
          <span className="text-faint">Not mounted</span>
        );
      },
      header: "Mounted on",
      id: "devices",
      wide: true,
      width: "minmax(0,1.1fr)",
    },
    {
      align: "end",
      cell: ({ drive }) =>
        drive && (
          <span className="text-faint">{formatDate(drive.addedAt)}</span>
        ),
      header: "Added",
      id: "added",
      wide: true,
      width: "140px",
    },
  ];
}

export function DrivesPage() {
  const router = useRouter();
  const params = useSearchParams();
  const drives = useResource<DrivesData>("/api/dash/drives");
  const wide = useMedia(WIDE, true);
  const phone = useMedia(PHONE);
  const [query, setQuery] = useState("");
  const deferred = useDeferredValue(query.trim().toLowerCase());
  const [activeId, setActiveId] = useState<string | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [target, setTarget] = useState<{
    account: Account;
    drive: Drive;
  } | null>(null);
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  const filter = useRef<HTMLInputElement>(null);

  const accounts = useMemo(
    () =>
      (drives.data?.accounts ?? []).map((account) => ({
        ...account,
        drives: account.drives.filter((drive) => !gone.has(drive.id)),
      })),
    [drives.data, gone]
  );
  const devices = useMemo(() => drives.data?.devices ?? [], [drives.data]);
  const names = useMemo(
    () => new Map(devices.map((device) => [device.id, device.name])),
    [devices]
  );
  const columns = useMemo(() => driveColumns(names), [names]);
  const all = useMemo(
    () =>
      accounts.flatMap((account) =>
        account.drives.map((drive) => ({ account, drive }))
      ),
    [accounts]
  );
  const total = all.length;

  const rows = useMemo(() => {
    const out: Row[] = [];
    for (const account of accounts) {
      const matched = deferred
        ? account.drives.filter((drive) =>
            [
              drive.name,
              drive.path,
              account.name,
              typeLabel(account.type),
              ...drive.devices.map((id) => names.get(id) ?? ""),
            ]
              .join(" ")
              .toLowerCase()
              .includes(deferred)
          )
        : account.drives;
      if (deferred && matched.length === 0) {
        continue;
      }
      out.push({ account, drive: null, id: `account:${account.id}` });
      for (const drive of matched) {
        out.push({ account, drive, id: drive.id });
      }
    }
    return out;
  }, [accounts, deferred, names]);

  const active = all.find((item) => item.drive.id === activeId) ?? null;

  useEffect(() => {
    if (
      !wide ||
      all.length === 0 ||
      all.some((item) => item.drive.id === activeId)
    ) {
      return;
    }
    setActiveId(all[0].drive.id);
  }, [wide, all, activeId]);

  useEffect(() => {
    if (wide) {
      setSheetOpen(false);
    }
  }, [wide]);

  useEffect(() => {
    const id = params.get("drive");
    if (!id) {
      return;
    }
    setActiveId(id);
    setSheetOpen(!window.matchMedia(WIDE).matches);
    router.replace("/dashboard", { scroll: false });
  }, [params, router]);

  const choose = (id: string) => {
    setActiveId(id);
    setRenaming(false);
  };
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
    setRemoving(true);
  };

  const remove = async () => {
    setRemoving(false);
    if (!target) {
      return;
    }
    const { drive } = target;
    const row = rowElement(drive.id);
    const effect = drain(row ? [row] : []);
    const [sent] = await Promise.all([
      send(`/api/dash/drives/${encodeURIComponent(drive.id)}`, "DELETE"),
      effect.finished,
    ]);
    if (!sent.ok) {
      effect.cancel();
      say(sent.message, "danger");
      return;
    }
    setGone((current) => new Set(current).add(drive.id));
    setActiveId(null);
    say(
      `Removed ${drive.name}. Every device unmounts it after its uploads finish.`,
      "warn"
    );
    drives.retry();
  };

  useShortcuts({
    "/": () => filter.current?.focus(),
    d: ask,
    esc: () => setQuery(""),
    r: () => {
      if (!active) {
        return;
      }
      open(active.drive.id);
      setRenaming(true);
    },
    ...(drives.error ? { r: drives.retry } : {}),
  });

  const hints: Hint[] = drives.error
    ? [{ combo: "r", label: "Retry" }]
    : [
        ...(active
          ? [
              { combo: "r", label: "Rename" },
              { combo: "d", label: "Remove" },
            ]
          : []),
        { combo: "/", label: "Filter" },
        ...(wide ? [] : [{ combo: "enter", label: "Open" }]),
      ];
  useHints(hints);

  const empty =
    accounts.length === 0 ? (
      <div className="grid justify-items-center gap-2 px-6 py-16 text-center font-sans">
        <p className="text-[15px] font-medium">No drives yet</p>
        <p className="text-muted-foreground">
          Add accounts and drives in the dokky app.
        </p>
        <Link
          className="text-primary mt-2 font-mono text-[12.5px] hover:underline"
          href="/docs#add-drive"
        >
          How to add a drive
        </Link>
      </div>
    ) : (
      <p className="text-muted-foreground px-[18px] py-10 font-sans">
        No drives match "{query.trim()}". Press esc to clear the filter.
      </p>
    );

  let body = (
    <>
      <FilterPrompt
        label="Filter drives"
        onChange={setQuery}
        placeholder="filter by drive, account or device"
        ref={filter}
        value={query}
      />
      <DataTable
        activeId={activeId}
        columns={columns}
        empty={empty}
        getId={rowId}
        group={groupLabel}
        label="Drives"
        onActiveChange={choose}
        onOpen={open}
        rows={rows}
      />
    </>
  );
  if (drives.loading) {
    body = (
      <div aria-busy className="flex flex-col gap-2 px-4 pt-2">
        {SKELETON_ROWS.map((row) => (
          <Skeleton className="h-7" key={row} />
        ))}
      </div>
    );
  }
  if (drives.error) {
    body = (
      <div className="grid justify-items-start gap-3 px-[18px] py-8 font-sans">
        <p>Couldn't load your drives. {drives.error}</p>
        <Action combo="r" onClick={drives.retry} tone="primary">
          Retry
        </Action>
      </div>
    );
  }

  const inspector = active && (
    <Panel
      aside={typeLabel(active.account.type)}
      className="flex min-h-0 flex-col"
      delay={110}
      icon={HardDriveIcon}
      title={
        renaming ? (
          <RenameField
            drive={active.drive}
            onDone={(renamed) => {
              setRenaming(false);
              if (renamed) {
                drives.retry();
              }
            }}
          />
        ) : (
          <span className="title-roll" key={active.drive.id}>
            {active.drive.name}
          </span>
        )
      }
    >
      <DriveInspector
        account={active.account}
        devices={devices}
        drive={active.drive}
        key={active.drive.id}
        onRemove={ask}
        onRenaming={setRenaming}
      />
    </Panel>
  );

  return (
    <div className="split-layout" data-solo={wide && active ? undefined : ""}>
      <Panel
        aside={
          drives.data
            ? `${plural(total, "drive")} · ${plural(accounts.length, "account")}`
            : undefined
        }
        className="flex min-h-0 flex-col"
        icon={HardDriveIcon}
        title="Drives"
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
              {active?.drive.name ?? "Drive details"}
            </SheetTitle>
            {inspector}
          </SheetContent>
        </Sheet>
      )}
      <RemoveDriveDialog
        account={target?.account.name ?? "your storage"}
        name={target?.drive.name ?? "this drive"}
        onConfirm={() => void remove()}
        onOpenChange={setRemoving}
        open={removing}
      />
    </div>
  );
}
