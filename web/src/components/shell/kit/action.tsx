"use client";

import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { Keycap } from "./keycap";

const TONES = {
  danger:
    "border-border bg-transparent text-destructive hover:bg-destructive/10 dark:bg-transparent dark:hover:bg-destructive/10",
  fill: "bg-destructive text-white hover:bg-destructive/90",
  outline:
    "border-border bg-transparent hover:bg-raise dark:bg-transparent dark:hover:bg-raise",
  primary: "",
  quiet:
    "bg-transparent px-1 text-muted-foreground hover:bg-transparent hover:text-foreground dark:hover:bg-transparent",
} as const;

const VARIANTS = {
  danger: "outline",
  fill: "default",
  outline: "outline",
  primary: "default",
  quiet: "ghost",
} as const;

type ActionProps = ComponentProps<typeof Button> & {
  combo?: string;
  tone?: keyof typeof TONES;
};

export function Action({
  children,
  className,
  combo,
  tone = "outline",
  ...props
}: ActionProps) {
  return (
    <Button
      className={cn(
        "h-8 gap-2 rounded-md px-3 font-sans text-[13px] font-medium",
        combo && "pl-1.5",
        TONES[tone],
        className
      )}
      data-tone={tone}
      variant={VARIANTS[tone]}
      {...props}
    >
      {combo && <Keycap combo={combo} />}
      {children}
    </Button>
  );
}
