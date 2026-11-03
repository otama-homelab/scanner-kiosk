-- Native metadata only; this creates no scanner lease or readiness state.
CREATE TABLE scanner_metadata_snapshot (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 boot_id TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation>=1),
 published_at_ms INTEGER NOT NULL CHECK(published_at_ms>=0),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>published_at_ms),
 payload TEXT NOT NULL CHECK(length(CAST(payload AS BLOB))<=1048576 AND json_valid(payload) AND json_type(payload)='object')
) STRICT;
