INSERT OR IGNORE INTO coefficients (key, value, value_set_at) VALUES ('time_adjust', 'shadow', '1970-01-01T00:00:00.000Z');
DROP INDEX idx_dependencies_depends_on;
CREATE INDEX idx_dependencies_depends_on ON dependencies (depends_on_id, type, removed, bead_id);
