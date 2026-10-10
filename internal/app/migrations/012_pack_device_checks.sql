CREATE TABLE pack_device_checks(
 id TEXT PRIMARY KEY,packlist_id TEXT NOT NULL REFERENCES packlist_versions(id),
 user_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,
 basis_hash TEXT NOT NULL,snapshot_json TEXT NOT NULL,results_json TEXT NOT NULL,
 physical_passed INTEGER NOT NULL CHECK(physical_passed IN(0,1)),note TEXT NOT NULL
);
CREATE TABLE pack_device_check_outbox(event_id TEXT PRIMARY KEY REFERENCES pack_device_checks(id),delivered_at TEXT);
CREATE TABLE pack_device_check_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES pack_device_checks(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER pack_device_checks_no_update BEFORE UPDATE ON pack_device_checks BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER pack_device_checks_no_delete BEFORE DELETE ON pack_device_checks BEGIN SELECT RAISE(ABORT,'append-only'); END;
