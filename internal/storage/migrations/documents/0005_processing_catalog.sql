-- Processing metadata authority only. This is not scanner readiness or idle proof.
CREATE TABLE processing_coordinator (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 boot_id TEXT NOT NULL CHECK(length(boot_id)>=16),
 generation INTEGER NOT NULL CHECK(generation>=1),
 state TEXT NOT NULL CHECK(state IN ('active','stopped')),
 created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
 heartbeat_at_ms INTEGER NOT NULL CHECK(heartbeat_at_ms>=created_at_ms),
 valid_until_ms INTEGER NOT NULL CHECK(valid_until_ms>heartbeat_at_ms)
) STRICT;
CREATE TABLE ocr_catalog_snapshot (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 boot_id TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation>=1),
 published_at_ms INTEGER NOT NULL CHECK(published_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>published_at_ms),
 payload TEXT NOT NULL CHECK(length(CAST(payload AS BLOB))<=1048576 AND json_valid(payload) AND json_type(payload)='object')
) STRICT;
CREATE TRIGGER processing_generation BEFORE UPDATE ON processing_coordinator
WHEN NEW.singleton!=OLD.singleton OR NEW.generation<OLD.generation OR NEW.generation>OLD.generation+1
 OR (NEW.generation=OLD.generation AND (NEW.boot_id!=OLD.boot_id OR NEW.created_at_ms!=OLD.created_at_ms OR NEW.heartbeat_at_ms<OLD.heartbeat_at_ms OR (OLD.state='stopped' AND NEW.state!='stopped')))
 OR (NEW.generation!=OLD.generation AND NEW.boot_id=OLD.boot_id)
BEGIN SELECT RAISE(ABORT,'invalid processing generation'); END;
