CREATE TABLE device_intake(
 device_id TEXT PRIMARY KEY REFERENCES devices(id),
 asset_kind TEXT NOT NULL CHECK(asset_kind IN ('device','equipment','unknown')),
 observed_condition TEXT NOT NULL CHECK(observed_condition IN ('unknown','unremarkable','defective','quarantined')),
 external_code TEXT NOT NULL, source_location TEXT NOT NULL, row_hash TEXT NOT NULL
);
CREATE TABLE device_imports(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,csv_hash TEXT NOT NULL,preview_hash TEXT NOT NULL,snapshot_json TEXT NOT NULL,created_count INTEGER NOT NULL CHECK(created_count>=0),unchanged_count INTEGER NOT NULL CHECK(unchanged_count>=0));
CREATE TABLE device_import_outbox(event_id TEXT PRIMARY KEY REFERENCES device_imports(id),published_at TEXT);
CREATE TABLE device_import_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES device_imports(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER device_intake_no_update BEFORE UPDATE ON device_intake BEGIN SELECT RAISE(ABORT,'immutable intake'); END;
CREATE TRIGGER device_intake_no_delete BEFORE DELETE ON device_intake BEGIN SELECT RAISE(ABORT,'immutable intake'); END;
CREATE TRIGGER device_imports_no_update BEFORE UPDATE ON device_imports BEGIN SELECT RAISE(ABORT,'immutable import'); END;
CREATE TRIGGER device_imports_no_delete BEFORE DELETE ON device_imports BEGIN SELECT RAISE(ABORT,'immutable import'); END;
