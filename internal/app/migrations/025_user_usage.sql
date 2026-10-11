CREATE TABLE usage_events(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),subject_id TEXT REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('policy','qualify','revoke','bind','unbind')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(device_id,version));
CREATE TRIGGER usage_events_no_update BEFORE UPDATE ON usage_events BEGIN SELECT RAISE(ABORT,'immutable user usage event'); END;
CREATE TRIGGER usage_events_no_delete BEFORE DELETE ON usage_events BEGIN SELECT RAISE(ABORT,'immutable user usage event'); END;
CREATE TABLE usage_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES usage_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER usage_requests_no_update BEFORE UPDATE ON usage_requests BEGIN SELECT RAISE(ABORT,'immutable usage request'); END;
CREATE TRIGGER usage_requests_no_delete BEFORE DELETE ON usage_requests BEGIN SELECT RAISE(ABORT,'immutable usage request'); END;
CREATE INDEX usage_device_subject ON usage_events(device_id,subject_id,version);
