"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react";
import {
  type CSSProperties,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { Checkbox } from "@/components/ui/checkbox";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { PHONE, useMedia } from "@/hooks/use-media";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { cn } from "@/lib/utils";
import { replay } from "../motion";

export interface Column<T> {
  align?: "end";
  cell: (row: T) => ReactNode;
  header: string;
  id: string;
  sort?: (a: T, b: T) => number;
  // true shows from 768px, "xl" only from 1280px where the table has room for it
  wide?: boolean | "xl";
  width: string;
}

interface Sort {
  desc: boolean;
  id: string;
}

interface DataTableProps<T> {
  activeId?: string | null;
  columns: Column<T>[];
  defaultSort?: Sort;
  empty?: ReactNode;
  getId: (row: T) => string;
  keyboard?: boolean;
  label: string;
  menu?: (id: string) => ReactNode;
  onActiveChange?: (id: string) => void;
  onOpen?: (id: string) => void;
  onSelectedChange?: (next: Set<string>) => void;
  rowData?: (row: T) => Record<`data-${string}`, string | undefined>;
  rows: T[];
  selected?: ReadonlySet<string>;
  selectMode?: "always" | "auto";
}

const ROW = 34;
const PHONE_ROW = 52;
const HEAD = 26;
const PRINT_STEP = 38;
const PRINT_SPAN = 600;
const PRINT_MS = 1900;
const GRID =
  "grid items-center gap-3.5 pr-4 pl-[18px] [grid-template-columns:var(--cols-narrow)] md:[grid-template-columns:var(--cols-mid)] xl:[grid-template-columns:var(--cols)]";
const cellClass = <T,>(column: Column<T>) =>
  cn(
    "min-w-0 truncate",
    column.wide === true && "hidden md:block",
    column.wide === "xl" && "hidden xl:block",
    column.align === "end" && "text-right"
  );

export function DataTable<T>({
  activeId = null,
  columns,
  defaultSort,
  empty,
  getId,
  keyboard = true,
  label,
  menu,
  onActiveChange,
  onOpen,
  onSelectedChange,
  rowData,
  rows,
  selectMode = "auto",
  selected,
}: DataTableProps<T>) {
  const domId = useId();
  const scrollRef = useRef<HTMLDivElement>(null);
  const band = useRef<HTMLSpanElement>(null);
  const anchor = useRef(-1);
  const phone = useMedia(PHONE);
  const rowHeight = phone ? PHONE_ROW : ROW;
  const [sort, setSort] = useState<Sort | null>(defaultSort ?? null);
  const [hover, setHover] = useState(-1);
  const [printing, setPrinting] = useState(true);

  useEffect(() => {
    const timer = setTimeout(() => setPrinting(false), PRINT_MS);
    return () => clearTimeout(timer);
  }, []);

  const ordered = useMemo(() => {
    const compare = columns.find((column) => column.id === sort?.id)?.sort;
    if (!(compare && sort)) {
      return rows;
    }
    const sorted = [...rows].sort(compare);
    return sort.desc ? sorted.reverse() : sorted;
  }, [columns, rows, sort]);

  // Stable identity: the virtualizer rebuilds every measurement when getItemKey changes.
  const getItemKey = useCallback(
    (index: number) => {
      const row = ordered[index];
      return row ? getId(row) : index;
    },
    [ordered, getId]
  );

  const virtualizer = useVirtualizer({
    count: ordered.length,
    estimateSize: () => rowHeight,
    getItemKey,
    getScrollElement: () => scrollRef.current,
    overscan: 10,
    paddingStart: HEAD,
    scrollPaddingStart: HEAD,
  });

  const measured = useRef(rowHeight);
  useEffect(() => {
    if (measured.current === rowHeight) {
      return;
    }
    measured.current = rowHeight;
    virtualizer.measure();
  }, [rowHeight, virtualizer]);

  const activeIndex = activeId
    ? ordered.findIndex((row) => getId(row) === activeId)
    : -1;

  useEffect(() => {
    if (activeIndex < 0) {
      return;
    }
    virtualizer.scrollToIndex(activeIndex, { align: "auto" });
    replay(band.current, "stretch");
  }, [activeIndex, virtualizer]);

  const move = (delta: number) => {
    const next =
      ordered[Math.min(ordered.length - 1, Math.max(0, activeIndex + delta))];
    if (next) {
      onActiveChange?.(getId(next));
    }
  };

  const toggle = (index: number, range: boolean) => {
    const target = ordered[index];
    if (!(selected && onSelectedChange && target)) {
      return;
    }
    const on = !selected.has(getId(target));
    const ranged = range && anchor.current >= 0;
    const from = ranged ? Math.min(anchor.current, index) : index;
    const to = ranged ? Math.max(anchor.current, index) : index;
    const next = new Set(selected);
    for (const row of ordered.slice(from, to + 1)) {
      if (on) {
        next.add(getId(row));
      } else {
        next.delete(getId(row));
      }
    }
    anchor.current = index;
    onSelectedChange(next);
  };

  useShortcuts(
    {
      arrowdown: () => move(1),
      arrowup: () => move(-1),
      enter: () => {
        if (activeId) {
          onOpen?.(activeId);
        }
      },
      j: () => move(1),
      k: () => move(-1),
      x: () => {
        if (activeIndex >= 0) {
          toggle(activeIndex, false);
        }
      },
    },
    keyboard
  );

  const checks =
    Boolean(selected) && (selectMode === "always" || (selected?.size ?? 0) > 0);
  const template = (tier: 0 | 1 | 2) =>
    [
      checks ? "16px" : "",
      ...columns
        .filter(
          (column) =>
            !column.wide || (column.wide === true ? tier > 0 : tier > 1)
        )
        .map((column) => column.width),
    ]
      .join(" ")
      .trim();
  // Every on-screen row prints on its own beat; the beat shrinks so a tall screen still finishes within PRINT_SPAN.
  const printed = Math.max(
    1,
    Math.min(
      ordered.length,
      Math.ceil((virtualizer.scrollRect?.height ?? 0) / rowHeight) ||
        ordered.length
    )
  );
  const style = {
    "--cols": template(2),
    "--cols-mid": template(1),
    "--cols-narrow": template(0),
    "--step": `${Math.min(PRINT_STEP, PRINT_SPAN / printed)}ms`,
  } as CSSProperties;
  const allSelected =
    Boolean(selected) &&
    ordered.length > 0 &&
    ordered.every((row) => selected?.has(getId(row)));

  const grid = (
    <div
      aria-activedescendant={
        activeIndex >= 0 ? `${domId}-${activeId}` : undefined
      }
      aria-label={label}
      aria-multiselectable={selected ? true : undefined}
      aria-rowcount={ordered.length}
      className="dt"
      onPointerLeave={() => setHover(-1)}
      ref={scrollRef}
      role="grid"
      style={style}
      tabIndex={0}
    >
      <div className="relative" style={{ height: virtualizer.getTotalSize() }}>
        <div className={cn(GRID, "dt-head")} role="row">
          {checks && (
            <Checkbox
              aria-label={`Select all ${label}`}
              checked={allSelected}
              onCheckedChange={(checked) =>
                onSelectedChange?.(
                  checked ? new Set(ordered.map(getId)) : new Set()
                )
              }
            />
          )}
          {columns.map((column) => (
            <HeaderCell
              column={column}
              key={column.id}
              onSort={() =>
                setSort((current) =>
                  current?.id === column.id
                    ? { desc: !current.desc, id: column.id }
                    : { desc: false, id: column.id }
                )
              }
              sort={sort}
            />
          ))}
        </div>
        {hover >= 0 && hover < ordered.length && hover !== activeIndex && (
          <span
            aria-hidden
            className="dt-hover"
            style={{
              height: rowHeight,
              transform: `translateY(${HEAD + hover * rowHeight}px)`,
            }}
          />
        )}
        {activeIndex >= 0 && (
          <span
            aria-hidden
            className="dt-band"
            ref={band}
            style={{
              height: rowHeight,
              transform: `translateY(${HEAD + activeIndex * rowHeight}px)`,
            }}
          />
        )}
        {virtualizer.getVirtualItems().map((item) => {
          const row = ordered[item.index];
          if (!row) {
            return null;
          }
          const id = getId(row);
          return (
            <div
              aria-selected={selected ? selected.has(id) : id === activeId}
              className={cn(GRID, "dt-row")}
              data-print={printing ? "" : undefined}
              data-row-id={id}
              id={`${domId}-${id}`}
              key={item.key}
              onClick={(event) => {
                if (event.shiftKey && selected) {
                  toggle(item.index, true);
                  return;
                }
                anchor.current = item.index;
                onActiveChange?.(id);
                if (onOpen) {
                  onOpen(id);
                } else if (selected) {
                  toggle(item.index, false);
                }
              }}
              onPointerEnter={() => setHover(item.index)}
              role="row"
              style={
                {
                  "--i": Math.min(item.index, printed),
                  height: rowHeight,
                  transform: `translateY(${item.start}px)`,
                } as CSSProperties
              }
              {...rowData?.(row)}
            >
              {checks && (
                <Checkbox
                  aria-label="Select row"
                  checked={selected?.has(id)}
                  onClick={(event) => {
                    event.stopPropagation();
                    toggle(item.index, event.shiftKey);
                  }}
                  tabIndex={keyboard ? -1 : 0}
                />
              )}
              {columns.map((column) => (
                <div
                  className={cellClass(column)}
                  key={column.id}
                  role="gridcell"
                >
                  {column.cell(row)}
                </div>
              ))}
            </div>
          );
        })}
      </div>
      {ordered.length === 0 && (
        <div className="absolute inset-x-0" style={{ top: HEAD }}>
          {empty}
        </div>
      )}
    </div>
  );

  if (!menu) {
    return grid;
  }
  return (
    <ContextMenu>
      <ContextMenuTrigger
        className="flex min-h-0 flex-1 flex-col"
        onContextMenu={(event) => {
          const id = (event.target as HTMLElement).closest<HTMLElement>(
            "[data-row-id]"
          )?.dataset.rowId;
          if (id) {
            onActiveChange?.(id);
          }
        }}
      >
        {grid}
      </ContextMenuTrigger>
      {activeId && (
        <ContextMenuContent className="w-56">
          {menu(activeId)}
        </ContextMenuContent>
      )}
    </ContextMenu>
  );
}

function HeaderCell<T>({
  column,
  onSort,
  sort,
}: {
  column: Column<T>;
  onSort: () => void;
  sort: Sort | null;
}) {
  if (!column.sort) {
    return (
      <div className={cellClass(column)} role="columnheader">
        {column.header}
      </div>
    );
  }
  let direction: "ascending" | "descending" | "none" = "none";
  if (sort?.id === column.id) {
    direction = sort.desc ? "descending" : "ascending";
  }
  return (
    <div
      aria-sort={direction}
      className={cellClass(column)}
      role="columnheader"
    >
      <button
        className={cn(
          "hover:text-foreground inline-flex items-center gap-1 transition-colors",
          direction !== "none" && "text-muted-foreground"
        )}
        onClick={onSort}
        type="button"
      >
        {column.header}
        {direction === "ascending" && <ArrowUpIcon className="size-3" />}
        {direction === "descending" && <ArrowDownIcon className="size-3" />}
      </button>
    </div>
  );
}
