-- Trusted reconciliation authority, separate from read-only native metadata.
CREATE TABLE scanner_readiness (
 device_id TEXT PRIMARY KEY REFERENCES device_leases(device_id) ON DELETE CASCADE,
 boot_id TEXT NOT NULL CHECK(length(boot_id)>=16),
 generation INTEGER NOT NULL CHECK(generation>=1),
 lease_epoch INTEGER NOT NULL CHECK(lease_epoch>=1),
 physical_id TEXT NOT NULL UNIQUE CHECK(length(physical_id)>=16),
 handoff_id TEXT NOT NULL CHECK(length(handoff_id)>=16),
 scan_concurrency INTEGER NOT NULL CHECK(scan_concurrency BETWEEN 1 AND 32),
 ownership_confirmed_at_ms INTEGER NOT NULL CHECK(ownership_confirmed_at_ms>=0),
 local_stopped_at_ms INTEGER NOT NULL CHECK(local_stopped_at_ms>=ownership_confirmed_at_ms),
 idle_observed_at_ms INTEGER NOT NULL CHECK(idle_observed_at_ms>=local_stopped_at_ms),
 published_at_ms INTEGER NOT NULL CHECK(published_at_ms>=idle_observed_at_ms),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>published_at_ms)
) STRICT;
