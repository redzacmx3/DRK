CREATE TABLE stock_imports(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL REFERENCES users(id),created_at TEXT NOT NULL,csv_hash TEXT NOT NULL,preview_hash TEXT NOT NULL,snapshot_json TEXT NOT NULL,created_count INTEGER NOT NULL CHECK(created_count>=0),unchanged_count INTEGER NOT NULL CHECK(unchanged_count>=0));
CREATE TABLE stock_intake(source_id TEXT PRIMARY KEY COLLATE NOCASE,row_hash TEXT NOT NULL,location_id TEXT NOT NULL REFERENCES locations(id),movement_id TEXT NOT NULL UNIQUE REFERENCES stock_movements(id),import_id TEXT NOT NULL REFERENCES stock_imports(id),external_code TEXT NOT NULL,unknown_lot INTEGER NOT NULL CHECK(unknown_lot IN (0,1)));
CREATE TABLE stock_import_outbox(event_id TEXT PRIMARY KEY REFERENCES stock_imports(id),published_at TEXT);
CREATE TABLE stock_import_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES stock_imports(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER stock_intake_no_update BEFORE UPDATE ON stock_intake BEGIN SELECT RAISE(ABORT,'immutable intake'); END;
CREATE TRIGGER stock_intake_no_delete BEFORE DELETE ON stock_intake BEGIN SELECT RAISE(ABORT,'immutable intake'); END;
CREATE TRIGGER stock_imports_no_update BEFORE UPDATE ON stock_imports BEGIN SELECT RAISE(ABORT,'immutable import'); END;
CREATE TRIGGER stock_imports_no_delete BEFORE DELETE ON stock_imports BEGIN SELECT RAISE(ABORT,'immutable import'); END;
