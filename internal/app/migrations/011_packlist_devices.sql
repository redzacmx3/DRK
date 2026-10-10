CREATE TABLE packlist_devices(packlist_id TEXT NOT NULL REFERENCES packlist_versions(id),device_id TEXT NOT NULL REFERENCES devices(id),accessories_json TEXT NOT NULL,PRIMARY KEY(packlist_id,device_id));
CREATE TRIGGER packdevices_no_update BEFORE UPDATE ON packlist_devices BEGIN SELECT RAISE(ABORT,'append-only'); END;
CREATE TRIGGER packdevices_no_delete BEFORE DELETE ON packlist_devices BEGIN SELECT RAISE(ABORT,'append-only'); END;
