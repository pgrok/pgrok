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
