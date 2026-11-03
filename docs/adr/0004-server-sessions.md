# ADR 0004: SCS sessions with revocation-aware SQLite storage

Use unmodified SCS v2.9.0 for server sessions, with the existing modernc SQLite
state database. Keep Cookie data opaque, hash tokens in storage, and keep identity
and session state out of the temporary document database.

The earlier SQLite-store spike verified persistence and rotation but did not
fence a concurrent save after deletion. The product's SCS store adapter therefore
uses atomic revocation/deletion plus database triggers to reject stale writes.
This is application integration code; no SCS or upstream SQLite package is patched.

Session/guest components and tests are described in [HTTP sessions](../design/http-sessions.md).
OIDC authentication and the serving runtime remain pending. The initial release
requires their integration, cleanup/resource limits and browser acceptance.
