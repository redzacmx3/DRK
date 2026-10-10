CREATE TABLE new_device_control_events(
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL REFERENCES devices(id),
 version INTEGER NOT NULL CHECK(version>0),
 action TEXT NOT NULL CHECK(action IN ('commission','rules','inspection','defect','release')),
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
