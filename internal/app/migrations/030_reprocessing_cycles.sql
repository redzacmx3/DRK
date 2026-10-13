CREATE TABLE reprocessing_events(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('need','cycle','release','reject','revoke')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(device_id,version));
CREATE TABLE reprocessing_outbox(event_id TEXT PRIMARY KEY REFERENCES reprocessing_events(id),published_at TEXT);
CREATE TABLE reprocessing_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES reprocessing_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER reprocessing_events_no_update BEFORE UPDATE ON reprocessing_events BEGIN SELECT RAISE(ABORT,'immutable reprocessing event'); END;
CREATE TRIGGER reprocessing_events_no_delete BEFORE DELETE ON reprocessing_events BEGIN SELECT RAISE(ABORT,'immutable reprocessing event'); END;
CREATE TRIGGER reprocessing_requests_no_update BEFORE UPDATE ON reprocessing_requests BEGIN SELECT RAISE(ABORT,'immutable reprocessing response'); END;
CREATE TRIGGER reprocessing_requests_no_delete BEFORE DELETE ON reprocessing_requests BEGIN SELECT RAISE(ABORT,'immutable reprocessing response'); END;
