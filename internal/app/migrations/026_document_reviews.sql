CREATE TABLE document_reviews(
 id TEXT PRIMARY KEY,
 document_id TEXT NOT NULL REFERENCES device_documents(id),
 version INTEGER NOT NULL CHECK(version>0),
 actor_id TEXT NOT NULL REFERENCES users(id),
 action TEXT NOT NULL CHECK(action IN ('scan','release','reject','quarantine')),
 status TEXT NOT NULL CHECK(status IN ('quarantined','released','rejected')),
 created_at TEXT NOT NULL,
 payload_json TEXT NOT NULL,
 UNIQUE(document_id,version)
);
CREATE TABLE document_review_outbox(event_id TEXT PRIMARY KEY REFERENCES document_reviews(id),published_at TEXT);
CREATE TABLE document_review_requests(user_id TEXT NOT NULL REFERENCES users(id),key TEXT NOT NULL,request_hash TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES document_reviews(id),response_json TEXT NOT NULL,PRIMARY KEY(user_id,key));
CREATE TRIGGER document_reviews_no_update BEFORE UPDATE ON document_reviews BEGIN SELECT RAISE(ABORT,'immutable document review'); END;
CREATE TRIGGER document_reviews_no_delete BEFORE DELETE ON document_reviews BEGIN SELECT RAISE(ABORT,'immutable document review'); END;
CREATE TRIGGER document_review_requests_no_update BEFORE UPDATE ON document_review_requests BEGIN SELECT RAISE(ABORT,'immutable document review request'); END;
CREATE TRIGGER document_review_requests_no_delete BEFORE DELETE ON document_review_requests BEGIN SELECT RAISE(ABORT,'immutable document review request'); END;
CREATE VIEW effective_documents AS SELECT d.id,d.device_id,d.filename,d.media_type,d.size,d.sha256,d.content,d.created_by,d.created_at,d.note,
 COALESCE((SELECT r.status FROM document_reviews r WHERE r.document_id=d.id ORDER BY r.version DESC LIMIT 1),d.status) status,
 COALESCE((SELECT MAX(r.version) FROM document_reviews r WHERE r.document_id=d.id),0) review_version FROM device_documents d;
