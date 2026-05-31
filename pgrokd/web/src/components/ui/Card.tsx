import { HTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/utilities";

const Card = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(({ className, ...properties }, reference) => (
  <div
    ref={reference}
    className={cn("rounded-[--radius-retro] border-2 border-border bg-card shadow-retro", className)}
    {...properties}
  />
));
Card.displayName = "Card";

const CardHeader = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  ({ className, ...properties }, reference) => (
    <div ref={reference} className={cn("flex flex-col gap-1 border-b-2 border-border p-5", className)} {...properties} />
  ),
);
CardHeader.displayName = "CardHeader";

const CardTitle = forwardRef<HTMLHeadingElement, HTMLAttributes<HTMLHeadingElement>>(
  ({ className, ...properties }, reference) => (
    <h3 ref={reference} className={cn("font-head text-lg leading-tight", className)} {...properties} />
  ),
);
CardTitle.displayName = "CardTitle";

const CardContent = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  ({ className, ...properties }, reference) => (
    <div ref={reference} className={cn("p-5", className)} {...properties} />
  ),
);
CardContent.displayName = "CardContent";

export { Card, CardHeader, CardTitle, CardContent };
