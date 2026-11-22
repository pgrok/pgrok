import axios, { isAxiosError } from "axios";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Alert } from "@/components/retroui/Alert";
import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import { cn } from "@/lib/utils";
import { formatDuration } from "../components/LiveTunnels";
import ThemeToggle from "../components/ThemeToggle";
import useUser from "../hooks/useUser";
import { AdminPrincipal, AdminTunnel } from "../types";

const REFRESH_INTERVAL_MS = 3000;

// State-changing admin endpoints only accept JSON as a guard against
// cross-site requests.
const JSON_REQUEST = { headers: { "Content-Type": "application/json" } };

// AdminPage lets admins see all users and active tunnels, block or unblock
// users, and disconnect any tunnel.
export default function AdminPage() {
  const user = useUser();

  return (
    <div className="min-h-screen">
      <nav className="border-b-2 border-border bg-card">
        <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-4 sm:px-6 lg:px-8">
          <Link to="/" className="flex items-center gap-2">
            <img className="h-8 w-auto" src="/pgrok.png" alt="pgrok" />
            <Text as="h4">pgrok</Text>
          </Link>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" render={<Link to="/" />}>
              Dashboard
            </Button>
            <ThemeToggle />
            <Button size="sm" variant="outline" render={<a href="/-/sign-out" />}>
              Sign out
            </Button>
          </div>
        </div>
      </nav>

      <main className="mx-auto max-w-5xl px-4 py-10 sm:px-6 lg:px-8">
        <Text as="h1">Admin</Text>
        {user.isAdmin ? <AdminPanels /> : <NotAuthorized />}
      </main>
    </div>
  );
}

function NotAuthorized() {
  return (
    <Card className="mt-6 block w-full">
      <Card.Header className="border-b-2 border-border">
        <Card.Title>Not authorized</Card.Title>
      </Card.Header>
      <Card.Content className="flex flex-col items-start gap-4">
        <Text>
          You do not have access to the admin view. Ask the server operator to add your account to the{" "}
          <code>admins</code> list in <code>pgrokd.yml</code>.
        </Text>
        <Button size="sm" variant="outline" render={<Link to="/" />}>
          Back to dashboard
        </Button>
      </Card.Content>
    </Card>
  );
}

