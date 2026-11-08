"use client";

import {
  CommandIcon,
  HardDriveIcon,
  LogOutIcon,
  MonitorSmartphoneIcon,
  SunMoonIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { useResource } from "@/hooks/use-resource";
import { cn } from "@/lib/utils";
import { DialogHeading, POPUP } from "../kit/dialog";
import { Keycap } from "../kit/keycap";
import { PAGES } from "../nav";
import { logout } from "../session";
import type { DevicesData, DrivesData } from "../types";

// Plain matching beats cmdk's fuzzy scorer here: long names otherwise match almost any query.
const match = (value: string, search: string, keywords?: string[]) => {
  const query = search.trim().toLowerCase();
  if (!query) {
    return 1;
  }
  const text = [value, ...(keywords ?? [])].join(" ").toLowerCase();
  if (text.startsWith(query)) {
    return 1;
  }
  if (text.includes(` ${query}`)) {
    return 0.8;
  }
  return text.includes(query) ? 0.5 : 0;
};

interface PaletteProps {
  onOpenChange: (open: boolean) => void;
  onTheme: () => void;
  open: boolean;
}

export function CommandPalette({ onOpenChange, onTheme, open }: PaletteProps) {
  const router = useRouter();
  const drives = useResource<DrivesData>("/api/dash/drives").data;
  const devices =
    useResource<DevicesData>("/api/dash/devices").data?.devices ?? [];
  const [value, setValue] = useState("");

  useEffect(() => {
    if (open) {
      setValue("");
    }
  }, [open]);

  const run = (action: () => void) => {
    onOpenChange(false);
    action();
  };
  const go = (href: string) => run(() => router.push(href));

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent
        className={cn(POPUP, "top-[12vh] translate-y-0 pb-3 sm:max-w-[600px]")}
        showCloseButton={false}
      >
        <DialogHeading icon={CommandIcon}>Commands</DialogHeading>
        <Command
          filter={match}
          loop
          onValueChange={setValue}
          value={value}
          vimBindings={false}
        >
          <div className="px-5 pt-1 pb-2">
            <div className="field gap-2.5">
              <span aria-hidden className="text-primary font-mono">
                ❯
              </span>
              <CommandInput
                className="font-mono text-[14px]"
                placeholder="Type a command, drive or device"
              />
            </div>
          </div>
          <CommandList className="max-h-[min(420px,60vh)] px-3">
            <CommandEmpty>
              Nothing matches. Try a drive, a device or "theme".
            </CommandEmpty>
            <CommandGroup heading="Actions">
              <CommandItem
                onSelect={() => run(onTheme)}
                value="Switch theme dark light"
              >
                <SunMoonIcon />
                Switch theme
                <Keycap className="ml-auto" combo="t" />
              </CommandItem>
              <CommandItem
                onSelect={() => run(() => void logout())}
                value="Sign out log out"
              >
                <LogOutIcon />
                Sign out
                <Keycap className="ml-auto" combo="s" />
              </CommandItem>
            </CommandGroup>
            <CommandGroup heading="Go to">
              {PAGES.map((page) => (
                <CommandItem
                  key={page.href}
                  keywords={page.keywords}
                  onSelect={() => go(page.href)}
                  value={page.label}
                >
                  <page.icon />
                  {page.label}
                  <Keycap className="ml-auto" combo={page.key} />
                </CommandItem>
              ))}
            </CommandGroup>
            {drives?.accounts.some((account) => account.drives.length > 0) && (
              <CommandGroup heading="Drives">
                {drives.accounts.flatMap((account) =>
                  account.drives.map((drive) => (
                    <CommandItem
                      key={drive.id}
                      keywords={[account.name, "drive"]}
                      onSelect={() =>
                        go(`/dashboard?drive=${encodeURIComponent(drive.id)}`)
                      }
                      value={`drive ${drive.id} ${drive.name}`}
                    >
                      <HardDriveIcon />
                      <span className="truncate font-mono">{drive.name}</span>
                      <span className="text-faint ml-auto shrink-0 text-[12px]">
                        {account.name}
                      </span>
                    </CommandItem>
                  ))
                )}
              </CommandGroup>
            )}
            {devices.length > 0 && (
              <CommandGroup heading="Devices">
                {devices.map((device) => (
                  <CommandItem
                    key={device.id}
                    keywords={[device.os, "device"]}
                    onSelect={() =>
                      go(
                        `/dashboard/devices?device=${encodeURIComponent(device.id)}`
                      )
                    }
                    value={`device ${device.id} ${device.name}`}
                  >
                    <MonitorSmartphoneIcon />
                    <span className="truncate font-mono">{device.name}</span>
                    <span className="text-faint ml-auto shrink-0 text-[12px]">
                      {device.os}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
          <div className="palette-foot">
            <span>
              <Keycap combo="arrowup" />
              <Keycap combo="arrowdown" />
              Move
            </span>
            <span>
              <Keycap combo="enter" />
              Run
            </span>
            <span>
              <Keycap combo="esc" />
              Close
            </span>
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
