"use client";

import { ActivityIcon } from "lucide-react";
import {
  type CSSProperties,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { useMedia, WIDE } from "@/hooks/use-media";
import { useNow } from "@/hooks/use-now";
import { useResource } from "@/hooks/use-resource";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { ago } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useHints } from "../frame/frame";
import type { Hint } from "../frame/key-bar";
import { Action } from "../kit/action";
import { type Column, DataTable } from "../kit/data-table";
import { DetailsSheet } from "../kit/details-sheet";
import { FilterPrompt } from "../kit/filter-prompt";
import { Panel } from "../kit/panel";
import { say } from "../message";
import type { Entry } from "../types";
import { summary, tone } from "./summary";

interface Page {
  items: Entry[];
  more: boolean;
}

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];
const LOCAL = /\.local$/u;
const FOLDER = /^Folder /u;
const entryId = (entry: Entry) => entry.id;
const device = (entry: Entry) =>
  entry.device.replace(LOCAL, "") || "Unknown device";
const details = (entry: Entry) => entry.reason.replace(FOLDER, "Path ");
const line = (j: number) => ({ "--j": j }) as CSSProperties;
const stamp = (at: string) =>
  new Date(at).toLocaleString(undefined, {
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    month: "long",
    year: "numeric",
  });

function entryColumns(now: number): Column<Entry>[] {
  return [
    {
      cell: (entry) => (
        <span className="text-muted-foreground">{ago(entry.at, now)}</span>
      ),
      header: "When",
      id: "at",
      wide: true,
      width: "120px",
    },
    {
      cell: (entry) => (
        <>
          <span className={tone(entry.kind, entry.ok)}>
            {summary(entry.kind, entry.name)}
          </span>
          <small className="text-muted-foreground block font-sans text-[12px] md:hidden">
            {device(entry)}, {ago(entry.at, now)}
          </small>
        </>
      ),
      header: "What",
      id: "what",
      width: "minmax(0,1.6fr)",
    },
    {
      cell: (entry) => (
        <span className="text-muted-foreground">{device(entry)}</span>
      ),
      header: "Device",
      id: "device",
      wide: true,
      width: "minmax(0,0.7fr)",
    },
  ];
}

