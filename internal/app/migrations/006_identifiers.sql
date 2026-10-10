CREATE TABLE product_identifiers (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES product_types(id),
 kind TEXT NOT NULL CHECK(kind IN ('gtin','pzn','ref')), value TEXT NOT NULL,
 pack_quantity INTEGER NOT NULL CHECK(typeof(pack_quantity)='integer' AND pack_quantity BETWEEN 1 AND 1000000000),
 active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
 user_id TEXT NOT NULL REFERENCES users(id), created_at TEXT NOT NULL, note TEXT NOT NULL
);
CREATE UNIQUE INDEX unique_active_identifier ON product_identifiers(kind,value) WHERE active=1;
CREATE TABLE catalog_events (
 id TEXT PRIMARY KEY, actor_id TEXT NOT NULL REFERENCES users(id),
 action TEXT NOT NULL CHECK(action IN ('identifier_added','identifier_revoked')),
 identifier_id TEXT NOT NULL REFERENCES product_identifiers(id), created_at TEXT NOT NULL,
 note TEXT NOT NULL, snapshot_json TEXT NOT NULL
);
CREATE TABLE catalog_outbox (
 event_id TEXT PRIMARY KEY REFERENCES catalog_events(id), delivered_at TEXT
);
CREATE TABLE catalog_requests (
 user_id TEXT NOT NULL REFERENCES users(id), key TEXT NOT NULL, request_hash TEXT NOT NULL,
 event_id TEXT NOT NULL REFERENCES catalog_events(id), response_json TEXT NOT NULL,
 PRIMARY KEY(user_id,key)
);
CREATE TRIGGER identifiers_no_delete BEFORE DELETE ON product_identifiers BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER identifiers_revoke_only BEFORE UPDATE ON product_identifiers
 WHEN NOT (OLD.active=1 AND NEW.active=0 AND NEW.id=OLD.id AND NEW.product_id=OLD.product_id
 AND NEW.kind=OLD.kind AND NEW.value=OLD.value AND NEW.pack_quantity=OLD.pack_quantity
 AND NEW.user_id=OLD.user_id AND NEW.created_at=OLD.created_at AND NEW.note=OLD.note)
 BEGIN SELECT RAISE(ABORT,'only revocation permitted'); END;
CREATE TRIGGER catalog_events_no_update BEFORE UPDATE ON catalog_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER catalog_events_no_delete BEFORE DELETE ON catalog_events BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TABLE movement_packaging (
 movement_id TEXT PRIMARY KEY REFERENCES stock_movements(id),
 identifier_id TEXT NOT NULL REFERENCES product_identifiers(id),
 pack_count INTEGER NOT NULL CHECK(typeof(pack_count)='integer' AND pack_count BETWEEN 1 AND 1000000000),
 pack_quantity INTEGER NOT NULL CHECK(typeof(pack_quantity)='integer' AND pack_quantity BETWEEN 1 AND 1000000000)
);
CREATE TRIGGER packaging_validate BEFORE INSERT ON movement_packaging
 WHEN NOT EXISTS(SELECT 1 FROM stock_movements m JOIN batches b ON b.id=m.batch_id
 JOIN product_identifiers i ON i.id=NEW.identifier_id AND i.product_id=b.product_id
 WHERE m.id=NEW.movement_id AND i.active=1 AND i.pack_quantity=NEW.pack_quantity
 AND m.kind IN ('receipt','transfer','consumption') AND m.quantity=NEW.pack_count*NEW.pack_quantity)
 BEGIN SELECT RAISE(ABORT,'invalid packaging basis'); END;
CREATE TRIGGER packaging_no_update BEFORE UPDATE ON movement_packaging BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packaging_no_delete BEFORE DELETE ON movement_packaging BEGIN SELECT RAISE(ABORT,'append-only'); END;
