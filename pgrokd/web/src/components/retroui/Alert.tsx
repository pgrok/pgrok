import { cn } from "@/lib/utils";
import { HTMLAttributes } from "react";

// Alert is the solid RetroUI error box used for error messages.
export const Alert = ({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) => {
  return (
    <p
      role="alert"
      className={cn(
        "rounded border-2 border-black bg-destructive px-3 py-2 font-head text-sm text-destructive-foreground shadow-md",
        className,
      )}
      {...props}
    />
  );
};
