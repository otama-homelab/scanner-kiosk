CREATE TABLE oidc_start_requests (
 session_hash BLOB NOT NULL CHECK(length(session_hash)=32),
 key_hash BLOB NOT NULL CHECK(length(key_hash)=32),
 configuration_hash BLOB NOT NULL CHECK(length(configuration_hash)=32),
 state_hash BLOB NOT NULL UNIQUE CHECK(length(state_hash)=32),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>created_at_ms),
 PRIMARY KEY(session_hash,key_hash)
) STRICT;
CREATE TRIGGER oidc_start_matches_attempt BEFORE INSERT ON oidc_start_requests
WHEN NOT EXISTS(SELECT 1 FROM auth_attempts a WHERE a.state_hash=NEW.state_hash AND a.session_hash=NEW.session_hash AND a.created_at_ms=NEW.created_at_ms AND a.expires_at_ms=NEW.expires_at_ms)
BEGIN SELECT RAISE(ABORT,'invalid OIDC start attempt'); END;
CREATE TRIGGER oidc_start_immutable BEFORE UPDATE ON oidc_start_requests
BEGIN SELECT RAISE(ABORT,'immutable OIDC start'); END;
CREATE TABLE session_login_grants (
 source_hash TEXT PRIMARY KEY CHECK(length(source_hash)=43),
 grant_nonce TEXT NOT NULL CHECK(length(grant_nonce)=43),
 target_hash TEXT UNIQUE CHECK(target_hash IS NULL OR (length(target_hash)=43 AND target_hash!=source_hash)),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>created_at_ms),
 cancelled_at_ms INTEGER CHECK(cancelled_at_ms IS NULL OR cancelled_at_ms>=created_at_ms)
) STRICT;
CREATE TRIGGER session_grant_immutable BEFORE UPDATE ON session_login_grants
WHEN NEW.source_hash!=OLD.source_hash OR NEW.grant_nonce!=OLD.grant_nonce OR NEW.created_at_ms!=OLD.created_at_ms OR NEW.expires_at_ms!=OLD.expires_at_ms
 OR (OLD.target_hash IS NOT NULL AND NEW.target_hash IS NOT OLD.target_hash)
 OR (OLD.cancelled_at_ms IS NOT NULL AND NEW.cancelled_at_ms IS NOT OLD.cancelled_at_ms)
 OR (OLD.cancelled_at_ms IS NOT NULL AND OLD.target_hash IS NULL AND NEW.target_hash IS NOT NULL)
BEGIN SELECT RAISE(ABORT,'immutable session grant'); END;
