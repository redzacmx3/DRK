CREATE TABLE manual_access_events(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),subject_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('confirm','revoke')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(device_id,version));
CREATE INDEX manual_access_subject ON manual_access_events(device_id,subject_id,version);
CREATE TABLE manual_access_outbox(event_id TEXT PRIMARY KEY REFERENCES manual_access_events(id),published_at TEXT);
CREATE TABLE manual_access_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES manual_access_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER manual_access_no_update BEFORE UPDATE ON manual_access_events BEGIN SELECT RAISE(ABORT,'immutable manual access'); END;
CREATE TRIGGER manual_access_no_delete BEFORE DELETE ON manual_access_events BEGIN SELECT RAISE(ABORT,'immutable manual access'); END;
CREATE TRIGGER manual_access_requests_no_update BEFORE UPDATE ON manual_access_requests BEGIN SELECT RAISE(ABORT,'immutable manual access response'); END;
CREATE TRIGGER manual_access_requests_no_delete BEFORE DELETE ON manual_access_requests BEGIN SELECT RAISE(ABORT,'immutable manual access response'); END;
