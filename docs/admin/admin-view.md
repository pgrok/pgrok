# Admin view

Admins can see and manage all users and their active tunnels from the "Admin" page of the dashboard (`/admin`).

## Configure admins

List the admins in `pgrokd.yml` by their identifiers, i.e., the value of the claim configured as `identity_provider.field_mapping.identifier` (emails in the example config):

```yaml
admins:
  - "alice@example.com"
  - "bob@example.com"
```

Restart `pgrokd` to apply changes. Admins see an "Admin" link next to "Sign out" on the dashboard.

## Users

The "Users" table lists everyone who has signed in, with their subdomain, creation date, number of active tunnels, and whether they are blocked.

Blocking a user:

- Disconnects all of their active tunnels immediately.
- Rejects their token when `pgrok` (or a vanilla SSH client) connects, so `pgrok` exits with an authentication error instead of reconnecting.
- Signs them out of the dashboard and prevents them from signing in again until unblocked.

Unblocking a user restores access with the same token, they only need to restart `pgrok`. Admins cannot block themselves.

Deleting a user, e.g., someone who has been removed from the identity provider:

- Removes their account and token, which cannot be undone.
- Disconnects all of their active tunnels immediately, and `pgrok` exits with an authentication error instead of reconnecting.
- Signs them out of the dashboard.

Deletion is not a ban. If the person can still sign in to the identity provider, they get a fresh account with a new token (and the same subdomain) on their next sign-in. To keep them out, block them instead. Likewise, deleting a user whose identifier is listed in `admins` does not remove their admin rights; remove them from `admins` for that. Admins cannot delete themselves.

## All tunnels

The "All tunnels" table lists every active tunnel with its owner, public URL, protocol, connected duration and client address. Use "Disconnect" to close a tunnel's connection on the server side. Note that `pgrok` reconnects automatically, so block the user to keep them out.

Per-user traffic statistics are not shown here.
