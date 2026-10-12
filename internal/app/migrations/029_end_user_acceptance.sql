CREATE TABLE end_user_acceptance_events(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),subject_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('accept','reject','revoke')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(device_id,version));
CREATE INDEX end_user_acceptance_subject ON end_user_acceptance_events(device_id,subject_id,version);
CREATE TABLE end_user_acceptance_outbox(event_id TEXT PRIMARY KEY REFERENCES end_user_acceptance_events(id),published_at TEXT);
CREATE TABLE end_user_acceptance_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES end_user_acceptance_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER end_user_acceptance_no_update BEFORE UPDATE ON end_user_acceptance_events BEGIN SELECT RAISE(ABORT,'immutable end user acceptance'); END;
CREATE TRIGGER end_user_acceptance_no_delete BEFORE DELETE ON end_user_acceptance_events BEGIN SELECT RAISE(ABORT,'immutable end user acceptance'); END;
CREATE TRIGGER end_user_acceptance_requests_no_update BEFORE UPDATE ON end_user_acceptance_requests BEGIN SELECT RAISE(ABORT,'immutable end user acceptance response'); END;
CREATE TRIGGER end_user_acceptance_requests_no_delete BEFORE DELETE ON end_user_acceptance_requests BEGIN SELECT RAISE(ABORT,'immutable end user acceptance response'); END;
