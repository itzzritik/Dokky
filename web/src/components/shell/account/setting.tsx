import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

export function Setting({
  action,
  icon: Icon,
  note,
  title,
  tone,
}: {
  action?: ReactNode;
  icon: LucideIcon;
  note: ReactNode;
  title: ReactNode;
  tone?: "success" | "warning";
}) {
  return (
    <div className="setting" data-tone={tone}>
      <span aria-hidden className="setting-icon">
        <Icon />
      </span>
      <p>
        {title}
        <small>{note}</small>
      </p>
      {action}
    </div>
  );
}
