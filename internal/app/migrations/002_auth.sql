CREATE TABLE users (
 id TEXT PRIMARY KEY,
 username TEXT NOT NULL UNIQUE,
 password_hash TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1))
);
CREATE TABLE role_assignments (
 user_id TEXT NOT NULL REFERENCES users(id),
 site_id TEXT NOT NULL REFERENCES sites(id),
 role TEXT NOT NULL CHECK(role='viewer'),
 PRIMARY KEY(user_id,site_id,role)
);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id),
 csrf_hash TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 last_seen INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
