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
 * A user as returned by the `/api/admin/principals`.
 */
export interface AdminPrincipal {
  id: number;
  displayName: string;
  identifier: string;
  subdomain: string;
  createdAt: string;
  blocked: boolean;
  isAdmin: boolean;
  /**
   * Whether it is the signed-in admin.
   */
  isSelf: boolean;
  activeTunnels: number;
}

/**
 * An active tunnel with its owner as returned by the `/api/admin/tunnels`.
 */
export interface AdminTunnel extends Tunnel {
  ownerId: number;
  ownerDisplayName: string;
  ownerIdentifier: string;
}
