-- Standard SQLite table rebuild: Migrate uses a dedicated connection, checks
-- foreign keys before commit, and restores enforcement before returning.
CREATE TABLE new_stock_movements (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('receipt','transfer','consumption','reversal','inventory')),
 user_id TEXT NOT NULL REFERENCES users(id), batch_id TEXT NOT NULL REFERENCES batches(id),
 from_location_id TEXT REFERENCES locations(id), to_location_id TEXT REFERENCES locations(id),
 quantity INTEGER NOT NULL CHECK(typeof(quantity)='integer' AND quantity>=0),
 state TEXT NOT NULL CHECK(state IN ('available','quarantined','damaged')),
 created_at TEXT NOT NULL, note TEXT NOT NULL,
 reversed_movement_id TEXT UNIQUE REFERENCES stock_movements(id),
 counted_quantity INTEGER CHECK(counted_quantity IS NULL OR (typeof(counted_quantity)='integer' AND counted_quantity>=0)),
 basis_quantity INTEGER CHECK(basis_quantity IS NULL OR (typeof(basis_quantity)='integer' AND basis_quantity>=0)),
 basis_version INTEGER CHECK(basis_version IS NULL OR (typeof(basis_version)='integer' AND basis_version>=0)),
 CHECK(
  (kind IN ('receipt','transfer','consumption') AND quantity>0 AND reversed_movement_id IS NULL AND counted_quantity IS NULL AND basis_quantity IS NULL AND basis_version IS NULL AND (
    (kind='receipt' AND from_location_id IS NULL AND to_location_id IS NOT NULL) OR
    (kind='consumption' AND from_location_id IS NOT NULL AND to_location_id IS NULL) OR
    (kind='transfer' AND from_location_id IS NOT NULL AND to_location_id IS NOT NULL AND from_location_id!=to_location_id))) OR
  (kind='reversal' AND quantity>0 AND reversed_movement_id IS NOT NULL AND counted_quantity IS NULL AND basis_quantity IS NULL AND basis_version IS NULL AND
    (from_location_id IS NOT NULL OR to_location_id IS NOT NULL) AND (from_location_id IS NULL OR to_location_id IS NULL OR from_location_id!=to_location_id)) OR
  (kind='inventory' AND reversed_movement_id IS NULL AND from_location_id IS NULL AND to_location_id IS NOT NULL AND counted_quantity IS NOT NULL AND basis_quantity IS NOT NULL AND basis_version IS NOT NULL AND quantity=abs(counted_quantity-basis_quantity))
 )
);
INSERT INTO new_stock_movements(id,kind,user_id,batch_id,from_location_id,to_location_id,quantity,state,created_at,note)
 SELECT id,kind,user_id,batch_id,from_location_id,to_location_id,quantity,state,created_at,note FROM stock_movements;
DROP TABLE stock_movements;
ALTER TABLE new_stock_movements RENAME TO stock_movements;
CREATE TRIGGER movements_no_update BEFORE UPDATE ON stock_movements BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER movements_no_delete BEFORE DELETE ON stock_movements BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE INDEX movements_created ON stock_movements(created_at,id);
