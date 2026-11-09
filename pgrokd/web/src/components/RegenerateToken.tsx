import { Button } from "@/components/retroui/Button";
import axios, { isAxiosError } from "axios";
import { useState } from "react";
import useUser from "../hooks/useUser";

type State = "idle" | "confirming" | "pending" | "done";

// RegenerateToken renders the "Regenerate" action for the tunnel token, with an
// in-page confirmation and a notice explaining how to update clients afterwards.
// It is meant to sit inside a wrapping flex row next to the token so the panels
// break onto their own line.
export default function RegenerateToken() {
  const { token, setToken } = useUser();
  const [state, setState] = useState<State>("idle");
  const [error, setError] = useState("");

  async function regenerate() {
    setState("pending");
    setError("");
    try {
      // Sending a JSON body is required by the server as a CSRF guard.
      const response = await axios.post<{ token: string }>("/api/regenerate-token", {});
      setToken(response.data.token);
      setState("done");
    } catch (error_) {
      const message = isAxiosError<{ error?: string }>(error_) ? error_.response?.data?.error : undefined;
      setError(message || "Failed to regenerate token, please try again.");
      setState("confirming");
    }
  }

  if (state === "idle") {
    return (
      <Button size="sm" variant="outline" onClick={() => setState("confirming")}>
        Regenerate
      </Button>
    );
  }

  if (state === "done") {
    return (
      <Panel role="status" tone="success">
        <p className="font-semibold">Token regenerated. The new token is shown above.</p>
        <p className="mt-2">
          Update every pgrok client before it next connects: run <code className={codeClass}>pgrok init</code> again
          with <code className={codeClass}>--token {token}</code>, or replace the{" "}
          <code className={codeClass}>token</code> value in <code className={codeClass}>pgrok.yml</code>.
        </p>
        <div className="mt-3">
          <Button size="sm" variant="outline" onClick={() => setState("idle")}>
            Dismiss
          </Button>
        </div>
      </Panel>
    );
  }

  return (
    <Panel role="alertdialog" aria-labelledby="regenerate-token-title" tone="warning">
      <p id="regenerate-token-title" className="font-semibold">
        Regenerate your token?
      </p>
      <p className="mt-2">
        The current token stops working immediately. pgrok clients still using it will fail to connect or reconnect
        until you update them: run <code className={codeClass}>pgrok init ... --token</code> with the new token, or
        edit the <code className={codeClass}>token</code> value in <code className={codeClass}>pgrok.yml</code>.
      </p>
      <p className="mt-2">
        Tunnels that are already connected are not cut off: they keep running with the old token until they
        disconnect on their own.
      </p>
      {error && (
        <p role="alert" className="mt-2 font-semibold text-destructive">
          {error}
        </p>
      )}
      <div className="mt-3 flex gap-2">
        <Button
          size="sm"
          className="border-black bg-destructive text-destructive-foreground hover:bg-destructive"
          disabled={state === "pending"}
          onClick={() => void regenerate()}
        >
          {state === "pending" ? "Regenerating..." : "Regenerate token"}
        </Button>
        <Button size="sm" variant="outline" disabled={state === "pending"} onClick={() => setState("idle")}>
          Cancel
        </Button>
      </div>
    </Panel>
  );
}

const codeClass = "rounded border border-border bg-background px-1 font-mono text-xs break-all";

function Panel({
  tone,
  children,
  ...properties
}: { tone: "warning" | "success"; children: React.ReactNode } & React.HTMLAttributes<HTMLDivElement>) {
  const border = tone === "warning" ? "border-destructive" : "border-border";
  return (
    <div className={`mt-2 w-full rounded border-2 ${border} bg-background p-3 text-sm shadow`} {...properties}>
      {children}
    </div>
  );
}
