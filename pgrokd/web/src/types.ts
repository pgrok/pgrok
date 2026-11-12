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
