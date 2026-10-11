CREATE TABLE device_reminders(
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL REFERENCES devices(id),
 control_type TEXT NOT NULL CHECK(control_type IN ('STK','MTK','electrical')),
 due_on TEXT NOT NULL,
 basis_hash TEXT NOT NULL,
 threshold_days INTEGER NOT NULL CHECK(threshold_days BETWEEN -1 AND 365),
 source_version INTEGER NOT NULL CHECK(source_version>=0),
 created_at TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
 resolved_at TEXT,
 UNIQUE(device_id,control_type,basis_hash,threshold_days)
);
CREATE INDEX reminder_active ON device_reminders(active,device_id);
CREATE TRIGGER reminder_identity_immutable BEFORE UPDATE ON device_reminders
 WHEN NEW.id IS NOT OLD.id OR NEW.device_id IS NOT OLD.device_id OR NEW.control_type IS NOT OLD.control_type OR NEW.due_on IS NOT OLD.due_on OR NEW.basis_hash IS NOT OLD.basis_hash OR NEW.threshold_days IS NOT OLD.threshold_days OR NEW.source_version IS NOT OLD.source_version OR NEW.created_at IS NOT OLD.created_at
 BEGIN SELECT RAISE(ABORT,'immutable reminder evidence'); END;
CREATE TRIGGER reminder_no_delete BEFORE DELETE ON device_reminders BEGIN SELECT RAISE(ABORT,'immutable reminder evidence'); END;
CREATE TABLE device_reminder_outbox(
 reminder_id TEXT PRIMARY KEY REFERENCES device_reminders(id),
 created_at TEXT NOT NULL
);
CREATE TRIGGER reminder_outbox_no_update BEFORE UPDATE ON device_reminder_outbox BEGIN SELECT RAISE(ABORT,'immutable reminder outbox'); END;
CREATE TRIGGER reminder_outbox_no_delete BEFORE DELETE ON device_reminder_outbox BEGIN SELECT RAISE(ABORT,'immutable reminder outbox'); END;
CREATE TABLE device_reminder_worker(
 id INTEGER PRIMARY KEY CHECK(id=1),
 last_success_at TEXT NOT NULL,
 thresholds_json TEXT NOT NULL
);
