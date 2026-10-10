CREATE TABLE packlist_versions (
 id TEXT PRIMARY KEY, location_id TEXT NOT NULL REFERENCES locations(id),
 version INTEGER NOT NULL CHECK(version>0), title TEXT NOT NULL,
 check_valid_hours INTEGER NOT NULL CHECK(check_valid_hours BETWEEN 1 AND 168),
 user_id TEXT NOT NULL REFERENCES users(id), created_at TEXT NOT NULL, note TEXT NOT NULL,
 UNIQUE(location_id,version)
);
CREATE TABLE packlist_items (
 packlist_id TEXT NOT NULL REFERENCES packlist_versions(id), product_id TEXT NOT NULL REFERENCES product_types(id),
 quantity INTEGER NOT NULL CHECK(typeof(quantity)='integer' AND quantity BETWEEN 1 AND 1000000000),
 min_remaining_days INTEGER NOT NULL CHECK(min_remaining_days BETWEEN 0 AND 3650),
 PRIMARY KEY(packlist_id,product_id)
);
CREATE TABLE readiness_checks (
 id TEXT PRIMARY KEY, packlist_id TEXT NOT NULL REFERENCES packlist_versions(id),
 user_id TEXT NOT NULL REFERENCES users(id), created_at TEXT NOT NULL,
 basis_hash TEXT NOT NULL, snapshot_json TEXT NOT NULL,
 physical_passed INTEGER NOT NULL CHECK(physical_passed IN (0,1)), note TEXT NOT NULL
);
CREATE TABLE packlist_events (
 id TEXT PRIMARY KEY, actor_id TEXT NOT NULL REFERENCES users(id),
 action TEXT NOT NULL CHECK(action IN ('packlist_published','material_checked')),
 packlist_id TEXT NOT NULL REFERENCES packlist_versions(id),
 check_id TEXT REFERENCES readiness_checks(id), created_at TEXT NOT NULL,
 CHECK((action='packlist_published' AND check_id IS NULL) OR (action='material_checked' AND check_id IS NOT NULL))
);
CREATE TABLE packlist_outbox (
 event_id TEXT PRIMARY KEY REFERENCES packlist_events(id), delivered_at TEXT
);
CREATE TABLE packlist_requests (
 user_id TEXT NOT NULL REFERENCES users(id), key TEXT NOT NULL,
 request_hash TEXT NOT NULL, event_id TEXT NOT NULL REFERENCES packlist_events(id), response_json TEXT NOT NULL,
 PRIMARY KEY(user_id,key)
);
CREATE INDEX readiness_by_packlist ON readiness_checks(packlist_id,created_at DESC,id DESC);
CREATE TRIGGER packlists_no_update BEFORE UPDATE ON packlist_versions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packlists_no_delete BEFORE DELETE ON packlist_versions BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packitems_no_update BEFORE UPDATE ON packlist_items BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packitems_no_delete BEFORE DELETE ON packlist_items BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER checks_no_update BEFORE UPDATE ON readiness_checks BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER checks_no_delete BEFORE DELETE ON readiness_checks BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packevents_no_update BEFORE UPDATE ON packlist_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packevents_no_delete BEFORE DELETE ON packlist_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
