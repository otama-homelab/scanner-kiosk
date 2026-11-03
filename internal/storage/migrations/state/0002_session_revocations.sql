CREATE TABLE session_revocations (
 token TEXT PRIMARY KEY CHECK(length(token)=43),
 revoked_at_ms INTEGER NOT NULL CHECK(revoked_at_ms>=0),
 retain_until_ms INTEGER NOT NULL CHECK(retain_until_ms>revoked_at_ms)
) STRICT;
CREATE INDEX session_revocations_retention ON session_revocations(retain_until_ms);
CREATE TRIGGER session_not_revoked BEFORE INSERT ON sessions
WHEN NEW.expiry<=julianday('now') OR EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=NEW.token)
BEGIN SELECT RAISE(ABORT,'revoked session'); END;
CREATE TRIGGER session_update_not_revoked BEFORE UPDATE ON sessions
WHEN NEW.expiry<=julianday('now') OR EXISTS(SELECT 1 FROM session_revocations r WHERE r.token=NEW.token)
BEGIN SELECT RAISE(ABORT,'revoked session'); END;
CREATE TABLE guest_starts (
 binding_hash BLOB PRIMARY KEY CHECK(length(binding_hash)=32),
 key_hash BLOB NOT NULL CHECK(length(key_hash)=32),
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id) ON DELETE RESTRICT,
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>created_at_ms)
) STRICT;
CREATE TRIGGER guest_start_kind BEFORE INSERT ON guest_starts
WHEN NOT EXISTS(SELECT 1 FROM principals p WHERE p.id=NEW.principal_id AND p.kind='guest')
BEGIN SELECT RAISE(ABORT,'invalid guest principal'); END;
CREATE TRIGGER guest_start_immutable BEFORE UPDATE ON guest_starts
BEGIN SELECT RAISE(ABORT,'immutable guest start'); END;