function AdminPanels() {
  const [principals, setPrincipals] = useState<AdminPrincipal[]>();
  const [tunnels, setTunnels] = useState<AdminTunnel[]>();
  // Kept apart so that the next refresh does not clear the error of an action.
  const [loadError, setLoadError] = useState<string>();
  const [actionError, setActionError] = useState<string>();
  const [forbidden, setForbidden] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const latestRefresh = useRef(0);

  const refresh = useCallback(async () => {
    // A periodic refresh may still be in flight when an action triggers another
    // one, so only the latest refresh is applied.
    const id = ++latestRefresh.current;
    try {
      const [principalsResponse, tunnelsResponse] = await Promise.all([
        axios.get<AdminPrincipal[]>("/api/admin/principals"),
        axios.get<AdminTunnel[]>("/api/admin/tunnels"),
      ]);
      if (id !== latestRefresh.current) {
        return;
      }
      setPrincipals(principalsResponse.data);
      setTunnels(tunnelsResponse.data);
      setLoadError(undefined);
    } catch (error_) {
      if (id !== latestRefresh.current) {
        return;
      }
      if (isAxiosError(error_) && error_.response?.status === 403) {
        setForbidden(true);
      } else {
        setLoadError("Failed to load users and tunnels, retrying shortly.");
      }
    }
    setNow(Date.now());
  }, []);

  useEffect(() => {
    void refresh();
    const refreshTimer = setInterval(() => {
      if (!document.hidden) {
        void refresh();
      }
    }, REFRESH_INTERVAL_MS);
    // Keep the connected durations ticking between refreshes.
    const clockTimer = setInterval(() => setNow(Date.now()), 1000);
    return () => {
      clearInterval(refreshTimer);
      clearInterval(clockTimer);
    };
  }, [refresh]);

  async function setBlocked(principal: AdminPrincipal, blocked: boolean) {
    setActionError(undefined);
    try {
      await axios.patch(`/api/admin/principals/${principal.id}`, { blocked }, JSON_REQUEST);
    } catch (error_) {
      const message = isAxiosError<{ error?: string }>(error_) ? error_.response?.data?.error : undefined;
      setActionError(message ?? `Failed to ${blocked ? "block" : "unblock"} ${principal.displayName}.`);
    }
    await refresh();
  }

  async function deletePrincipal(principal: AdminPrincipal) {
    setActionError(undefined);
    try {
      // A body is required for axios to send the Content-Type header.
      await axios.delete(`/api/admin/principals/${principal.id}`, { ...JSON_REQUEST, data: {} });
    } catch (error_) {
      const message = isAxiosError<{ error?: string }>(error_) ? error_.response?.data?.error : undefined;
      setActionError(message ?? `Failed to delete ${principal.displayName}.`);
    }
    await refresh();
  }

  async function disconnect(id: string) {
    setActionError(undefined);
    try {
      // A body is required for axios to send the Content-Type header.
      await axios.delete(`/api/admin/tunnels/${encodeURIComponent(id)}`, { ...JSON_REQUEST, data: {} });
    } catch {
      setActionError("Failed to disconnect the tunnel, it may have already ended.");
    }
    await refresh();
  }

  if (forbidden) {
    return <NotAuthorized />;
  }

  return (
    <>
      {[actionError, loadError].map(
        (error) =>
          error && (
            <Alert key={error} className="mt-6">
              {error}
            </Alert>
          ),
      )}

      <Card className="mt-6 block w-full">
        <Card.Header className="border-b-2 border-border">
          <Card.Title>Users</Card.Title>
        </Card.Header>
        <Card.Content>
          {principals === undefined ? (
            <Text className="text-sm text-muted-foreground">Loading...</Text>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="border-b-2 border-border">
                    <th className="py-2 pr-4 font-semibold">User</th>
                    <th className="py-2 pr-4 font-semibold">Subdomain</th>
                    <th className="py-2 pr-4 font-semibold">Created</th>
                    <th className="py-2 pr-4 font-semibold">Tunnels</th>
                    <th className="py-2 pr-4 font-semibold">Status</th>
                    <th className="py-2">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y-2 divide-border">
                  {principals.map((principal) => (
                    <PrincipalRow
                      key={principal.id}
                      principal={principal}
                      onSetBlocked={setBlocked}
                      onDelete={deletePrincipal}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card.Content>
      </Card>

      <Card className="mt-6 block w-full">
        <Card.Header className="border-b-2 border-border">
          <Card.Title>All tunnels</Card.Title>
        </Card.Header>
        <Card.Content>
          {tunnels === undefined && <Text className="text-sm text-muted-foreground">Loading...</Text>}
          {tunnels?.length === 0 && <Text className="text-sm text-muted-foreground">No live tunnels.</Text>}
          {tunnels !== undefined && tunnels.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="border-b-2 border-border">
                    <th className="py-2 pr-4 font-semibold">Owner</th>
                    <th className="py-2 pr-4 font-semibold">URL</th>
                    <th className="py-2 pr-4 font-semibold">Protocol</th>
                    <th className="py-2 pr-4 font-semibold">Connected</th>
                    <th className="py-2 pr-4 font-semibold">Client</th>
                    <th className="py-2">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y-2 divide-border">
                  {tunnels.map((tunnel) => (
                    <TunnelRow key={tunnel.id} tunnel={tunnel} now={now} onDisconnect={disconnect} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card.Content>
      </Card>
    </>
  );
}

function PrincipalRow({
  principal,
  onSetBlocked,
  onDelete,
}: {
  principal: AdminPrincipal;
  onSetBlocked: (principal: AdminPrincipal, blocked: boolean) => Promise<void>;
  onDelete: (principal: AdminPrincipal) => Promise<void>;
}) {
  // The confirmed action is fixed when the confirm opens, so that a refresh
  // showing another admin's change cannot flip it.
  const [confirming, setConfirming] = useState<"block" | "unblock" | "delete">();
  const [busy, setBusy] = useState(false);
  const createdAt = new Date(principal.createdAt);

  async function confirmBlock() {
    setBusy(true);
    await onSetBlocked(principal, confirming === "block");
    setBusy(false);
    setConfirming(undefined);
  }

  async function confirmDelete() {
    setBusy(true);
    // The row is removed by the refresh once the user is deleted.
    await onDelete(principal);
    setBusy(false);
    setConfirming(undefined);
  }

  const tunnels = `${principal.activeTunnels} tunnel${principal.activeTunnels === 1 ? "" : "s"}`;
  const confirmingBlock = confirming === "block" || confirming === "unblock";
  const unblocking = confirmingBlock ? confirming === "unblock" : principal.blocked;
  const action = unblocking ? "Unblock" : "Block";
  let question = "Block this user?";
  if (unblocking) {
    question = "Restore access?";
  } else if (principal.activeTunnels > 0) {
    question = `Block and disconnect ${tunnels}?`;
  }

  const dangerButton = "bg-destructive text-destructive-foreground hover:bg-destructive";
  return (
    <>
      <tr className={cn(principal.blocked && "bg-destructive/10", confirming === "delete" && "border-b-0")}>
        <td className="py-3 pr-4">
          <div className="font-semibold">
            {principal.displayName}
            {principal.isSelf && <span className="font-normal text-muted-foreground"> (you)</span>}
          </div>
          <div className="text-muted-foreground">{principal.identifier}</div>
        </td>
        <td className="py-3 pr-4">
          <code className="break-all">{principal.subdomain}</code>
        </td>
        <td className="whitespace-nowrap py-3 pr-4" title={createdAt.toLocaleString()}>
          {createdAt.toLocaleDateString()}
        </td>
        <td className="py-3 pr-4">{principal.activeTunnels}</td>
        <td className="py-3 pr-4">
          <div className="flex gap-1">
            {principal.blocked ? (
              <Tag className="border-destructive bg-destructive text-destructive-foreground">Blocked</Tag>
            ) : (
              <Tag>Active</Tag>
            )}
            {principal.isAdmin && <Tag className="bg-primary text-primary-foreground">Admin</Tag>}
          </div>
        </td>
        {/* The right padding keeps the buttons' offset shadows from being clipped. */}
        <td className="py-3 pr-1">
          {/* Admins cannot block or delete themselves. */}
          {!principal.isSelf && confirmingBlock && (
            <div className="flex items-center justify-end gap-2">
              <span className="text-right">{question}</span>
              <Button
                size="sm"
                className={unblocking ? undefined : dangerButton}
                disabled={busy}
                onClick={() => void confirmBlock()}
              >
                {action}
              </Button>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirming(undefined)}>
                Cancel
              </Button>
            </div>
          )}
          {!principal.isSelf && !confirmingBlock && (
            <div className="flex justify-end gap-2">
              <Button
                size="sm"
                variant="outline"
                title={
                  principal.blocked
                    ? "Allow this user to sign in and connect again with the same token"
                    : "Disconnect this user's tunnels and reject their sign-ins and connections"
                }
                disabled={confirming === "delete"}
                onClick={() => setConfirming(principal.blocked ? "unblock" : "block")}
              >
                {action}
              </Button>
              <Button
                size="sm"
                variant="outline"
                title="Remove this user and disconnect their tunnels"
                disabled={confirming === "delete"}
                onClick={() => setConfirming("delete")}
              >
                Delete
              </Button>
            </div>
          )}
        </td>
      </tr>
      {confirming === "delete" && (
        <tr className={cn(principal.blocked && "bg-destructive/10")}>
          <td colSpan={6} className="pb-3 pr-1">
            <div
              role="alert"
              className="flex flex-col gap-3 rounded border-2 border-destructive px-3 py-2 sm:flex-row sm:items-center sm:justify-between"
            >
              <div>
                <p className="font-semibold">
                  Delete {principal.displayName} ({principal.identifier})? This can&apos;t be undone and disconnects{" "}
                  {tunnels}.
                </p>
                <p className="text-muted-foreground">
                  They get a new account with a new token if they sign in again. To keep them out, block instead.
                </p>
              </div>
              <div className="flex shrink-0 gap-2">
                <Button size="sm" className={dangerButton} disabled={busy} onClick={() => void confirmDelete()}>
                  Delete
                </Button>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirming(undefined)}>
                  Cancel
                </Button>
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function TunnelRow({
  tunnel,
  now,
  onDisconnect,
}: {
  tunnel: AdminTunnel;
  now: number;
  onDisconnect: (id: string) => Promise<void>;
}) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const connectedAt = new Date(tunnel.connectedAt);

  async function confirm() {
    setBusy(true);
    await onDisconnect(tunnel.id);
    setBusy(false);
    setConfirming(false);
  }

  return (
    <tr>
      <td className="py-3 pr-4">
        <div className="font-semibold">{tunnel.ownerDisplayName || `#${tunnel.ownerId}`}</div>
        <div className="break-all text-muted-foreground">{tunnel.ownerIdentifier}</div>
      </td>
      <td className="py-3 pr-4">
        {tunnel.protocol === "http" ? (
          <a
            className="break-all underline decoration-primary underline-offset-2"
            href={tunnel.url}
            target="_blank"
            rel="noreferrer"
          >
            {tunnel.url}
          </a>
        ) : (
          <code className="break-all">{tunnel.url}</code>
        )}
      </td>
      <td className="py-3 pr-4 uppercase">{tunnel.protocol}</td>
      <td className="whitespace-nowrap py-3 pr-4" title={connectedAt.toLocaleString()}>
        {formatDuration(now - connectedAt.getTime())}
      </td>
      <td className="py-3 pr-4">
        <code>{tunnel.remoteAddr}</code>
      </td>
      <td className="py-3 pr-1">
        {confirming ? (
          <div className="flex items-center justify-end gap-2 whitespace-nowrap">
            <span>Disconnect this client?</span>
            <Button
              size="sm"
              className="bg-destructive text-destructive-foreground hover:bg-destructive"
              disabled={busy}
              onClick={() => void confirm()}
            >
              Disconnect
            </Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirming(false)}>
              Cancel
            </Button>
          </div>
        ) : (
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="outline"
              title="Close the connection of this client, it reconnects automatically unless stopped"
              onClick={() => setConfirming(true)}
            >
              Disconnect
            </Button>
          </div>
        )}
      </td>
    </tr>
  );
}

function Tag({ className = "", children }: { className?: string; children: React.ReactNode }) {
  return (
    <span className={cn("rounded border-2 border-border px-1.5 py-0.5 text-xs font-semibold", className)}>
      {children}
    </span>
  );
}
