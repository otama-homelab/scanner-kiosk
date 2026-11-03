CREATE TABLE principals (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 kind TEXT NOT NULL CHECK(kind IN ('oidc','guest')),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 disabled_at_ms INTEGER CHECK(disabled_at_ms IS NULL OR disabled_at_ms>=created_at_ms)
) STRICT;
CREATE TABLE oidc_identities (
 issuer TEXT NOT NULL CHECK(length(issuer)>0),
 subject TEXT NOT NULL CHECK(length(subject)>0),
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id) ON DELETE RESTRICT,
 PRIMARY KEY(issuer,subject)
) STRICT;
CREATE TRIGGER oidc_identity_kind BEFORE INSERT ON oidc_identities
WHEN NOT EXISTS(SELECT 1 FROM principals WHERE id=NEW.principal_id AND kind='oidc')
BEGIN SELECT RAISE(ABORT,'invalid principal kind'); END;
CREATE TABLE sessions (
 token TEXT PRIMARY KEY,
 data BLOB NOT NULL,
 expiry REAL NOT NULL
) STRICT;
CREATE INDEX sessions_expiry ON sessions(expiry);
CREATE TABLE auth_attempts (
 state_hash BLOB PRIMARY KEY CHECK(length(state_hash)=32),
 session_hash BLOB NOT NULL CHECK(length(session_hash)=32),
 nonce TEXT NOT NULL CHECK(length(nonce)>=16),
 code_verifier TEXT NOT NULL CHECK(length(code_verifier)>=43),
 created_at_ms INTEGER NOT NULL,
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>created_at_ms),
 consumed_at_ms INTEGER CHECK(consumed_at_ms IS NULL OR consumed_at_ms>=created_at_ms)
) STRICT;
CREATE INDEX auth_attempts_expiry ON auth_attempts(expires_at_ms);
CREATE TABLE principal_cleanup_requests (
 principal_id TEXT PRIMARY KEY REFERENCES principals(id) ON DELETE RESTRICT,
 reason TEXT NOT NULL CHECK(reason IN ('guest_end','principal_disabled')),
 requested_at_ms INTEGER NOT NULL,
 completed_at_ms INTEGER CHECK(completed_at_ms IS NULL OR completed_at_ms>=requested_at_ms)
) STRICT;
CREATE TRIGGER principal_identity_immutable BEFORE UPDATE ON principals
WHEN NEW.id!=OLD.id OR NEW.kind!=OLD.kind OR NEW.created_at_ms!=OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable principal identity'); END;
CREATE TRIGGER oidc_identity_immutable BEFORE UPDATE ON oidc_identities
BEGIN SELECT RAISE(ABORT,'immutable OIDC identity'); END;
CREATE TRIGGER auth_attempt_immutable BEFORE UPDATE ON auth_attempts
WHEN NEW.state_hash!=OLD.state_hash OR NEW.session_hash!=OLD.session_hash OR NEW.nonce!=OLD.nonce OR NEW.code_verifier!=OLD.code_verifier OR NEW.created_at_ms!=OLD.created_at_ms OR NEW.expires_at_ms!=OLD.expires_at_ms OR OLD.consumed_at_ms IS NOT NULL
BEGIN SELECT RAISE(ABORT,'immutable authorization attempt'); END;
