"use client";

import type { ReactNode } from "react";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";

interface DetailsSheetProps {
  children: ReactNode;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  title: string;
}

// Below the sidebar breakpoint the details slide in from the right, leaving a strip to tap away.
export function DetailsSheet({
  children,
  onOpenChange,
  open,
  title,
}: DetailsSheetProps) {
  return (
    <Sheet onOpenChange={onOpenChange} open={open}>
      <SheetContent
        className="bg-background w-[min(380px,88vw)] gap-0 p-3 pt-6 sm:max-w-[380px]"
        showCloseButton={false}
        side="right"
      >
        <SheetTitle className="sr-only">{title}</SheetTitle>
        {children}
      </SheetContent>
    </Sheet>
  );
}
