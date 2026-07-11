# RustFS lifecycle

The shared `heyamedia` bucket has two expiration rules:

| Prefix | Expiration |
| --- | --- |
| `data/ephemeral/24h/` | 1 day after object creation |
| `data/ephemeral/48h/` | 2 days after object creation |

No rule matches `images/`, `data/blobs/`, or any other bucket prefix. Object
Lock retention is unrelated and remains disabled; these are lifecycle expiry
rules on an unversioned bucket.

[`heyamedia-lifecycle.json`](./heyamedia-lifecycle.json) is an export of the
live bucket configuration. An administrator can restore it with:

```bash
mc ilm rule import ALIAS/heyamedia < ops/rustfs/heyamedia-lifecycle.json
mc ilm rule ls ALIAS/heyamedia
```

Application credentials intentionally do not have lifecycle-administration
permission. Use the RustFS administrative credentials only for rule changes.
