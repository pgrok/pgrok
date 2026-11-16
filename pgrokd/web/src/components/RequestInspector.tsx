import axios, { isAxiosError } from "axios";
import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/retroui/Button";
import { Card } from "@/components/retroui/Card";
import { Text } from "@/components/retroui/Text";
import { InspectedRequest, InspectedRequestDetails } from "../types";

const REFRESH_INTERVAL_MS = 2000;

// RequestInspector lists the latest requests to the signed-in user's HTTP
// tunnels, refreshing periodically, and shows the details of the selected one
// with the option to replay it.
export default function RequestInspector() {
  const [requests, setRequests] = useState<InspectedRequest[]>();
  const [disabled, setDisabled] = useState(false);
  const [error, setError] = useState<string>();
  const [selectedId, setSelectedId] = useState<string>();

  const refresh = useCallback(async () => {
    try {
      const response = await axios.get<InspectedRequest[]>("/api/requests");
      setRequests(response.data);
      setError(undefined);
    } catch (error_) {
      if (isAxiosError(error_) && error_.response?.status === 404) {
        setDisabled(true);
        return;
      }
      setError("Failed to load requests, retrying shortly.");
    }
  }, []);

  useEffect(() => {
    if (disabled) {
      return;
    }
    void refresh();
    const timer = setInterval(() => {
      if (!document.hidden) {
        void refresh();
      }
    }, REFRESH_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [refresh, disabled]);

  function onReplayed(replay: InspectedRequest) {
    setSelectedId(replay.id);
    void refresh();
  }

  return (
    <Card className="mt-6 block w-full">
      <Card.Header className="border-b-2 border-border">
        <Card.Title>Requests</Card.Title>
        <Card.Description className="text-sm">
          The latest HTTP requests to your tunnels. Credential headers are redacted.
        </Card.Description>
      </Card.Header>
      <Card.Content>
        {disabled && (
          <Text className="text-sm text-muted-foreground">
            The request inspector is disabled on this server. Admins can enable it with{" "}
            <code>proxy.inspector: true</code> in <code>pgrokd.yml</code>.
          </Text>
        )}
        {error && (
          <p role="alert" className="mb-4 rounded border-2 border-destructive px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        )}
        {!disabled && requests === undefined && !error && (
          <Text className="text-sm text-muted-foreground">Loading...</Text>
        )}
        {!disabled && requests?.length === 0 && (
          <div className="flex flex-col gap-2 text-sm">
            <Text className="font-semibold">No requests yet.</Text>
            <Text className="text-sm text-muted-foreground">
              Requests to your HTTP tunnels show up here as they happen, try visiting one of your tunnel URLs.
            </Text>
          </div>
        )}
        {!disabled && requests !== undefined && requests.length > 0 && (
          <>
            <RequestList requests={requests} selectedId={selectedId} onSelect={setSelectedId} />
            {selectedId && <RequestDetails key={selectedId} id={selectedId} onReplayed={onReplayed} />}
          </>
        )}
      </Card.Content>
    </Card>
  );
}

function RequestList({
  requests,
  selectedId,
  onSelect,
}: {
  requests: InspectedRequest[];
  selectedId?: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="max-h-80 overflow-auto rounded border-2 border-border">
      <table className="w-full text-left text-sm">
        <thead className="sticky top-0 bg-card">
          <tr className="border-b-2 border-border">
            <th className="px-3 py-2 font-semibold">Time</th>
            <th className="py-2 pr-3 font-semibold">Method</th>
            <th className="py-2 pr-3 font-semibold">Path</th>
            <th className="py-2 pr-3 font-semibold">Status</th>
            <th className="py-2 pr-3 text-right font-semibold">Duration</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {requests.map((request) => {
            const selected = request.id === selectedId;
            return (
              <tr
                key={request.id}
                aria-selected={selected}
                className={`cursor-pointer ${selected ? "bg-accent text-accent-foreground" : "hover:bg-accent/40"}`}
                onClick={() => onSelect(request.id)}
              >
                <td className="whitespace-nowrap px-3 py-2" title={new Date(request.startedAt).toLocaleString()}>
                  {new Date(request.startedAt).toLocaleTimeString()}
                </td>
                <td className="py-2 pr-3 font-semibold">{request.method}</td>
                <td className="max-w-md py-2 pr-3">
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      className="min-w-0 cursor-pointer truncate text-left font-mono focus-visible:outline-2 focus-visible:outline-primary"
                      title={`${request.host}${request.path}`}
                      onClick={() => onSelect(request.id)}
                    >
                      {request.path}
                    </button>
                    {request.replayOf && <Tag>replay</Tag>}
                  </div>
                </td>
                <td className={`py-2 pr-3 ${statusClassName(request.status, selected)}`}>{request.status || "-"}</td>
                <td className="whitespace-nowrap py-2 pr-3 text-right">{formatDuration(request.durationMs)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function RequestDetails({ id, onReplayed }: { id: string; onReplayed: (replay: InspectedRequest) => void }) {
  const [details, setDetails] = useState<InspectedRequestDetails>();
  const [error, setError] = useState<string>();
  const [replaying, setReplaying] = useState(false);

  useEffect(() => {
    axios
      .get<InspectedRequestDetails>(`/api/requests/${encodeURIComponent(id)}`)
      .then((response) => setDetails(response.data))
      .catch(() => setError("Failed to load the request, it may have been evicted."));
  }, [id]);

  async function replay() {
    setReplaying(true);
    setError(undefined);
    try {
      // The JSON body is required by the server to prevent cross-site requests.
      const response = await axios.post<InspectedRequestDetails>(`/api/requests/${encodeURIComponent(id)}/replay`, {});
      onReplayed(response.data);
    } catch (error_) {
      const message = isAxiosError<{ error?: string }>(error_) ? error_.response?.data?.error : undefined;
      setError(`Failed to replay the request${message ? `: ${message}` : "."}`);
    }
    setReplaying(false);
  }

  if (!details) {
    return error ? (
      <ErrorMessage message={error} />
    ) : (
      <Text className="mt-4 text-sm text-muted-foreground">Loading...</Text>
    );
  }

  const upgrade = headerValue(details.requestHeaders, "Upgrade") !== "";
  const replayable = !details.requestBodyTruncated && !upgrade;
  return (
    <section aria-label="Request details" className="mt-4 rounded border-2 border-border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 text-sm">
          <code className="break-all font-semibold">
            {details.method} {details.host}
            {details.path}
          </code>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 text-muted-foreground">
            <span className={statusClassName(details.status, false)}>{details.status || "No response"}</span>
            <span>{formatDuration(details.durationMs)}</span>
            <span>{new Date(details.startedAt).toLocaleString()}</span>
            {details.replayOf && <Tag>replay</Tag>}
          </div>
        </div>
        <div className="pr-1">
          <Button
            size="sm"
            disabled={replaying || !replayable}
            title={
              replayable
                ? "Send this request again through the same tunnel"
                : "Requests with incompletely captured bodies or upgraded connections cannot be replayed"
            }
            onClick={() => void replay()}
          >
            {replaying ? "Replaying..." : "Replay"}
          </Button>
        </div>
      </div>
      {error && <ErrorMessage message={error} />}
      <div className="mt-4 grid gap-4 md:grid-cols-2">
        <Message
          title="Request"
          headers={details.requestHeaders}
          body={details.requestBody}
          truncated={details.requestBodyTruncated}
        />
        <Message
          title="Response"
          headers={details.responseHeaders}
          body={details.responseBody}
          truncated={details.responseBodyTruncated}
          streaming={details.streaming}
        />
      </div>
    </section>
  );
}

function Message({
  title,
  headers,
  body,
  truncated,
  streaming = false,
}: {
  title: string;
  headers: Record<string, string[]> | null;
  body: string | null;
  truncated: boolean;
  streaming?: boolean;
}) {
  const names = Object.keys(headers ?? {});
  return (
    <div className="min-w-0">
      <Text as="h6" className="mb-2">
        {title}
      </Text>
      <div className="overflow-x-auto rounded border-2 border-border">
        <table className="w-full text-left font-mono text-xs">
          <tbody className="divide-y divide-border">
            {names.length === 0 && (
              <tr>
                <td className="px-2 py-1 text-muted-foreground">No headers</td>
              </tr>
            )}
            {names.flatMap((name) =>
              (headers?.[name] ?? []).map((value, index) => (
                <tr key={`${name}-${index}`}>
                  <td className="whitespace-nowrap px-2 py-1 align-top font-semibold">{name}</td>
                  <td className="break-all px-2 py-1">{value}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      </div>
      <div className="mt-2">
        {streaming ? (
          <Text className="text-xs text-muted-foreground">
            The body of streamed responses and upgraded connections is not captured.
          </Text>
        ) : (
          <Body
            base64={body ?? ""}
            contentType={headerValue(headers, "Content-Type")}
            encoding={headerValue(headers, "Content-Encoding")}
            truncated={truncated}
          />
        )}
      </div>
    </div>
  );
}

function Body({
  base64,
  contentType,
  encoding,
  truncated,
}: {
  base64: string;
  contentType: string;
  encoding: string;
  truncated: boolean;
}) {
  // Undefined while decoding, and null when the body is not text.
  const [text, setText] = useState<string | null>();

  useEffect(() => {
    decodeBody(base64, contentType, encoding, truncated)
      .then(setText)
      // eslint-disable-next-line unicorn/no-null
      .catch(() => setText(null));
  }, [base64, contentType, encoding, truncated]);

  if (base64 === "") {
    return <Text className="text-xs text-muted-foreground">No body</Text>;
  }
  return (
    <>
      {text === null ? (
        <Text className="text-xs text-muted-foreground">
          Binary or compressed body ({atob(base64).length.toLocaleString()} bytes)
        </Text>
      ) : (
        <pre className="max-h-96 overflow-auto rounded border-2 border-border bg-background px-3 py-2 text-xs">
          <code>{text}</code>
        </pre>
      )}
      {truncated && <Text className="mt-1 text-xs text-muted-foreground">Truncated to the first 64 KiB.</Text>}
    </>
  );
}

// decodeBody returns the body as text, with JSON pretty-printed, or null if it
// is not UTF-8 text.
async function decodeBody(
  base64: string,
  contentType: string,
  encoding: string,
  truncated: boolean,
): Promise<string | null> {
  let bytes = Uint8Array.from(atob(base64), (char) => char.codePointAt(0) ?? 0);
  if ((encoding === "gzip" || encoding === "deflate") && !truncated) {
    const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream(encoding));
    bytes = new Uint8Array(await new Response(stream).arrayBuffer());
  }

  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    // eslint-disable-next-line unicorn/no-null
    return null;
  }
  if (contentType.includes("json") || /^\s*[[{]/.test(text)) {
    try {
      return JSON.stringify(JSON.parse(text), undefined, 2);
    } catch {
      // Not valid JSON (e.g. truncated), show it as is.
    }
  }
  return text;
}

function headerValue(headers: Record<string, string[]> | null, name: string): string {
  return headers?.[name]?.[0] ?? "";
}

function statusClassName(status: number, selected: boolean): string {
  return status >= 400 && !selected ? "font-semibold text-destructive" : "font-semibold";
}

function Tag({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-block shrink-0 rounded border-2 border-border bg-primary px-1 text-xs font-semibold text-primary-foreground">
      {children}
    </span>
  );
}

function ErrorMessage({ message }: { message: string }) {
  return (
    <p role="alert" className="mt-4 rounded border-2 border-destructive px-3 py-2 text-sm text-destructive">
      {message}
    </p>
  );
}

// formatDuration renders a millisecond duration compactly, e.g. "12 ms" or
// "1.5 s".
function formatDuration(ms: number): string {
  if (ms < 1000) {
    return `${ms < 10 ? ms.toFixed(1) : Math.round(ms)} ms`;
  }
  return `${(ms / 1000).toFixed(1)} s`;
}
