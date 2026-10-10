CREATE TABLE device_control_events(
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL REFERENCES devices(id),
 version INTEGER NOT NULL CHECK(version>0),
 action TEXT NOT NULL CHECK(action IN ('commission','rules')),
 actor_id TEXT NOT NULL REFERENCES users(id),
 created_at TEXT NOT NULL,
 payload_json TEXT NOT NULL,
 UNIQUE(device_id,version)
);
CREATE TABLE device_control_outbox(event_id TEXT PRIMARY KEY REFERENCES device_control_events(id),published_at TEXT);
CREATE TABLE device_control_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES device_control_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER device_controls_no_update BEFORE UPDATE ON device_control_events BEGIN SELECT RAISE(ABORT,'immutable device control event'); END;
CREATE TRIGGER device_controls_no_delete BEFORE DELETE ON device_control_events BEGIN SELECT RAISE(ABORT,'immutable device control event'); END;
