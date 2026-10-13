CREATE TABLE pack_release_events(id TEXT PRIMARY KEY,packlist_id TEXT NOT NULL REFERENCES packlist_versions(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),subject_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('release','reject','revoke')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(packlist_id,version));
CREATE INDEX pack_release_subject ON pack_release_events(packlist_id,subject_id,version);
CREATE TABLE pack_release_outbox(event_id TEXT PRIMARY KEY REFERENCES pack_release_events(id),published_at TEXT);
CREATE TABLE pack_release_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES pack_release_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER pack_release_no_update BEFORE UPDATE ON pack_release_events BEGIN SELECT RAISE(ABORT,'immutable pack release'); END;
CREATE TRIGGER pack_release_no_delete BEFORE DELETE ON pack_release_events BEGIN SELECT RAISE(ABORT,'immutable pack release'); END;
CREATE TRIGGER pack_release_requests_no_update BEFORE UPDATE ON pack_release_requests BEGIN SELECT RAISE(ABORT,'immutable pack release response'); END;
CREATE TRIGGER pack_release_requests_no_delete BEFORE DELETE ON pack_release_requests BEGIN SELECT RAISE(ABORT,'immutable pack release response'); END;
