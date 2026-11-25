import { AlertDialog } from "@base-ui/react/alert-dialog";
import { ReactNode } from "react";

// Dialog is the RetroUI modal used to confirm actions. It overlays the page
// instead of expanding inline, so the layout does not shift. It only closes
// through onOpenChange (Escape or a footer button), never on outside clicks.
// Put the safe action (e.g., "Cancel") first in the footer: it gets the
// initial focus.
export function Dialog({
  open,
  onOpenChange,
  title,
  children,
  footer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  children: ReactNode;
  footer: ReactNode;
}) {
  return (
    <AlertDialog.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-40 bg-black/50 transition-opacity duration-150 data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <AlertDialog.Popup className="fixed left-1/2 top-1/2 z-50 w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded border-2 border-border bg-card p-5 text-card-foreground shadow-lg transition-all duration-150 data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0">
          <AlertDialog.Title className="font-head text-xl">{title}</AlertDialog.Title>
          <AlertDialog.Description render={<div />} className="mt-3 flex flex-col gap-2 text-sm">
            {children}
          </AlertDialog.Description>
          <div className="mt-5 flex flex-wrap justify-end gap-2">{footer}</div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}

// The class for the button of a destructive action.
export const dangerButtonClass = "border-black bg-destructive text-destructive-foreground hover:bg-destructive";
