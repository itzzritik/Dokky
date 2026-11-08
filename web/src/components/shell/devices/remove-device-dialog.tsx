"use client";

import { TriangleAlertIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { Action } from "../kit/action";
import { ShellDialog } from "../kit/dialog";

interface RemoveDeviceProps {
  accounts: string[];
  lostAtOpen: boolean;
  name: string;
  offline: boolean;
  onConfirm: (lost: boolean) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
}

export function RemoveDeviceDialog({
  accounts,
  lostAtOpen,
  name,
  offline,
  onConfirm,
  onOpenChange,
  open,
}: RemoveDeviceProps) {
  const [lost, setLost] = useState(lostAtOpen);
  useEffect(() => {
    if (open) {
      setLost(lostAtOpen);
    }
  }, [open, lostAtOpen]);
  const keeps =
    accounts.length > 0
      ? ` Until then it keeps its sign-ins for ${accounts.join(", ")}.`
      : "";
  return (
    <ShellDialog
      actions={
        <Action combo="d" tone="fill" type="submit">
          Remove device
        </Action>
      }
      description={
        lost
          ? "It erases right away. Files it hasn't uploaded are lost."
          : "It signs out and erases its dokky data once its uploads finish. Your files stay in your storage."
      }
      focusCancel
      heading={`Remove ${name}?`}
      icon={TriangleAlertIcon}
      keys={{ d: () => onConfirm(lost) }}
      onOpenChange={onOpenChange}
      onSubmit={() => onConfirm(lost)}
      open={open}
      title="Remove device"
      tone="danger"
    >
      {offline && (
        <p className="text-muted-foreground text-[13px]">{`It's offline, so it erases itself when it next comes online.${keeps}`}</p>
      )}
      <label className="mt-1 flex w-fit cursor-pointer items-center gap-2.5 text-[13.5px]">
        <Checkbox
          checked={lost}
          onCheckedChange={(checked) => setLost(checked === true)}
        />
        Lost or stolen
      </label>
      {lost && (
        <div className="warn-box">
          <b>!</b>
          <span>You may need to sign in to GitHub again.</span>
        </div>
      )}
    </ShellDialog>
  );
}
