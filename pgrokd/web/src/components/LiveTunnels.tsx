import axios from "axios";
import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import { Tunnel } from "../types";

const REFRESH_INTERVAL_MS = 3000;

// LiveTunnels lists the signed-in user's active tunnels, refreshing
// periodically, and lets the user disconnect any of them.
export default function LiveTunnels() {
  const [tunnels, setTunnels] = useState<Tunnel[]>();
  const [error, setError] = useState<string>();
  const [now, setNow] = useState(() => Date.now());

  const refresh = useCallback(async () => {
    try {
      const response = await axios.get<Tunnel[]>("/api/tunnels");
      setTunnels(response.data);
      setError(undefined);
    } catch {
      setError("Failed to load live tunnels, retrying shortly.");
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

  async function disconnect(id: string) {
    try {
      await axios.delete(`/api/tunnels/${encodeURIComponent(id)}`);
    } catch {
      setError("Failed to disconnect the tunnel, it may have already ended.");
    }
    await refresh();
  }

  return (
    <Card className="mt-6 block w-full">
      <Card.Header className="border-b-2 border-border">
        <Card.Title>Live tunnels</Card.Title>
      </Card.Header>
      <Card.Content>
        {error && (
          <p role="alert" className="mb-4 rounded border-2 border-destructive px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        )}
        {tunnels === undefined && !error && <Text className="text-sm text-muted-foreground">Loading...</Text>}
        {tunnels?.length === 0 && <EmptyState />}
        {tunnels !== undefined && tunnels.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b-2 border-border">
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
  );
}

function TunnelRow({
  tunnel,
  now,
  onDisconnect,
}: {
  tunnel: Tunnel;
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
      {/* The right padding keeps the buttons' offset shadows from being clipped. */}
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

function EmptyState() {
  return (
    <div className="flex flex-col gap-2 text-sm">
      <Text className="font-semibold">No live tunnels.</Text>
      <Text className="text-sm text-muted-foreground">
        Start one with the <code>pgrok</code> client using the token above, for example:
      </Text>
      <pre className="overflow-x-auto rounded border-2 border-border bg-background px-3 py-2">
        <code>{"pgrok http 3000\npgrok tcp 5432"}</code>
      </pre>
    </div>
  );
}

// formatDuration renders a millisecond duration compactly, e.g. "2h 5m" or
// "42s".
function formatDuration(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) {
    return `${days}d ${hours}h`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds % 60}s`;
  }
  return `${seconds}s`;
}
