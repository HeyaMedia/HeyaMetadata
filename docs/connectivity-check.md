# Public connectivity check

HeyaMetadata hosts the stateless outside-in connectivity service used by Heya
media servers. It is deliberately separate from canonical metadata workflows:
it creates no entities, observations, or River jobs.

## Public contract

`GET /v1/ip` returns the caller's observed public source address:

```json
{"ip":"84.23.101.7"}
```

`POST /v1/check` accepts a TCP port and one fresh, short-lived challenge:

```json
{
  "port": 47231,
  "challenge": "3c9f0a1b2d4e5f60718293a4b5c6d7e8"
}
```

The service TLS-dials that port on the request's observed source IP, sends
`GET /api/connectivity/probe` with the literal IP as `Host`, and compares the
returned challenge in constant time. Certificate validity is intentionally
ignored because the media server can still have a self-signed bootstrap
certificate. The leaf SHA-256, SANs, and self-signed state are diagnostic
output, not an identity decision.

A completed negative probe is still HTTP `200`. `reachable` means the
TCP/TLS/HTTP exchange completed; `verified` means the returned challenge also
matched. Failure codes are stable client contract values:

- `timeout`
- `connection_refused`
- `tls_handshake`
- `http_error`
- `challenge_mismatch`
- `same_network` (the checker and caller share a public IP, so an outside-in
  result cannot be determined without risking a NAT hairpin false negative)

Invalid fields or source addresses return `400`; malformed JSON is rejected as
an input-validation `4xx`. Rate or in-flight limits return `429`, a
`Retry-After` header, and:

```json
{"retry_after_seconds":15}
```

## Security boundary

The target IP never appears in the request body. The only possible target is
the resolved source of the current HTTP request, and it must be a global public
unicast address. Private, loopback, link-local, carrier-NAT, documentation,
benchmarking, multicast, and reserved ranges are rejected before a socket is
opened. Hostnames are never accepted or resolved, and redirects are impossible
because the HTTP exchange is written directly over the established TLS socket.

Forwarding headers are ignored unless the immediate peer belongs to
`HEYA_METADATA_CONNECTIVITY_TRUSTED_PROXIES`, a comma-separated CIDR allowlist.
`CF-Connecting-IP` is preferred. Otherwise `X-Forwarded-For` is traversed from
the trusted origin side and the first untrusted hop is selected. The default
allowlist contains loopback and RFC1918 networks for a local/private Cloudflare
tunnel or cluster ingress. If Cloudflare connects directly to the origin, add
Cloudflare's current published edge CIDRs explicitly.

The complete probe has a ten-second deadline; connect plus TLS handshake share
a five-second deadline. Response bodies are capped at 4 KiB. Redis enforces ten
checks per minute, one concurrent check, and sixty IP reads per minute for each
source IP. Rate-limit keys hash the source address and expire automatically.
Challenges are never logged.

At process boot the service obtains its own public egress address from
`HEYA_METADATA_CONNECTIVITY_PUBLIC_IP_ECHO_URL` and refreshes that value hourly.
If a check arrives from that same public address, it skips the network probe and
returns HTTP `200` with `reachable: false`, `verified: false`, and
`error.code: "same_network"`. A failed echo refresh keeps the last successful
address; if no address has ever been resolved, ordinary source-IP-only probing
continues instead of making the connectivity endpoint unavailable.
