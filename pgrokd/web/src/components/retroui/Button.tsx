import { cn } from "@/lib/utils";
import { cva, VariantProps } from "class-variance-authority";
import React, { ButtonHTMLAttributes } from "react";
import { Button as BaseButton } from "@base-ui/react/button";

export const buttonVariants = cva(
  "font-head transition-all rounded cursor-pointer duration-200 font-medium flex justify-center items-center disabled:opacity-60 disabled:cursor-not-allowed focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
  {
    variants: {
      variant: {
        default:
          "shadow-md hover:shadow active:shadow-none bg-primary text-primary-foreground border-2 border-black transition hover:translate-y-1 active:translate-y-2 active:translate-x-1 hover:bg-primary-hover",
        secondary:
          "shadow-md hover:shadow active:shadow-none bg-secondary shadow-primary text-secondary-foreground border-2 border-black transition hover:translate-y-1 active:translate-y-2 active:translate-x-1 hover:bg-secondary-hover",
        outline:
          "shadow-md hover:shadow active:shadow-none bg-transparent border-2 transition hover:translate-y-1 active:translate-y-2 active:translate-x-1",
        link: "bg-transparent hover:underline",
        ghost: "bg-transparent hover:bg-accent"
      },
      size: {
        sm: "px-3 py-1 text-sm shadow hover:shadow-sm",
        md: "px-4 py-1.5 text-base",
        lg: "px-6 lg:px-8 py-2 lg:py-3 text-md lg:text-lg",
        icon: "p-2",
      },
    },
    defaultVariants: {
      size: "md",
      variant: "default",
    },
  },
);

export interface IButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  render?: React.ReactElement | ((props: Record<string, any>) => React.ReactElement);
}

export const Button = ({
  children,
  size = "md",
  className = "",
  variant = "default",
  render,
  ref,
  ...props
}: IButtonProps & { ref?: React.Ref<HTMLButtonElement> }) => {
  return (
    <BaseButton
      ref={ref}
      className={cn(buttonVariants({ variant, size }), className)}
      render={render}
      {...props}
    >
      {children}
    </BaseButton>
  );
};
