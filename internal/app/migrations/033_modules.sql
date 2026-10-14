CREATE TABLE module_configuration_events (
    version INTEGER PRIMARY KEY CHECK(version > 0),
    modules TEXT NOT NULL CHECK(length(modules) BETWEEN 1 AND 1000),
    created_at TEXT NOT NULL
);
CREATE TRIGGER module_configuration_sequence BEFORE INSERT ON module_configuration_events
WHEN NEW.version != (SELECT COALESCE(MAX(version),0)+1 FROM module_configuration_events)
BEGIN SELECT RAISE(ABORT,'module configuration revision must increase'); END;
CREATE TRIGGER module_configuration_no_update BEFORE UPDATE ON module_configuration_events
BEGIN SELECT RAISE(ABORT,'module configuration history is immutable'); END;
CREATE TRIGGER module_configuration_no_delete BEFORE DELETE ON module_configuration_events
BEGIN SELECT RAISE(ABORT,'module configuration history is immutable'); END;
