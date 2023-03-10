# pgrok - Poor man's ngrok

## What?

The pgrok is a multi-tenant HTTP reverse tunnel solution through remote port forwarding from the SSH protocol.

This is intended for small teams that need to expose the local development environment to the public internet, and you need to bring your own domain name and SSO provider.

It gives stable subdomain for every user, and gated by your SSO through OIDC protocol.

Think this as a bare-bone alternative to the [ngrok's $65/user/month enterprise tier](https://ngrok.com/pricing). Try to put this behind a production system will blow up your SLA.

For individuals and production systems, just buy ngork, it is still my favorite.

## Why?

Stable subdomains and SSO are two things too expensive.

## Credits

- The [logo](https://www.flaticon.com/free-icon/nat_9168228) is from [flaticon.com](https://www.flaticon.com/).
- The project wouldn't be possible without reading [function61/holepunch-server](https://github.com/function61/holepunch-server), [function61/holepunch-client](https://github.com/function61/holepunch-client), and [TCP/IP Port Forwarding](https://github.com/apache/mina-sshd/blob/master/docs/technical/tcpip-forwarding.md).

## License

This project is under the MIT License. See the [LICENSE](LICENSE) file for the full license text.
