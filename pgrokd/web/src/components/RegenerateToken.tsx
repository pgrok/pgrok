import axios, { isAxiosError } from "axios";
import { useState } from "react";
import { Alert } from "@/components/retroui/Alert";
import { Button } from "@/components/retroui/Button";
import { Dialog, dangerButtonClass } from "@/components/retroui/Dialog";
import useUser from "../hooks/useUser";
import { RegenerateTokenResponse } from "../types";

type State = "confirming" | "pending" | "done";

// RegenerateToken renders the "Regenerate" action for the tunnel token. It asks
// for confirmation in a dialog, which then explains how to update clients.
export default function RegenerateToken() {
  const { token, setToken } = useUser();
  const [open, setOpen] = useState(false);
  // Kept while the dialog closes, so that its content does not change meanwhile.
  const [state, setState] = useState<State>("confirming");
  const [error, setError] = useState("");

  function openDialog() {
    setState("confirming");
    setError("");
    setOpen(true);
  }

  async function regenerate() {
    setState("pending");
    setError("");
    try {
      // The server requires a JSON Content-Type as a CSRF guard, which axios sets
      // for the object body.
      const response = await axios.post<RegenerateTokenResponse>("/api/regenerate-token", {});
      setToken(response.data.token);
      setState("done");
    } catch (error_) {
      const message = isAxiosError<RegenerateTokenResponse>(error_) ? error_.response?.data?.error : undefined;
      setError(message || "Failed to regenerate token, please try again.");
      setState("confirming");
    }
  }

  const pending = state === "pending";
  return (
    <>
      <Button size="sm" variant="outline" onClick={openDialog}>
        Regenerate
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => !pending && setOpen(next)}
        title={state === "done" ? "Token regenerated" : "Regenerate your token?"}
        footer={
          state === "done" ? (
            <Button size="sm" onClick={() => setOpen(false)}>
              Done
            </Button>
          ) : (
            <>
              <Button size="sm" variant="outline" disabled={pending} onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button size="sm" className={dangerButtonClass} disabled={pending} onClick={() => void regenerate()}>
                {pending ? "Regenerating..." : "Regenerate token"}
              </Button>
            </>
          )
        }
      >
        {state === "done" ? (
          <>
            <p>The dashboard now shows your new token.</p>
            <p>
              Replace the <code className={codeClass}>token</code> value in{" "}
              <code className={codeClass}>pgrok.yml</code> with it, or pass{" "}
              <code className={codeClass}>--token {token}</code> to clients started with that flag. Re-running{" "}
              <code className={codeClass}>pgrok init ... --token {token}</code> also works, but rewrites the whole{" "}
              <code className={codeClass}>pgrok.yml</code>.
            </p>
          </>
        ) : (
          <>
            <p>
              The current token stops working immediately. Update to the new token: the{" "}
              <code className={codeClass}>token</code> value in <code className={codeClass}>pgrok.yml</code>, or the{" "}
              <code className={codeClass}>--token</code> flag for clients started with it.
            </p>
            <p>Tunnels that are already connected are not cut off.</p>
            {error && <Alert>{error}</Alert>}
          </>
        )}
      </Dialog>
    </>
  );
}

const codeClass = "rounded border border-border bg-background px-1 font-mono text-xs break-all";
