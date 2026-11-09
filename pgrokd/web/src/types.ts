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
