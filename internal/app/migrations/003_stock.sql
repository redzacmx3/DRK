ALTER TABLE role_assignments RENAME TO old_role_assignments;
CREATE TABLE role_assignments (
 user_id TEXT NOT NULL REFERENCES users(id),
 site_id TEXT NOT NULL REFERENCES sites(id),
 role TEXT NOT NULL CHECK(role IN ('viewer','helper','material_manager')),
 PRIMARY KEY(user_id,site_id,role)
);
INSERT INTO role_assignments SELECT * FROM old_role_assignments;
DROP TABLE old_role_assignments;
CREATE TABLE product_types (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, base_unit TEXT NOT NULL
);
CREATE TABLE batches (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES product_types(id),
 lot TEXT NOT NULL, expires_on TEXT, blocked INTEGER NOT NULL DEFAULT 0 CHECK(blocked IN (0,1)),
 UNIQUE(product_id,lot)
);
CREATE TABLE stock_balances (
 batch_id TEXT NOT NULL REFERENCES batches(id), location_id TEXT NOT NULL REFERENCES locations(id),
 state TEXT NOT NULL CHECK(state IN ('available','quarantined','damaged')),
 quantity INTEGER NOT NULL CHECK(typeof(quantity)='integer' AND quantity>=0),
 version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
 PRIMARY KEY(batch_id,location_id,state)
);
CREATE TABLE stock_movements (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('receipt','transfer','consumption')),
 user_id TEXT NOT NULL REFERENCES users(id), batch_id TEXT NOT NULL REFERENCES batches(id),
 from_location_id TEXT REFERENCES locations(id), to_location_id TEXT REFERENCES locations(id),
 quantity INTEGER NOT NULL CHECK(typeof(quantity)='integer' AND quantity>0),
 state TEXT NOT NULL CHECK(state IN ('available','quarantined','damaged')),
 created_at TEXT NOT NULL, note TEXT NOT NULL,
 CHECK((kind='receipt' AND from_location_id IS NULL AND to_location_id IS NOT NULL) OR
       (kind='consumption' AND from_location_id IS NOT NULL AND to_location_id IS NULL) OR
       (kind='transfer' AND from_location_id IS NOT NULL AND to_location_id IS NOT NULL AND from_location_id!=to_location_id))
);
CREATE TABLE movement_lines (
 movement_id TEXT NOT NULL REFERENCES stock_movements(id), batch_id TEXT NOT NULL REFERENCES batches(id),
 location_id TEXT NOT NULL REFERENCES locations(id), state TEXT NOT NULL CHECK(state IN ('available','quarantined','damaged')),
 delta INTEGER NOT NULL CHECK(typeof(delta)='integer' AND delta!=0),
 PRIMARY KEY(movement_id,location_id,state)
);
CREATE TABLE audit_events (
 id TEXT PRIMARY KEY, actor_id TEXT NOT NULL REFERENCES users(id), action TEXT NOT NULL,
 object_id TEXT NOT NULL REFERENCES stock_movements(id), created_at TEXT NOT NULL
);
CREATE TABLE outbox (
 id TEXT PRIMARY KEY, movement_id TEXT NOT NULL UNIQUE REFERENCES stock_movements(id),
 event_type TEXT NOT NULL, created_at TEXT NOT NULL, delivered_at TEXT
);
CREATE TABLE idempotency_keys (
 user_id TEXT NOT NULL REFERENCES users(id), key TEXT NOT NULL,
 request_hash TEXT NOT NULL, movement_id TEXT NOT NULL REFERENCES stock_movements(id), response_json TEXT NOT NULL,
 PRIMARY KEY(user_id,key)
);
CREATE TRIGGER movements_no_update BEFORE UPDATE ON stock_movements BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER movements_no_delete BEFORE DELETE ON stock_movements BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER lines_no_update BEFORE UPDATE ON movement_lines BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER lines_no_delete BEFORE DELETE ON movement_lines BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
