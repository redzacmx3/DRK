CREATE TABLE duty_fulfilment_events(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('accept','reject','revoke')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(device_id,version));
CREATE TABLE duty_fulfilment_outbox(event_id TEXT PRIMARY KEY REFERENCES duty_fulfilment_events(id),published_at TEXT);
CREATE TABLE duty_fulfilment_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES duty_fulfilment_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER duty_fulfilment_no_update BEFORE UPDATE ON duty_fulfilment_events BEGIN SELECT RAISE(ABORT,'immutable duty fulfilment'); END;
CREATE TRIGGER duty_fulfilment_no_delete BEFORE DELETE ON duty_fulfilment_events BEGIN SELECT RAISE(ABORT,'immutable duty fulfilment'); END;
CREATE TRIGGER duty_fulfilment_requests_no_update BEFORE UPDATE ON duty_fulfilment_requests BEGIN SELECT RAISE(ABORT,'immutable duty fulfilment response'); END;
CREATE TRIGGER duty_fulfilment_requests_no_delete BEFORE DELETE ON duty_fulfilment_requests BEGIN SELECT RAISE(ABORT,'immutable duty fulfilment response'); END;
