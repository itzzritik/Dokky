"use client";

import { type ComponentProps, type ReactNode, useState } from "react";
import { cn } from "@/lib/utils";

type FieldProps = Omit<ComponentProps<"input">, "onChange" | "value"> & {
  invalid?: boolean;
  onValueChange: (value: string) => void;
  value: string;
};

export function FieldLabel({
  children,
  className,
  htmlFor,
}: {
  children: ReactNode;
  className?: string;
  htmlFor: string;
}) {
  return (
    <label
      className={cn(
        "text-muted-foreground block font-mono text-[12px] font-medium",
        className
      )}
      htmlFor={htmlFor}
    >
      {children}
    </label>
  );
}

function Field({
  children,
  className,
  invalid,
  onValueChange,
  ...props
}: FieldProps & { children?: ReactNode }) {
  return (
    <div
      className={cn("field", className)}
      data-invalid={invalid ? "" : undefined}
    >
      <input
        aria-invalid={invalid}
        className="field-input"
        onChange={(event) => onValueChange(event.target.value)}
        spellCheck={false}
        {...props}
      />
      {children}
    </div>
  );
}

export function TextField(props: FieldProps) {
  return <Field type="text" {...props} />;
}

export function PasswordField(props: FieldProps) {
  const [shown, setShown] = useState(false);
  return (
    <Field
      autoComplete="current-password"
      {...props}
      type={shown ? "text" : "password"}
    >
      <button
        aria-label={shown ? "Hide password" : "Show password"}
        aria-pressed={shown}
        className="field-toggle"
        onClick={() => setShown(!shown)}
        type="button"
      >
        {shown ? "hide" : "show"}
      </button>
    </Field>
  );
}
