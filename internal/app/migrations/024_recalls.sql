CREATE TABLE recalls(id TEXT PRIMARY KEY,kind TEXT NOT NULL CHECK(kind IN ('material','device')),title TEXT NOT NULL,reference TEXT NOT NULL,product_id TEXT REFERENCES product_types(id),batch_id TEXT REFERENCES batches(id),manufacturer TEXT NOT NULL,model TEXT NOT NULL,serial_exact TEXT NOT NULL,serial_from TEXT NOT NULL,serial_to TEXT NOT NULL,created_by TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL);
CREATE TRIGGER recalls_no_update BEFORE UPDATE ON recalls BEGIN SELECT RAISE(ABORT,'immutable recall definition'); END;
CREATE TRIGGER recalls_no_delete BEFORE DELETE ON recalls BEGIN SELECT RAISE(ABORT,'immutable recall definition'); END;
CREATE TABLE recall_state(recall_id TEXT PRIMARY KEY REFERENCES recalls(id),version INTEGER NOT NULL CHECK(version>0),status TEXT NOT NULL CHECK(status IN ('OPEN','CLOSED')));
CREATE TABLE recall_events(id TEXT PRIMARY KEY,recall_id TEXT NOT NULL REFERENCES recalls(id),version INTEGER NOT NULL,actor_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL,location_id TEXT REFERENCES locations(id),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(recall_id,version));
CREATE TRIGGER recall_events_no_update BEFORE UPDATE ON recall_events BEGIN SELECT RAISE(ABORT,'immutable recall event'); END;
CREATE TRIGGER recall_events_no_delete BEFORE DELETE ON recall_events BEGIN SELECT RAISE(ABORT,'immutable recall event'); END;
CREATE TABLE recall_actions(recall_id TEXT NOT NULL REFERENCES recalls(id),target_key TEXT NOT NULL,basis_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES recall_events(id),PRIMARY KEY(recall_id,target_key));
CREATE TABLE recall_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES recall_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER recall_requests_no_update BEFORE UPDATE ON recall_requests BEGIN SELECT RAISE(ABORT,'immutable recall request'); END;
CREATE TRIGGER recall_requests_no_delete BEFORE DELETE ON recall_requests BEGIN SELECT RAISE(ABORT,'immutable recall request'); END;
CREATE VIEW effective_batches AS SELECT b.id,b.product_id,b.lot,b.expires_on,
 (b.blocked OR EXISTS(SELECT 1 FROM recalls r JOIN recall_state s ON s.recall_id=r.id WHERE r.kind='material' AND r.product_id=b.product_id AND (r.batch_id IS NULL OR r.batch_id=b.id) AND s.status='OPEN')) AS blocked,
 COALESCE((SELECT SUM(s.version) FROM recalls r JOIN recall_state s ON s.recall_id=r.id WHERE r.kind='material' AND r.product_id=b.product_id AND (r.batch_id IS NULL OR r.batch_id=b.id)),0) AS recall_revision FROM batches b;
