"use client";

import { Command as CommandPrimitive } from "cmdk";
import type * as React from "react";
import { cn } from "@/lib/utils";

function Command({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive>) {
  return (
    <CommandPrimitive
      className={cn(
        "text-foreground flex size-full flex-col overflow-hidden",
        className
      )}
      data-slot="command"
      {...props}
    />
  );
}

function CommandInput({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Input>) {
  return (
    <CommandPrimitive.Input
      className={cn(
        "text-foreground placeholder:text-faint h-full w-full min-w-0 flex-1 bg-transparent outline-hidden disabled:opacity-50",
        className
      )}
      data-slot="command-input"
      {...props}
    />
  );
}

function CommandList({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.List>) {
  return (
    <CommandPrimitive.List
      className={cn(
        "max-h-80 scroll-py-2 overflow-x-hidden overflow-y-auto",
        className
      )}
      data-slot="command-list"
      {...props}
    />
  );
}

function CommandEmpty({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Empty>) {
  return (
    <CommandPrimitive.Empty
      className={cn(
        "text-muted-foreground py-8 text-center text-sm",
        className
      )}
      data-slot="command-empty"
      {...props}
    />
  );
}

function CommandGroup({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Group>) {
  return (
    <CommandPrimitive.Group
      className={cn(
        "**:[[cmdk-group-heading]]:text-faint **:[[cmdk-group-heading]]:px-2.5 **:[[cmdk-group-heading]]:pt-2 **:[[cmdk-group-heading]]:pb-1 **:[[cmdk-group-heading]]:text-[12px]",
        className
      )}
      data-slot="command-group"
      {...props}
    />
  );
}

function CommandItem({
  className,
  ...props
}: React.ComponentProps<typeof CommandPrimitive.Item>) {
  return (
    <CommandPrimitive.Item
      className={cn(
        "data-[selected=true]:bg-raise data-[selected=true]:before:bg-primary [&_svg]:text-muted-foreground relative flex h-[34px] cursor-default items-center gap-3 rounded-md px-2.5 text-[13px] outline-hidden select-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-[selected=true]:before:absolute data-[selected=true]:before:inset-y-2 data-[selected=true]:before:left-0 data-[selected=true]:before:w-0.5 data-[selected=true]:before:rounded-full [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-3.5",
        className
      )}
      data-slot="command-item"
      {...props}
    />
  );
}

export {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
};
