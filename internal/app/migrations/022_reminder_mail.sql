CREATE TABLE device_reminder_deliveries(
 id TEXT PRIMARY KEY,
 reminder_id TEXT NOT NULL REFERENCES device_reminders(id),
 site_id TEXT NOT NULL REFERENCES sites(id),
 recipient TEXT NOT NULL,
 message_id TEXT NOT NULL UNIQUE,
 created_at TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','sending','retry','accepted','failed','uncertain','skipped')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_attempt_at TEXT NOT NULL,
 lease_token TEXT,
 lease_until TEXT,
 last_attempt_at TEXT,
 last_code TEXT NOT NULL DEFAULT '',
 UNIQUE(reminder_id,recipient)
);
CREATE INDEX reminder_delivery_due ON device_reminder_deliveries(state,next_attempt_at);
CREATE TRIGGER delivery_identity_immutable BEFORE UPDATE ON device_reminder_deliveries
 WHEN NEW.id IS NOT OLD.id OR NEW.reminder_id IS NOT OLD.reminder_id OR NEW.site_id IS NOT OLD.site_id OR NEW.recipient IS NOT OLD.recipient OR NEW.message_id IS NOT OLD.message_id OR NEW.created_at IS NOT OLD.created_at
 BEGIN SELECT RAISE(ABORT,'immutable delivery identity'); END;
CREATE TRIGGER delivery_no_delete BEFORE DELETE ON device_reminder_deliveries BEGIN SELECT RAISE(ABORT,'immutable delivery identity'); END;
CREATE TABLE device_reminder_mail_attempts(
 id TEXT PRIMARY KEY,
 delivery_id TEXT NOT NULL REFERENCES device_reminder_deliveries(id),
 attempted_at TEXT NOT NULL,
 outcome TEXT NOT NULL,
 code TEXT NOT NULL
);
CREATE TRIGGER mail_attempt_no_update BEFORE UPDATE ON device_reminder_mail_attempts BEGIN SELECT RAISE(ABORT,'immutable mail attempt'); END;
CREATE TRIGGER mail_attempt_no_delete BEFORE DELETE ON device_reminder_mail_attempts BEGIN SELECT RAISE(ABORT,'immutable mail attempt'); END;
