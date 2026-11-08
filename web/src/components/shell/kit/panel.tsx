import type { LucideIcon } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { cn } from "@/lib/utils";

interface PanelProps {
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
  delay?: number;
  icon: LucideIcon;
  title: ReactNode;
  tone?: "danger";
}

export function PanelEdge() {
  return (
    <svg aria-hidden className="panel-edge">
      <rect pathLength={1} rx="10" x="0.5" y="0.5" />
    </svg>
  );
}

export function Panel({
  aside,
  children,
  className,
  delay = 0,
  icon: Icon,
  title,
  tone,
}: PanelProps) {
  return (
    <section
      className={cn("panel", className)}
      data-tone={tone}
      style={{ "--d": `${delay}ms` } as CSSProperties}
    >
      <PanelEdge />
      <h2 className="panel-title">
        <Icon aria-hidden className="panel-icon" />
        <span className="min-w-0 truncate">{title}</span>
      </h2>
      {aside != null && <span className="panel-aside">{aside}</span>}
      {children}
    </section>
  );
}
