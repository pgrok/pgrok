/**
 * The response type of the `/api/identity-provider`.
 */
export interface FetchIdentityProviderResponse {
  /**
   * The potential error returned by the server.
   */
  error: string;

  displayName: string;
  authURL: string;
}

/**
 * The response type of the `/api/regenerate-token`.
 */
export interface RegenerateTokenResponse {
  /**
   * The potential error returned by the server.
   */
  error: string;

  token: string;
}

/**
 * An active tunnel as returned by the `/api/tunnels`.
 */
export interface Tunnel {
  id: string;
  protocol: "http" | "tcp";
  url: string;
  remoteAddr: string;
  /**
   * The RFC 3339 timestamp of when the tunnel was established.
   */
  connectedAt: string;
}

/**
 * A recorded request as listed by the `/api/requests`.
 */
export interface InspectedRequest {
  id: string;
  tunnelId: string;
  /**
   * The ID of the replayed request, only set for replays.
   */
  replayOf?: string;
  method: string;
  host: string;
  /**
   * The path and query of the request.
   */
  path: string;
  /**
   * The response status code, 0 when no response was sent.
   */
  status: number;
  /**
   * The RFC 3339 timestamp of when the request was received.
   */
  startedAt: string;
  durationMs: number;
  /**
   * Whether the response was streamed or upgraded the connection, whose body
   * is not captured.
   */
  streaming: boolean;
}

/**
 * A recorded request as returned by the `/api/requests/{id}`. Credential
 * headers are redacted, and bodies are base64-encoded.
 */
export interface InspectedRequestDetails extends InspectedRequest {
  requestHeaders: Record<string, string[]> | null;
  requestBody: string | null;
  requestBodyTruncated: boolean;
  responseHeaders: Record<string, string[]> | null;
  responseBody: string | null;
  responseBodyTruncated: boolean;
}
