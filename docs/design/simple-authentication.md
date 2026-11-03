# Local username/password authentication

Status: local credential service, SQLite adapter, account command, password HTTP login, shared session-grant fencing and bilingual frontend are implemented. Select simple explicitly with SCANNER_AUTHENTICATE_TYPE=simple. Named none remains pending; it is not an alias for legacy guest.

## Service and database boundary

`authport.Password` returns an internal principal plus credential version. `simpleauth.Service` verifies credentials through a backend-neutral `Records` port. It has no SQL, HTTP, cookie or OIDC dependency. The SQLite adapter uses explicit parameterized SQL for lookup, atomic principal/account creation, password replacement and enabled/version recheck. Hash calculation holds no database transaction. There is no ORM or public self-registration endpoint.

Names are NFC-normalized and case-folded identifiers, 1–64 Unicode characters and at most 256 UTF-8 bytes. Letters/digits are allowed; underscore, dot and hyphen are allowed after the first character. Names are not silently trimmed. Passwords are not trimmed, case-folded or Unicode-normalized. Creation/reset requires at least 15 Unicode characters, a maximum of 1024 UTF-8 bytes and no NUL/CR/LF. Verification is bounded to 1024 bytes. No character-class composition rule is imposed.

The initial PHC profile is Argon2id v19, m=19456 KiB, t=2, p=1, with a random 16-byte salt and 32-byte result. This uses the existing pinned `golang.org/x/crypto` dependency. The [Go package documentation](https://pkg.go.dev/golang.org/x/crypto@v0.58.0/argon2) describes IDKey; the selected memory/iteration/parallelism combination meets the corresponding [OWASP password-storage recommendation](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html). Encoded stored values cannot request arbitrary KDF parameters: unsupported versions, malformed/noncanonical base64, lengths or profiles fail before hash calculation.

One shared service instance permits at most one hash operation concurrently and rejects excess work with ErrLimit rather than building a queue. Missing names still run the same fixed-profile calculation against a random dummy record. Existing and unknown wrong credentials both return ErrDenied. Comparisons use constant-time equality for fixed-size derived keys. The service rechecks enabled state and credential version after successful calculation, so a concurrent reset/disable yields no identity. Cancellation and unavailable/corrupt storage yield no identity. These component properties do not replace the pending HTTP login request/rate limits.

`PasswordHash` formats and JSON-marshals as redacted. Only its explicit Encoded method exposes the PHC string for persistence. Plaintext passwords are never inserted into SQL or printed. Passwords are passed transiently as input; the implementation does not claim complete erasure of all Go string copies from memory.

## Account administration

The compiled command now supports:

```sh
SCANNER_STATE_DIR=/private/scanner/state scanner user create alice < /run/secrets/password-input
SCANNER_STATE_DIR=/private/scanner/state scanner user set-password alice < /run/secrets/password-input
```

The secret path is an operator-supplied input example, not a file created or configured by the application. A pipe from a password manager works as well. The password is read from standard input, bounded to the allowed password plus one optional LF/CRLF ending, and never accepted as a command argument. Spaces are preserved. The command reads only SCANNER_STATE_DIR, requires an existing private non-symlink state directory, and does not load API/OIDC/worker settings or start HTTP/native processing. Success is printed only after the repository command returns success. A duplicate normalized name conflicts rather than overwriting credentials or creating an orphan principal.

Reset preserves the principal ID and increments credential_version atomically. It does not enable a disabled account. Creation/reset uncertainty returns no successful result; repeating creation cannot replace an already committed account. The command is an operator action on the state database, not an unprivileged browser administration function. API session issuance now checks the principal/type/credential version in both the source rotation and final atomic grant transaction; a PasswordResult by itself grants no cookie or authorization. Run account commands as the application OS user against its private state directory.

## State migration 0004

The principal kind constraint now admits future none/simple identities as well as existing oidc/guest. The migration preserves the principal table and its three dependent identity/guest/cleanup tables by copying and rebuilding them within the existing single transaction. Foreign-key enforcement stays enabled. Existing sessions, revocations, OIDC attempts/start receipts and login grants are untouched; original kind/identity/guest immutability triggers are recreated. New local_credentials records are tied only to simple principals and contain normalized name, hash, credential version and timestamps. Account identity is immutable and updates require exactly the next version.

Upgrade tests seed a genuine version-3 state database with OIDC/guest mappings, a disabled principal, cleanup intent, session bytes, revocation, attempt/start and successor grant, then open it with the current product initializer. They verify row preservation, foreign-key checks, original identity constraints, local credential constraints and reopen idempotence. A deliberate table collision late in migration proves rollback restores the original version-3 schema/data without partial migration tables. Older executables must continue to reject this newer schema; backward schema migration is not implemented.

## Evidence and limits

Tests cover real Argon2id verification, random salts, malformed/cost-amplifying profiles, Unicode/password input bounds, duplicate/concurrent creation, reset, old password refusal, disable, reset/disable during verification, uncertain commit, bounded work, cancellation and unavailable storage. The real compiled `scanner user` command creates/resets a private account without any OIDC/API environment and its resulting record authenticates through the service. Store/command tests also check no plaintext canary appears in the state database/WAL or command output. The non-SQL HTTP/session injection proof is documented separately in [HTTP sessions](http-sessions.md).

An initial local benchmark on Linux/amd64, Intel i5-12400, Go 1.27.3, three non-race iterations measured approximately 27.8 ms/op, 19,925,565 allocated bytes/op and 32 allocations/op. This is development-host evidence, not container/homelab resource acceptance or a login latency guarantee. Reproduce with `go test ./internal/simpleauth -run '^$' -bench BenchmarkPasswordVerification -benchtime=3x -count=1`.

The session verification receipt includes local credential/admin/migration and HTTP login/grant-race tests. The browser receipt separately includes simple_auth for the compiled command/API and actual username/password form. None-mode same-name history behavior remains a separate pending user decision; no account linking to OIDC, existing guests or a same-name none identity occurs here.


## HTTP login, configuration and session fencing

Use SCANNER_AUTHENTICATE_TYPE=oidc or simple. The old SCANNER_AUTH_MODE key remains accepted when the canonical key is absent; providing both fails startup even if the values agree. Legacy guest is available only through the old key and retains isolated-session semantics. Canonical none is rejected until the pending same-name history contract is settled; neither OIDC failure nor an unsupported type selects guest. The session JSON field is now authenticate_type (auth_mode is removed), and the embedded frontend/API contract were updated together. Simple mode does not read OIDC configuration or create provider clients. Worker configuration reads none of these authentication settings.

POST /auth/simple/login requires an active anonymous Secure cookie, exact Origin, valid explicit operation key and a bounded JSON object containing only username/password. Duplicate, unknown, case-variant, null and trailing fields are rejected. Credentials never enter a URL or an idempotency cache. Wrong credentials return a generic rejection without a new cookie. Existing authenticated sessions must sign out rather than silently switch users. There is no automatic credential retry following an unknown response.

OIDC and simple share establishLogin and the existing one-source/one-successor session rotation protocol. Both source rotation and final commit verify the enabled principal and exact kind in the state transaction. Simple additionally verifies the PasswordResult credential version. State migration 0005 adds simple_session_credentials keyed to the committed hashed session token; its mapping and version are immutable, linked to the simple principal and deleted with the session. A stale version cannot refresh its session. Bootstrap, protected routes and streaming authorization recheck the binding/current credential version. Password reset therefore invalidates old simple sessions without disabling the principal or touching another account. In-flight operations authorized before a reset are not globally rolled back; streams revalidate through their existing authorization checks.

Logout accepts OIDC and simple, revokes the current session or a source cookie's hidden successor, and keeps owned documents/jobs under their original TTL. Tests inject reset before rotation/commit, disable, logout and a committed-but-failed response: none may issue a success cookie. An old source cookie can revoke the committed-but-unacknowledged successor and its credential binding. This preserves the prior OIDC callback/logout guarantees.

A separate process-wide login token bucket is configured by SCANNER_AUTH_LOGIN_REQUESTS_PER_MINUTE (default 10, range 1–600) and SCANNER_AUTH_LOGIN_BURST (default 5, range 1–20). It does not trust client/forwarding addresses and applies across names/browser cookies. Excess attempts return 429/Retry-After. Existing API rate/inflight bounds and the shared single-KDF service gate also apply. These limits are separate from deployment load acceptance.

## Frontend and reproducible browser proof

The login form is an isolated login.ts component with explicit labeled username/current-password fields, native validation, Enter submission, pending lock and a 15-second abort. It clears the password on success, rejection, timeout or view disposal; no password is stored in localStorage/sessionStorage. Browser password-manager autocomplete remains supported. A lost or missing cookie can be reset and re-bootstrapped without replaying credentials. The form uses the existing neutral/indigo theme and English/Japanese messages. Changing locale while submitting aborts/disposes the pending view and never replays its password.

check-web.py provisions Alice/Bob through the actual compiled stdin command into separate private stores, runs a real simple API behind the fixture TLS proxy, and executes check-web-simple.cjs with a fresh browser context. It proves labeled English-default responsive input, rejection/clearing, successful login and history, reload without password storage, cross-user refusal, sign-out, missing-cookie recovery, bilingual labels and pending-abort/no-replay. The history row is a synthetic document fixture; this is authentication/owner/browser evidence, not scan capture or real mobile hardware acceptance.
