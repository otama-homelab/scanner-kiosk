-- Legacy keys have no provable age; NULL rows remain permanent tombstones.
ALTER TABLE request_receipts ADD COLUMN key_expires_at_ms INTEGER
 CHECK(key_expires_at_ms IS NULL OR key_expires_at_ms>=86400000);
CREATE TRIGGER request_key_expiry_immutable BEFORE UPDATE OF key_expires_at_ms ON request_receipts
 WHEN NEW.key_expires_at_ms IS NOT OLD.key_expires_at_ms
 BEGIN SELECT RAISE(ABORT,'request key expiry is immutable'); END;
CREATE TABLE request_retirement_fence (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 retired_at_ms INTEGER NOT NULL CHECK(retired_at_ms>=0)
) STRICT;
INSERT INTO request_retirement_fence(singleton,retired_at_ms) VALUES(1,0);
CREATE TRIGGER request_retirement_monotonic BEFORE UPDATE ON request_retirement_fence
 WHEN NEW.singleton!=OLD.singleton OR NEW.retired_at_ms<OLD.retired_at_ms
 BEGIN SELECT RAISE(ABORT,'request retirement fence cannot decrease'); END;
CREATE TRIGGER request_retirement_no_delete BEFORE DELETE ON request_retirement_fence
 BEGIN SELECT RAISE(ABORT,'request retirement fence cannot be deleted'); END;
CREATE INDEX request_receipts_aged_retirement ON request_receipts(expires_at_ms,key_expires_at_ms,owner_id,key_hash)
 WHERE key_expires_at_ms IS NOT NULL;
