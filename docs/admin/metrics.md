# Prometheus metrics

pgrokd can expose [Prometheus](https://prometheus.io) metrics at `/metrics` on a dedicated port. It is disabled by default, enable it by setting the port in `pgrokd.yml`:

```yaml
metrics:
  port: 3390
```

> [!IMPORTANT]
> The metrics server listens on all interfaces but is meant for your monitoring system only. Do **not** expose the port to the public (e.g. via your reverse proxy or network security policy), because metric labels contain the subdomains of your users.

Then add a scrape config to Prometheus:

```yaml
scrape_configs:
  - job_name: "pgrokd"
    static_configs:
      - targets: ["localhost:3390"]
```

## Available metrics

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| `pgrokd_http_requests_total` | Counter | `subdomain`, `method`, `code` | HTTP requests proxied through HTTP tunnels. `code` is the status class (e.g. `2xx`), `method` is `OTHER` for non-standard methods. |
| `pgrokd_http_request_duration_seconds` | Histogram | `subdomain` | Duration of HTTP requests proxied through HTTP tunnels. |
| `pgrokd_http_bytes_total` | Counter | `subdomain`, `direction` | HTTP body bytes proxied through HTTP tunnels, `in` for request bodies and `out` for response bodies. |
| `pgrokd_tcp_tunnel_bytes_total` | Counter | `subdomain`, `direction` | Bytes proxied through TCP tunnels, `in` from visitors to the tunnel client and `out` for the reverse. |
| `pgrokd_tunnels_active` | Gauge | `protocol` | Active tunnels (`http` or `tcp`). |
| `pgrokd_ssh_connections_active` | Gauge | | Active authenticated SSH connections. |
| `pgrokd_ssh_auth_failures_total` | Counter | | Rejected SSH token authentication attempts. |

The standard Go runtime (`go_*`) and process (`process_*`) metrics are also exposed.

The `subdomain` label is the stable subdomain of the user who owns the tunnel (e.g. `unknwon` for both `unknwon.example.com` and `staging-unknwon.example.com`), so there is at most one value per user. Requests to hosts that have no active tunnel are not counted.
