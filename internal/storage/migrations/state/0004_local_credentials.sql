-- Rebuild the principal and its three dependent tables in the same migration
-- transaction. Foreign keys remain enabled; child rows are copied before drop.
CREATE TABLE principals_next (
 id TEXT PRIMARY KEY CHECK(length(id)>=16),
 kind TEXT NOT NULL CHECK(kind IN ('oidc','guest','none','simple')),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 disabled_at_ms INTEGER CHECK(disabled_at_ms IS NULL OR disabled_at_ms>=created_at_ms)
) STRICT;
INSERT INTO principals_next SELECT * FROM principals;
CREATE TABLE migration_oidc_identities AS SELECT * FROM oidc_identities;
CREATE TABLE migration_guest_starts AS SELECT * FROM guest_starts;
CREATE TABLE migration_principal_cleanup AS SELECT * FROM principal_cleanup_requests;
DROP TABLE oidc_identities;
DROP TABLE guest_starts;
DROP TABLE principal_cleanup_requests;
DROP TABLE principals;
ALTER TABLE principals_next RENAME TO principals;
CREATE TRIGGER principal_identity_immutable BEFORE UPDATE ON principals
WHEN NEW.id!=OLD.id OR NEW.kind!=OLD.kind OR NEW.created_at_ms!=OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'immutable principal identity'); END;
CREATE TABLE oidc_identities (
 issuer TEXT NOT NULL CHECK(length(issuer)>0),
 subject TEXT NOT NULL CHECK(length(subject)>0),
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id) ON DELETE RESTRICT,
 PRIMARY KEY(issuer,subject)
) STRICT;
INSERT INTO oidc_identities SELECT * FROM migration_oidc_identities;
CREATE TRIGGER oidc_identity_kind BEFORE INSERT ON oidc_identities
WHEN NOT EXISTS(SELECT 1 FROM principals WHERE id=NEW.principal_id AND kind='oidc')
BEGIN SELECT RAISE(ABORT,'invalid principal kind'); END;
CREATE TRIGGER oidc_identity_immutable BEFORE UPDATE ON oidc_identities
BEGIN SELECT RAISE(ABORT,'immutable OIDC identity'); END;
CREATE TABLE guest_starts (
 binding_hash BLOB PRIMARY KEY CHECK(length(binding_hash)=32),
 key_hash BLOB NOT NULL CHECK(length(key_hash)=32),
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id) ON DELETE RESTRICT,
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>created_at_ms)
) STRICT;
INSERT INTO guest_starts SELECT * FROM migration_guest_starts;
CREATE TRIGGER guest_start_kind BEFORE INSERT ON guest_starts
WHEN NOT EXISTS(SELECT 1 FROM principals p WHERE p.id=NEW.principal_id AND p.kind='guest')
BEGIN SELECT RAISE(ABORT,'invalid guest principal'); END;
CREATE TRIGGER guest_start_immutable BEFORE UPDATE ON guest_starts
BEGIN SELECT RAISE(ABORT,'immutable guest start'); END;
CREATE TABLE principal_cleanup_requests (
 principal_id TEXT PRIMARY KEY REFERENCES principals(id) ON DELETE RESTRICT,
 reason TEXT NOT NULL CHECK(reason IN ('guest_end','principal_disabled')),
 requested_at_ms INTEGER NOT NULL,
 completed_at_ms INTEGER CHECK(completed_at_ms IS NULL OR completed_at_ms>=requested_at_ms)
) STRICT;
INSERT INTO principal_cleanup_requests SELECT * FROM migration_principal_cleanup;
DROP TABLE migration_oidc_identities;
DROP TABLE migration_guest_starts;
DROP TABLE migration_principal_cleanup;

CREATE TABLE local_credentials (
 username TEXT PRIMARY KEY CHECK(length(username)>=1 AND length(username)<=256),
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id) ON DELETE RESTRICT,
 password_hash TEXT NOT NULL CHECK(length(password_hash)>=80 AND length(password_hash)<=256),
 credential_version INTEGER NOT NULL CHECK(credential_version>=1),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms)
) STRICT;
CREATE TRIGGER local_credential_kind BEFORE INSERT ON local_credentials
WHEN NOT EXISTS(SELECT 1 FROM principals WHERE id=NEW.principal_id AND kind='simple')
BEGIN SELECT RAISE(ABORT,'invalid local principal'); END;
CREATE TRIGGER local_credential_update BEFORE UPDATE ON local_credentials
WHEN NEW.username!=OLD.username OR NEW.principal_id!=OLD.principal_id OR NEW.created_at_ms!=OLD.created_at_ms
 OR NEW.updated_at_ms<OLD.updated_at_ms OR NEW.credential_version!=OLD.credential_version+1
BEGIN SELECT RAISE(ABORT,'invalid credential revision'); END;
