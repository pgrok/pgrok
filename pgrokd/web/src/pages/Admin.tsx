import axios, { isAxiosError } from "axios";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
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
  const [error, setError] = useState<string>();
  const [forbidden, setForbidden] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const refresh = useCallback(async () => {
    try {
      const [principalsResponse, tunnelsResponse] = await Promise.all([
        axios.get<AdminPrincipal[]>("/api/admin/principals"),
        axios.get<AdminTunnel[]>("/api/admin/tunnels"),
      ]);
      setPrincipals(principalsResponse.data);
      setTunnels(tunnelsResponse.data);
      setError(undefined);
    } catch (error_) {
      if (isAxiosError(error_) && error_.response?.status === 403) {
        setForbidden(true);
      } else {
        setError("Failed to load users and tunnels, retrying shortly.");
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
    try {
      await axios.patch(`/api/admin/principals/${principal.id}`, { blocked }, JSON_REQUEST);
    } catch (error_) {
      const message = isAxiosError<{ error?: string }>(error_) ? error_.response?.data?.error : undefined;
      setError(message ?? `Failed to ${blocked ? "block" : "unblock"} ${principal.displayName}.`);
    }
    await refresh();
  }

  async function disconnect(id: string) {
    try {
      // A body is required for axios to send the Content-Type header.
      await axios.delete(`/api/admin/tunnels/${encodeURIComponent(id)}`, { ...JSON_REQUEST, data: {} });
    } catch {
      setError("Failed to disconnect the tunnel, it may have already ended.");
    }
    await refresh();
  }

  if (forbidden) {
    return <NotAuthorized />;
  }

  return (
    <>
      {error && (
        <p role="alert" className="mt-6 rounded border-2 border-destructive px-3 py-2 text-sm text-destructive">
          {error}
        </p>
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
                    <PrincipalRow key={principal.id} principal={principal} onSetBlocked={setBlocked} />
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
}: {
  principal: AdminPrincipal;
  onSetBlocked: (principal: AdminPrincipal, blocked: boolean) => Promise<void>;
}) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const createdAt = new Date(principal.createdAt);

  async function confirm() {
    setBusy(true);
    await onSetBlocked(principal, !principal.blocked);
    setBusy(false);
    setConfirming(false);
  }

  const action = principal.blocked ? "Unblock" : "Block";
  let question = "Block this user?";
  if (principal.blocked) {
    question = "Restore access?";
  } else if (principal.activeTunnels > 0) {
    question = `Block and disconnect ${principal.activeTunnels} tunnel${principal.activeTunnels === 1 ? "" : "s"}?`;
  }

  return (
    <tr className={principal.blocked ? "bg-destructive/10" : undefined}>
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
        {/* Admins cannot block themselves. */}
        {!principal.isSelf && confirming && (
          <div className="flex items-center justify-end gap-2">
            <span className="text-right">{question}</span>
            <Button
              size="sm"
              className={
                principal.blocked ? undefined : "bg-destructive text-destructive-foreground hover:bg-destructive"
              }
              disabled={busy}
              onClick={() => void confirm()}
            >
              {action}
            </Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => setConfirming(false)}>
              Cancel
            </Button>
          </div>
        )}
        {!principal.isSelf && !confirming && (
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="outline"
              title={
                principal.blocked
                  ? "Allow this user to sign in and connect again with the same token"
                  : "Disconnect this user's tunnels and reject their sign-ins and connections"
              }
              onClick={() => setConfirming(true)}
            >
              {action}
            </Button>
          </div>
        )}
      </td>
    </tr>
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
    <span className={cn("rounded border-2 border-border px-1.5 py-0.5 text-xs font-semibold", className)}>{children}</span>
  );
}
