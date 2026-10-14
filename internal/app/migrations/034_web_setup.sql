-- Remember the Compose/CLI input separately from the administrated live choice.
-- An unchanged startup input must not overwrite a later web administration.
CREATE TABLE module_startup_selection (
    id INTEGER PRIMARY KEY CHECK(id=1),
    modules TEXT NOT NULL CHECK(length(modules) BETWEEN 1 AND 1000)
);
INSERT INTO module_startup_selection(id,modules)
SELECT 1,modules FROM module_configuration_events ORDER BY version DESC LIMIT 1;

CREATE TABLE module_management_events (
    configuration_version INTEGER PRIMARY KEY REFERENCES module_configuration_events(version),
    actor_id TEXT REFERENCES users(id),
    source TEXT NOT NULL CHECK(source IN ('environment','admin','setup')),
    note TEXT NOT NULL CHECK(length(note) BETWEEN 1 AND 1000)
);
INSERT INTO module_management_events(configuration_version,actor_id,source,note)
SELECT version,NULL,'environment','Modulkonfiguration vor der Webverwaltung'
FROM module_configuration_events;
CREATE TRIGGER module_management_events_no_update BEFORE UPDATE ON module_management_events
BEGIN SELECT RAISE(ABORT,'module management history is immutable'); END;
CREATE TRIGGER module_management_events_no_delete BEFORE DELETE ON module_management_events
BEGIN SELECT RAISE(ABORT,'module management history is immutable'); END;

CREATE TABLE module_management_requests (
    user_id TEXT NOT NULL REFERENCES users(id),
    key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    event_id TEXT NOT NULL REFERENCES administrative_events(id),
    response_json TEXT NOT NULL,
    PRIMARY KEY(user_id,key)
);
CREATE TRIGGER module_management_requests_no_update BEFORE UPDATE ON module_management_requests
BEGIN SELECT RAISE(ABORT,'module management request is immutable'); END;
CREATE TRIGGER module_management_requests_no_delete BEFORE DELETE ON module_management_requests
BEGIN SELECT RAISE(ABORT,'module management request is immutable'); END;

CREATE TABLE web_setup_state (
    id INTEGER PRIMARY KEY CHECK(id=1),
    token_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    completed_at TEXT,
    admin_id TEXT REFERENCES users(id),
    completion_reason TEXT NOT NULL DEFAULT ''
);
CREATE TRIGGER web_setup_completed_no_update BEFORE UPDATE ON web_setup_state
WHEN OLD.completed_at IS NOT NULL
BEGIN SELECT RAISE(ABORT,'completed web setup is immutable'); END;
CREATE TRIGGER web_setup_no_delete BEFORE DELETE ON web_setup_state
BEGIN SELECT RAISE(ABORT,'web setup state must be retained'); END;
