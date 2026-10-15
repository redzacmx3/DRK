-- Recognition photos are separate from technical proofs and quarantined documents.
CREATE TABLE device_photo_events (
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL REFERENCES devices(id),
 version INTEGER NOT NULL CHECK(version>0),
 action TEXT NOT NULL CHECK(action IN ('set','remove')),
 actor_id TEXT NOT NULL REFERENCES users(id),
 created_at TEXT NOT NULL,
 note TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 content BLOB NOT NULL,
 UNIQUE(device_id,version),
 CHECK((action='set' AND length(content)>0 AND length(content)<=4194304 AND length(sha256)=64) OR (action='remove' AND length(content)=0 AND sha256=''))
);
CREATE TABLE device_photo_requests (
 user_id TEXT NOT NULL REFERENCES users(id),
 key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 event_id TEXT NOT NULL REFERENCES device_photo_events(id),
 response_json TEXT NOT NULL,
 PRIMARY KEY(user_id,key)
);
CREATE TRIGGER device_photo_events_no_update BEFORE UPDATE ON device_photo_events BEGIN SELECT RAISE(ABORT,'immutable recognition photo'); END;
CREATE TRIGGER device_photo_events_no_delete BEFORE DELETE ON device_photo_events BEGIN SELECT RAISE(ABORT,'immutable recognition photo'); END;
CREATE TRIGGER device_photo_requests_no_update BEFORE UPDATE ON device_photo_requests BEGIN SELECT RAISE(ABORT,'immutable photo request'); END;
CREATE TRIGGER device_photo_requests_no_delete BEFORE DELETE ON device_photo_requests BEGIN SELECT RAISE(ABORT,'immutable photo request'); END;
