"use client";

import { TriangleAlertIcon } from "lucide-react";
import { Action } from "../kit/action";
import { ShellDialog } from "../kit/dialog";

interface RemoveDriveProps {
  account: string;
  name: string;
  onConfirm: () => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
}

export function RemoveDriveDialog({
  account,
  name,
  onConfirm,
  onOpenChange,
  open,
}: RemoveDriveProps) {
  return (
    <ShellDialog
      actions={
        <Action combo="d" tone="fill" type="submit">
          Remove drive
        </Action>
      }
      description={`Every device finishes its uploads, then unmounts it. Your files stay in ${account}.`}
      focusCancel
      heading={`Remove ${name}?`}
      icon={TriangleAlertIcon}
      keys={{ d: onConfirm }}
      onOpenChange={onOpenChange}
      onSubmit={onConfirm}
      open={open}
      title="Remove drive"
      tone="danger"
    />
  );
}
