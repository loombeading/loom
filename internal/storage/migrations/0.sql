CREATE TABLE beads (
	id             TEXT PRIMARY KEY,
	namespace         TEXT NOT NULL,
	namespace_set_at      TEXT,
	title          TEXT NOT NULL,
	title_set_at       TEXT,
	description    TEXT,
	description_set_at TEXT,
	status         TEXT NOT NULL,
	status_set_at      TEXT,
	closed_at      TEXT,
	closed_at_set_at   TEXT,
	priority       INTEGER NOT NULL CHECK (priority BETWEEN 1 AND 4),
	priority_set_at    TEXT,
	type     TEXT NOT NULL,
	type_set_at  TEXT,
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL,
	claimed_by       TEXT,
	claimed_by_set_at    TEXT,
	summary        TEXT,
	summary_set_at     TEXT,
	labels         TEXT,
	labels_set_at      TEXT,
	reasoning_depth          INTEGER,
	reasoning_depth_set_at       TEXT,
	external_refs  TEXT,
	external_refs_set_at TEXT,
	claim_expires_at TEXT,
	claim_expires_at_set_at TEXT,
	severity       INTEGER NOT NULL DEFAULT 3 CHECK (severity BETWEEN 1 AND 4),
	severity_set_at    TEXT,
	due_at         TEXT,
	due_at_set_at      TEXT,
	expedite_until TEXT,
	expedite_until_set_at TEXT,
	expedite_reason TEXT,
	expedite_reason_set_at TEXT,
	redetect_key     TEXT,
	redetect_key_set_at  TEXT,
	last_redetected_at   TEXT,
	last_redetected_at_set_at TEXT,
	revived_at     TEXT,
	revived_at_set_at  TEXT,
	short_id       TEXT
);
CREATE TABLE dependencies (
	bead_id      TEXT NOT NULL,
	depends_on_id TEXT NOT NULL,
	type          TEXT NOT NULL,
	created_at    TEXT NOT NULL,
	removed       INTEGER NOT NULL DEFAULT 0,
	removed_set_at    TEXT,
	PRIMARY KEY (bead_id, depends_on_id, type)
);
CREATE TABLE token_costs (
	id          TEXT PRIMARY KEY,
	bead_id    TEXT NOT NULL,
	actor       TEXT NOT NULL,
	recorded_at TEXT NOT NULL,
	tokens_in   INTEGER NOT NULL,
	tokens_out  INTEGER NOT NULL
);
CREATE TABLE audit_log (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	occurred_at  TEXT NOT NULL,
	actor        TEXT NOT NULL,
	bead_id     TEXT,
	kind         TEXT NOT NULL,
	field        TEXT,
	new_value    TEXT,
	reason       TEXT,
	origin       TEXT NOT NULL
);
CREATE INDEX idx_dependencies_depends_on ON dependencies (depends_on_id, type, removed);
CREATE INDEX idx_dependencies_bead ON dependencies (bead_id, type, removed);
CREATE INDEX idx_beads_status_priority_created ON beads (status, priority, created_at);
CREATE INDEX idx_audit_log_bead_field_occurred ON audit_log (bead_id, field, occurred_at);
CREATE INDEX idx_dependencies_bead_covering ON dependencies (bead_id, type, removed, depends_on_id);
CREATE INDEX idx_beads_id_status ON beads (id, status);
CREATE INDEX idx_beads_short_id ON beads (short_id);
CREATE INDEX idx_token_costs_bead ON token_costs (bead_id);
CREATE UNIQUE INDEX idx_beads_redetect_key ON beads (namespace, redetect_key) WHERE redetect_key IS NOT NULL AND status IN ('open', 'in_progress');
CREATE TABLE coefficients (
	key      TEXT PRIMARY KEY,
	value    TEXT NOT NULL,
	value_set_at TEXT NOT NULL
);
INSERT INTO coefficients (key, value, value_set_at) VALUES
	('promote_after', '72h', '1970-01-01T00:00:00.000Z'),
	('promote_after_gate', '6h', '1970-01-01T00:00:00.000Z'),
	('decay_after', '7d', '1970-01-01T00:00:00.000Z'),
	('hold_after', '14d', '1970-01-01T00:00:00.000Z'),
	('cancel_after', '30d', '1970-01-01T00:00:00.000Z'),
	('redetect_window', '7d', '1970-01-01T00:00:00.000Z'),
	('severity_1_ceiling', '1', '1970-01-01T00:00:00.000Z'),
	('severity_2_ceiling', '1', '1970-01-01T00:00:00.000Z'),
	('severity_2_floor', '2', '1970-01-01T00:00:00.000Z'),
	('severity_3_ceiling', '2', '1970-01-01T00:00:00.000Z'),
	('severity_3_floor', '4', '1970-01-01T00:00:00.000Z'),
	('severity_4_ceiling', '3', '1970-01-01T00:00:00.000Z'),
	('severity_4_floor', '4', '1970-01-01T00:00:00.000Z'),
	('due_lead', '72h', '1970-01-01T00:00:00.000Z'),
	('expedite_max_open', '2', '1970-01-01T00:00:00.000Z'),
	('expedite_max_age', '24h', '1970-01-01T00:00:00.000Z'),
	('cutoff_priority', '2', '1970-01-01T00:00:00.000Z');
