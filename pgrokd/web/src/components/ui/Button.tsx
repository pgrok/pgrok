import { type VariantProps, cva } from "class-variance-authority";
import { ButtonHTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/utilities";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 rounded-[--radius-retro] border-2 border-border font-head text-sm font-medium transition-all focus:outline-none active:translate-x-[2px] active:translate-y-[2px] active:shadow-none disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-foreground shadow-retro hover:bg-primary-hover",
        outline: "bg-card text-foreground shadow-retro hover:bg-background",
        link: "border-transparent shadow-none underline-offset-4 hover:underline",
      },
      size: {
        default: "h-11 px-5 py-2",
        sm: "h-9 px-3",
        lg: "h-12 px-8 text-base",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProperties
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {}

const Button = forwardRef<HTMLButtonElement, ButtonProperties>(
  ({ className, variant, size, ...properties }, reference) => {
    return <button ref={reference} className={cn(buttonVariants({ variant, size }), className)} {...properties} />;
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
