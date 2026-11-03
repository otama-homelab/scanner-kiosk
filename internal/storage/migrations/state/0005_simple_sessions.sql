CREATE TABLE simple_session_credentials (
 session_hash TEXT PRIMARY KEY CHECK(length(session_hash)=43) REFERENCES sessions(token) ON DELETE CASCADE,
 principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 credential_version INTEGER NOT NULL CHECK(credential_version>=1)
) STRICT;
CREATE INDEX simple_session_principal ON simple_session_credentials(principal_id);
CREATE TRIGGER simple_session_current BEFORE INSERT ON simple_session_credentials
WHEN NOT EXISTS(SELECT 1 FROM local_credentials c JOIN principals p ON p.id=c.principal_id AND p.kind='simple' AND p.disabled_at_ms IS NULL
 WHERE c.principal_id=NEW.principal_id AND c.credential_version=NEW.credential_version)
BEGIN SELECT RAISE(ABORT,'stale local credential'); END;
CREATE TRIGGER simple_session_immutable BEFORE UPDATE ON simple_session_credentials
BEGIN SELECT RAISE(ABORT,'immutable session credential'); END;
CREATE TRIGGER simple_session_refresh_current BEFORE UPDATE ON sessions
WHEN EXISTS(SELECT 1 FROM simple_session_credentials b WHERE b.session_hash=OLD.token)
 AND NOT EXISTS(SELECT 1 FROM simple_session_credentials b JOIN local_credentials c ON c.principal_id=b.principal_id AND c.credential_version=b.credential_version
 JOIN principals p ON p.id=c.principal_id AND p.kind='simple' AND p.disabled_at_ms IS NULL WHERE b.session_hash=OLD.token)
BEGIN SELECT RAISE(ABORT,'stale local session'); END;
