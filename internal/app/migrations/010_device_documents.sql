CREATE TABLE new_device_control_events(
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL REFERENCES devices(id),
 version INTEGER NOT NULL CHECK(version>0),
 action TEXT NOT NULL CHECK(action IN ('commission','rules','inspection','defect','release','instruction')),
 actor_id TEXT NOT NULL REFERENCES users(id),
 created_at TEXT NOT NULL,
 payload_json TEXT NOT NULL,
 UNIQUE(device_id,version)
);
INSERT INTO new_device_control_events SELECT * FROM device_control_events;
DROP TABLE device_control_events;
ALTER TABLE new_device_control_events RENAME TO device_control_events;
CREATE TRIGGER device_controls_no_update BEFORE UPDATE ON device_control_events BEGIN SELECT RAISE(ABORT,'immutable device control event'); END;
CREATE TRIGGER device_controls_no_delete BEFORE DELETE ON device_control_events BEGIN SELECT RAISE(ABORT,'immutable device control event'); END;
CREATE TABLE device_documents(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),filename TEXT NOT NULL,media_type TEXT NOT NULL,size INTEGER NOT NULL CHECK(size>0 AND size<=4194304),sha256 TEXT NOT NULL,content BLOB NOT NULL,created_by TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,note TEXT NOT NULL,status TEXT NOT NULL CHECK(status='quarantined'),CHECK(length(content)=size));
CREATE TABLE document_events(id TEXT PRIMARY KEY,document_id TEXT NOT NULL REFERENCES device_documents(id),actor_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,snapshot_json TEXT NOT NULL);
CREATE TABLE document_outbox(event_id TEXT PRIMARY KEY REFERENCES document_events(id),published_at TEXT);
CREATE TABLE document_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES document_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER documents_no_update BEFORE UPDATE ON device_documents BEGIN SELECT RAISE(ABORT,'immutable document'); END;
CREATE TRIGGER documents_no_delete BEFORE DELETE ON device_documents BEGIN SELECT RAISE(ABORT,'immutable document'); END;
CREATE TRIGGER document_events_no_update BEFORE UPDATE ON document_events BEGIN SELECT RAISE(ABORT,'immutable document event'); END;
CREATE TRIGGER document_events_no_delete BEFORE DELETE ON document_events BEGIN SELECT RAISE(ABORT,'immutable document event'); END;
