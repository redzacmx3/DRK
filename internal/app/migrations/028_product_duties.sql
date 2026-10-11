CREATE TABLE product_duty_events(id TEXT PRIMARY KEY,device_id TEXT NOT NULL REFERENCES devices(id),version INTEGER NOT NULL CHECK(version>0),actor_id TEXT NOT NULL REFERENCES users(id),action TEXT NOT NULL CHECK(action IN ('review','revoke')),created_at TEXT NOT NULL,payload_json TEXT NOT NULL,UNIQUE(device_id,version));

CREATE TABLE product_duty_outbox(event_id TEXT PRIMARY KEY REFERENCES product_duty_events(id),published_at TEXT);
CREATE TABLE product_duty_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES product_duty_events(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER product_duty_no_update BEFORE UPDATE ON product_duty_events BEGIN SELECT RAISE(ABORT,'immutable product duty'); END;
CREATE TRIGGER product_duty_no_delete BEFORE DELETE ON product_duty_events BEGIN SELECT RAISE(ABORT,'immutable product duty'); END;
CREATE TRIGGER product_duty_requests_no_update BEFORE UPDATE ON product_duty_requests BEGIN SELECT RAISE(ABORT,'immutable product duty response'); END;
CREATE TRIGGER product_duty_requests_no_delete BEFORE DELETE ON product_duty_requests BEGIN SELECT RAISE(ABORT,'immutable product duty response'); END;