export function ActivityPage() {
  const first = useResource<Page>("/api/dash/activity");
  const wide = useMedia(WIDE, true);
  const now = useNow(60_000);
  const columns = useMemo(() => entryColumns(now), [now]);
  const [older, setOlder] = useState<Page>({ items: [], more: true });
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState("");
  const deferred = useDeferredValue(query.trim().toLowerCase());
  const [activeId, setActiveId] = useState<string | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const filter = useRef<HTMLInputElement>(null);

  const list = useMemo(() => {
    const head = first.data?.items ?? [];
    const seen = new Set(head.map(entryId));
    return [...head, ...older.items.filter((entry) => !seen.has(entry.id))];
  }, [first.data, older.items]);
  const more = older.items.length ? older.more : (first.data?.more ?? false);
  const rows = useMemo(
    () =>
      deferred
        ? list.filter((entry) =>
            [summary(entry.kind, entry.name), device(entry), details(entry)]
              .join(" ")
              .toLowerCase()
              .includes(deferred)
          )
        : list,
    [list, deferred]
  );
  const active = list.find((entry) => entry.id === activeId) ?? null;

  useEffect(() => {
    if (
      !wide ||
      list.length === 0 ||
      list.some((entry) => entry.id === activeId)
    ) {
      return;
    }
    setActiveId(list[0].id);
  }, [wide, list, activeId]);

  useEffect(() => {
    if (wide) {
      setSheetOpen(false);
    }
  }, [wide]);

  const loadOlder = async () => {
    const last = list.at(-1);
    if (!(last && more) || loading) {
      return;
    }
    setLoading(true);
    const res = await fetch(
      `/api/dash/activity?before=${encodeURIComponent(last.at)}`
    ).catch(() => null);
    setLoading(false);
    if (!res?.ok) {
      say("Couldn't load older activity. Try again.", "danger");
      return;
    }
    const page = (await res.json()) as Page;
    setOlder((current) => ({
      items: [...current.items, ...page.items],
      more: page.more,
    }));
  };

  const open = (id: string) => {
    setActiveId(id);
    if (!wide) {
      setSheetOpen(true);
    }
  };

  useShortcuts({
    "/": () => filter.current?.focus(),
    esc: () => setQuery(""),
    o: () => void loadOlder(),
    ...(first.error ? { r: first.retry } : {}),
  });
  const hints: Hint[] = first.error
    ? [{ combo: "r", label: "Retry" }]
    : [
        { combo: "/", label: "Filter" },
        ...(more ? [{ combo: "o", label: "Load older" }] : []),
        ...(wide ? [] : [{ combo: "enter", label: "Open" }]),
      ];
  useHints(hints);

  let body = (
    <>
      <FilterPrompt
        label="Filter activity"
        onChange={setQuery}
        placeholder="filter by drive, device or what happened"
        ref={filter}
        value={query}
      />
      <DataTable
        activeId={activeId}
        columns={columns}
        empty={
          <p className="text-muted-foreground px-[18px] py-10 font-sans">
            {list.length
              ? "Nothing matches the filter."
              : "Nothing yet. What your devices do shows up here."}
          </p>
        }
        getId={entryId}
        label="Activity"
        onActiveChange={setActiveId}
        onOpen={open}
        rows={rows}
      />
      {more && list.length > 0 && (
        <div className="border-rule border-t px-[18px] pt-2.5">
          <Action
            combo="o"
            disabled={loading}
            onClick={() => void loadOlder()}
            tone="quiet"
          >
            {loading ? "Loading" : "Load older"}
          </Action>
        </div>
      )}
    </>
  );
  if (first.loading) {
    body = (
      <div aria-busy className="flex flex-col gap-2 px-4 pt-2">
        {SKELETON_ROWS.map((row) => (
          <Skeleton className="h-7" key={row} />
        ))}
      </div>
    );
  }
  if (first.error) {
    body = (
      <div className="grid justify-items-start gap-3 px-[18px] py-8 font-sans">
        <p>Couldn't load your activity. {first.error}</p>
        <Action combo="r" onClick={first.retry} tone="primary">
          Retry
        </Action>
      </div>
    );
  }

  const inspector = active && (
    <Panel
      className="flex min-h-0 flex-1 flex-col"
      delay={110}
      icon={ActivityIcon}
      title="Details"
    >
      <div className="inspector" key={active.id}>
        <p
          className={cn(
            "px-[18px] pb-3 font-mono text-[14px] font-medium",
            tone(active.kind, active.ok)
          )}
          style={line(0)}
        >
          {summary(active.kind, active.name)}
        </p>
        <dl className="inspector-kv" style={line(1)}>
          <dt>When</dt>
          <dd>{stamp(active.at)}</dd>
          <dt>Device</dt>
          <dd className="truncate">{device(active)}</dd>
          <dt>Result</dt>
          <dd className={active.ok ? "text-success" : "text-destructive"}>
            {active.ok ? "Done" : "Failed"}
          </dd>
          {details(active) && (
            <>
              <dt>Details</dt>
              <dd className="break-words">{details(active)}</dd>
            </>
          )}
        </dl>
      </div>
    </Panel>
  );

  return (
    <div className="split-layout" data-solo={wide && active ? undefined : ""}>
      <Panel
        aside={first.data ? "Last 90 days" : undefined}
        className="flex min-h-0 flex-col"
        icon={ActivityIcon}
        title="Activity"
      >
        {body}
      </Panel>
      {wide && inspector}
      {!wide && (
        <DetailsSheet
          onOpenChange={setSheetOpen}
          open={sheetOpen && Boolean(active)}
          title={"Activity details"}
        >
          {inspector}
        </DetailsSheet>
      )}
    </div>
  );
}
