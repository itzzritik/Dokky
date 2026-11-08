"use client";

import type { LucideIcon } from "lucide-react";
import {
  type FormEvent,
  type ReactNode,
  type Ref,
  type RefObject,
  useRef,
} from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { cn } from "@/lib/utils";
import { Keycap } from "./keycap";
import { PanelEdge } from "./panel";

export const POPUP =
  "panel shell-dialog max-w-[calc(100%-2rem)] gap-0 rounded-[10px] bg-background p-0 pt-[22px] text-foreground shadow-[0_30px_90px_-20px_rgb(0_0_0/0.6)] ring-0";

export function DialogHeading({
  children,
  icon: Icon,
}: {
  children: ReactNode;
  icon: LucideIcon;
}) {
  return (
    <>
      <PanelEdge />
      <DialogTitle className="panel-title font-mono text-[12px] leading-[18px] font-medium">
        <Icon aria-hidden className="panel-icon" />
        <span className="truncate">{children}</span>
      </DialogTitle>
    </>
  );
}

interface ShellDialogProps {
  actions: ReactNode;
  aside?: ReactNode;
  cancelLabel?: string;
  children?: ReactNode;
  className?: string;
  description?: ReactNode;
  error?: string | null;
  focusCancel?: boolean;
  heading?: ReactNode;
  icon: LucideIcon;
  initialFocus?: RefObject<HTMLElement | null>;
  keys?: Record<string, () => void>;
  onCancel?: () => void;
  onOpenChange: (open: boolean) => void;
  onSubmit?: () => void;
  open: boolean;
  popupRef?: Ref<HTMLDivElement>;
  title: string;
  tone?: "danger";
}

export function ShellDialog({
  actions,
  aside,
  cancelLabel = "Cancel",
  children,
  className,
  description,
  error,
  focusCancel,
  heading,
  icon,
  initialFocus,
  keys,
  onCancel,
  onOpenChange,
  onSubmit,
  open,
  popupRef,
  title,
  tone,
}: ShellDialogProps) {
  const cancel = useRef<HTMLButtonElement>(null);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    onSubmit?.();
  };
  useShortcuts({ ...keys, ...(onSubmit && { enter: onSubmit }) }, open, true);
  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent
        className={cn(POPUP, "pb-3.5 sm:max-w-[470px]", className)}
        data-tone={tone}
        initialFocus={focusCancel ? cancel : initialFocus}
        ref={popupRef}
        showCloseButton={false}
      >
        <form onSubmit={submit}>
          <DialogHeading icon={icon}>{title}</DialogHeading>
          {aside != null && <span className="panel-aside">{aside}</span>}
          <div className="grid gap-2.5 px-6 pt-1">
            {heading && (
              <p className="font-sans text-[17px] font-semibold tracking-[-0.01em]">
                {heading}
              </p>
            )}
            {description && (
              <DialogDescription className="text-muted-foreground text-[13.5px]">
                {description}
              </DialogDescription>
            )}
            {children}
            <p
              aria-live="polite"
              className="text-destructive min-h-5 text-[12.5px]"
            >
              {error}
            </p>
          </div>
          <div className="border-rule mx-6 mt-1 flex items-center justify-between border-t pt-3.5">
            <button
              className="text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex items-center gap-2 rounded-md px-1 text-[12.5px] outline-none focus-visible:ring-2"
              onClick={() => (onCancel ? onCancel() : onOpenChange(false))}
              ref={cancel}
              type="button"
            >
              <Keycap combo="esc" />
              {cancelLabel}
            </button>
            <div className="flex items-center gap-2">{actions}</div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
