// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func setUserVersionRaw(t *testing.T, dir string, v string) {
	t.Helper()
	db := openWritable(t, dir)
	if _, err := db.SQL.ExecContext(t.Context(), `PRAGMA user_version = `+v); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
}

func migrateOpts(env map[string]string) OpenOptions {
	return OpenOptions{Env: envMap(env), Actor: "migrate-test"}
}

func TestFreshDatabaseIsCurrentVersion(t *testing.T) {
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	defer func() { _ = db.Close() }()

	v, err := UserVersion(t.Context(), db)
	if err != nil || v != SchemaVersion {
		t.Fatalf("user_version = %d (err %v), want SchemaVersion = %d", v, err, SchemaVersion)
	}
	var n int
	if err := db.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name IN ('beads','dependencies','token_costs','audit_log','coefficients')`).Scan(&n); err != nil || n != 5 {
		t.Fatalf("tables = %d (err %v), want 5", n, err)
	}
}

func TestReopenVersionZeroKeepsData(t *testing.T) {
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	if _, err := db.SQL.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, kind, origin) VALUES ('t', 'a', 'schema', 'local')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db = openWritable(t, dir)
	defer func() { _ = db.Close() }()
	var n int
	if err := db.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit_log rows = %d (err %v), want 1", n, err)
	}
}

func TestMigrateAtCurrentIsNoOp(t *testing.T) {
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()

	res, err := Migrate(context.Background(), dir, migrateOpts(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.From != SchemaVersion || res.To != SchemaVersion {
		t.Fatalf("result = %+v, want a no-op at %d", res, SchemaVersion)
	}
}

func TestMigrateRejectsOtherVersions(t *testing.T) {
	for _, v := range []string{"2", "18", "19", "20"} {
		dir := mustMkdirBeads(t)
		_ = openWritable(t, dir).Close()
		setUserVersionRaw(t, dir, v)

		if _, err := Migrate(context.Background(), dir, migrateOpts(nil)); !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("version %s: err = %v, want ErrSchemaTooNew", v, err)
		}
		if _, err := os.Stat(DBPath(dir) + ".bak-schema" + v); err == nil {
			t.Fatalf("version %s: backup created", v)
		}
	}
}

func TestMigrateReadOnlyIsRejected(t *testing.T) {
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()
	setUserVersionRaw(t, dir, "0")

	if _, err := Migrate(context.Background(), dir, migrateOpts(map[string]string{"LM_READONLY": "1"})); !errors.Is(err, ErrReadOnlyWrite) {
		t.Fatalf("err = %v, want ErrReadOnlyWrite", err)
	}
}

func TestMigrateMissingDatabase(t *testing.T) {
	if _, err := Migrate(context.Background(), t.TempDir(), migrateOpts(nil)); !errors.Is(err, ErrDBNotFound) {
		t.Fatalf("err = %v, want ErrDBNotFound", err)
	}
}

func TestMigrateRequiresEnv(t *testing.T) {
	if _, err := Migrate(context.Background(), t.TempDir(), OpenOptions{}); err == nil {
		t.Fatal("want an error when OpenOptions.Env is nil")
	}
}

func TestMigrateOpenFailuresAreReported(t *testing.T) {
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()
	wantErr := errors.New("injected")

	origDSN, origOpen := migrateBuildDSN, migrateSQLOpen
	t.Cleanup(func() { migrateBuildDSN, migrateSQLOpen = origDSN, origOpen })

	migrateBuildDSN = func(string, bool, string, int) (string, error) { return "", wantErr }
	if _, err := Migrate(context.Background(), dir, migrateOpts(nil)); !errors.Is(err, wantErr) {
		t.Fatalf("dsn err = %v, want %v", err, wantErr)
	}

	migrateBuildDSN = origDSN
	migrateSQLOpen = func(string, string) (*sql.DB, error) { return nil, wantErr }
	if _, err := Migrate(context.Background(), dir, migrateOpts(nil)); !errors.Is(err, wantErr) {
		t.Fatalf("open err = %v, want %v", err, wantErr)
	}
}

func TestMigrateStatFailureIsReported(t *testing.T) {
	requireNonRoot(t)
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := Migrate(context.Background(), dir, migrateOpts(nil))
	if err == nil || errors.Is(err, ErrDBNotFound) {
		t.Fatalf("err = %v, want a stat error other than ErrDBNotFound", err)
	}
}

func seedVersionZero(t *testing.T) string {
	t.Helper()
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	if _, err := db.SQL.ExecContext(t.Context(), `INSERT INTO audit_log (occurred_at, actor, kind, origin) VALUES ('t', 'a', 'schema', 'local')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(t.Context(), `DROP INDEX idx_beads_short_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(t.Context(), `ALTER TABLE beads DROP COLUMN short_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(t.Context(), `PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	return dir
}

func addColumnMigration() fstest.MapFS {
	return fstest.MapFS{migrationPath(1): {Data: []byte(`ALTER TABLE beads ADD COLUMN short_id TEXT;
CREATE INDEX idx_beads_short_id ON beads (short_id);`)}}
}

func rawCount(t *testing.T, path, query string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrateAdvancesOneVersionAndKeepsAuditLog(t *testing.T) {
	dir := seedVersionZero(t)

	res, err := migrateTo(t.Context(), dir, migrateOpts(nil), addColumnMigration(), 1)
	if err != nil {
		t.Fatal(err)
	}
	backup := DBPath(dir) + ".bak-schema0"
	if res.From != 0 || res.To != 1 || res.Backup != backup {
		t.Fatalf("result = %+v", res)
	}
	if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 1 {
		t.Fatalf("user_version = %d, want 1", v)
	}
	if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM audit_log`); n != 1 {
		t.Fatalf("audit_log rows = %d, want 1", n)
	}
	if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM pragma_table_info('beads') WHERE name = 'short_id'`); n != 1 {
		t.Fatal("short_id column missing after migration")
	}
	if v := rawCount(t, backup, `PRAGMA user_version`); v != 0 {
		t.Fatalf("backup user_version = %d, want 0", v)
	}
	if n := rawCount(t, backup, `SELECT count(*) FROM audit_log`); n != 1 {
		t.Fatalf("backup audit_log rows = %d, want 1", n)
	}
}

func mustOpenRawAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateKeepsExistingBackup(t *testing.T) {
	dir := seedVersionZero(t)
	backup := DBPath(dir) + ".bak-schema0"
	if err := os.WriteFile(backup, []byte("first attempt"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := migrateTo(t.Context(), dir, migrateOpts(nil), addColumnMigration(), 1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(backup)
	if err != nil || string(got) != "first attempt" {
		t.Fatalf("backup is %d bytes (err %v), want the first attempt untouched", len(got), err)
	}
}

func TestMigrateMissingFileLeavesDatabaseUntouched(t *testing.T) {
	dir := seedVersionZero(t)

	if _, err := migrateTo(t.Context(), dir, migrateOpts(nil), fstest.MapFS{}, 1); err == nil {
		t.Fatal("want an error for a missing migration file")
	}
	if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 0 {
		t.Fatalf("user_version = %d, want 0", v)
	}
	if _, err := os.Stat(DBPath(dir) + ".bak-schema0"); err == nil {
		t.Fatal("backup written before the migration files were read")
	}
}

func TestMigrateFailedStepRollsBack(t *testing.T) {
	dir := seedVersionZero(t)
	files := fstest.MapFS{
		"migrations/1.sql": {Data: addColumnMigration()["migrations/1.sql"].Data},
		"migrations/2.sql": {Data: []byte("not sql")},
	}

	if _, err := migrateTo(t.Context(), dir, migrateOpts(nil), files, 2); err == nil {
		t.Fatal("want an error for a broken migration")
	}
	if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 0 {
		t.Fatalf("user_version = %d, want 0", v)
	}
	if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM pragma_table_info('beads') WHERE name = 'short_id'`); n != 0 {
		t.Fatal("step 1 survived a failed step 2")
	}
}

func TestMigrateStopsWhenBackupFails(t *testing.T) {
	dir := seedVersionZero(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing", "x"), DBPath(dir)+".bak-schema0"); err != nil {
		t.Fatal(err)
	}

	if _, err := migrateTo(t.Context(), dir, migrateOpts(nil), addColumnMigration(), 1); err == nil || !strings.Contains(err.Error(), "back up") {
		t.Fatalf("err = %v, want a backup error", err)
	}
	if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 0 {
		t.Fatalf("user_version = %d, want 0", v)
	}
}

func TestMigrateBeginFailureIsReported(t *testing.T) {
	dir := seedVersionZero(t)
	wantErr := errors.New("injected")
	orig := migrateBeginTx
	t.Cleanup(func() { migrateBeginTx = orig })
	migrateBeginTx = func(context.Context, *sql.DB) (*sql.Tx, error) { return nil, wantErr }

	if _, err := migrateTo(t.Context(), dir, migrateOpts(nil), addColumnMigration(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestBackupOnceReportsFailures(t *testing.T) {
	db := mustOpenRaw(t, seedVersionZero(t))
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backupOnce(t.Context(), db, filepath.Join(file, "x")); err == nil || !strings.Contains(err.Error(), "stat") {
		t.Fatalf("err = %v, want a stat error", err)
	}
	if err := backupOnce(t.Context(), db, filepath.Join(t.TempDir(), "missing", "x")); err == nil || !strings.Contains(err.Error(), "back up") {
		t.Fatalf("err = %v, want a VACUUM INTO error", err)
	}
}

func TestMigrateFromCLIEntryAdvancesNothingAtCurrent(t *testing.T) {
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()
	res, err := Migrate(t.Context(), dir, migrateOpts(nil))
	if err != nil || res.From != SchemaVersion || res.To != SchemaVersion || res.Backup != "" {
		t.Fatalf("result = %+v (err %v)", res, err)
	}
	if _, err := os.Stat(DBPath(dir) + ".bak-schema" + strconv.Itoa(SchemaVersion)); err == nil {
		t.Fatal("backup written for a no-op migration")
	}
}

func TestOpenRejectsOlderPopulatedDatabase(t *testing.T) {
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()
	setUserVersionRaw(t, dir, "-1")

	_, err := Open(t.Context(), dir, migrateOpts(nil))
	if !errors.Is(err, ErrSchemaTooOld) {
		t.Fatalf("err = %v, want ErrSchemaTooOld", err)
	}
}

func TestMigrationFilesCoverEachVersion(t *testing.T) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != SchemaVersion+1 {
		t.Fatalf("migrations has %d files, want %d (0.sql through %d.sql)", len(entries), SchemaVersion+1, SchemaVersion)
	}
	for v := 0; v <= SchemaVersion; v++ {
		if _, err := fs.Stat(migrationFiles, migrationPath(v)); err != nil {
			t.Fatalf("migration for version %d: %v", v, err)
		}
	}
}

func TestMigratedSchemaMatchesFreshSchema(t *testing.T) {
	migrated := mustOpenRaw(t, mustMkdirBeads(t))
	for v := 0; v <= SchemaVersion; v++ {
		b, err := fs.ReadFile(migrationFiles, migrationPath(v))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrated.ExecContext(t.Context(), string(b)); err != nil {
			t.Fatalf("apply %d: %v", v, err)
		}
	}
	freshDir := mustMkdirBeads(t)
	_ = openWritable(t, freshDir).Close()
	fresh := mustOpenRaw(t, freshDir)

	if got, want := mustSchemaShape(t, migrated), mustSchemaShape(t, fresh); got != want {
		t.Fatalf("migrated schema differs from schemaDDL:\nmigrated:\n%s\nfresh:\n%s", got, want)
	}
}

func mustSchemaShape(t *testing.T, db *sql.DB) string {
	t.Helper()
	s, err := schemaShape(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSchemaShapeReportsQueryFailure(t *testing.T) {
	raw := mustOpenRaw(t, mustMkdirBeads(t))
	_ = raw.Close()
	if _, err := schemaShape(t.Context(), raw); err == nil || !strings.Contains(err.Error(), "read schema") {
		t.Fatalf("err = %v, want a read schema error", err)
	}
}

func seedPrePublicVersionOne(t *testing.T) string {
	t.Helper()
	dir := mustMkdirBeads(t)
	db := openWritable(t, dir)
	if _, err := db.SQL.ExecContext(t.Context(), `INSERT INTO beads (id, namespace, title, status, priority, type, created_at, updated_at, short_id) VALUES ('abc11111111111111111111111', 'lm', 'x', 'open', 2, 'task', 't', 't', 'abc')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	setUserVersionRaw(t, dir, "1")
	return dir
}

func TestMigrateRelabelsPrePublicVersionOne(t *testing.T) {
	dir := seedPrePublicVersionOne(t)

	res, err := Migrate(t.Context(), dir, migrateOpts(nil))
	if err != nil {
		t.Fatal(err)
	}
	backup := DBPath(dir) + ".bak-schema1"
	if res.From != 1 || res.To != 0 || res.Backup != backup {
		t.Fatalf("result = %+v", res)
	}
	if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 0 {
		t.Fatalf("user_version = %d, want 0", v)
	}
	if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM beads WHERE short_id = 'abc'`); n != 1 {
		t.Fatalf("beads rows = %d, want the row kept", n)
	}
	if v := rawCount(t, backup, `PRAGMA user_version`); v != 1 {
		t.Fatalf("backup user_version = %d, want 1", v)
	}
	db, err := Open(t.Context(), dir, migrateOpts(nil))
	if err != nil {
		t.Fatalf("open after relabel: %v", err)
	}
	_ = db.Close()
}

func TestMigrateRejectsPrePublicVersionOneWithOtherSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		stmt string
	}{
		{"missing column", `ALTER TABLE beads DROP COLUMN revived_at_set_at`},
		{"missing index", `DROP INDEX idx_beads_short_id`},
		{"extra trigger", `CREATE TRIGGER t_extra AFTER INSERT ON beads BEGIN SELECT 1; END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := seedPrePublicVersionOne(t)
			raw := mustOpenRawAt(t, DBPath(dir))
			if _, err := raw.ExecContext(t.Context(), tc.stmt); err != nil {
				t.Fatal(err)
			}
			_ = raw.Close()

			if _, err := Migrate(t.Context(), dir, migrateOpts(nil)); !errors.Is(err, ErrSchemaTooNew) {
				t.Fatalf("err = %v, want ErrSchemaTooNew", err)
			}
			if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 1 {
				t.Fatalf("user_version = %d, want 1", v)
			}
			if _, err := os.Stat(DBPath(dir) + ".bak-schema1"); err == nil {
				t.Fatal("backup created for a rejected relabel")
			}
		})
	}
}

func TestMigrateRelabelStopsWhenBackupFails(t *testing.T) {
	dir := seedPrePublicVersionOne(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing", "x"), DBPath(dir)+".bak-schema1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(t.Context(), dir, migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), "back up") {
		t.Fatalf("err = %v, want a backup error", err)
	}
	if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 1 {
		t.Fatalf("user_version = %d, want 1", v)
	}
}

func TestMigrateRelabelReportsCanceledContext(t *testing.T) {
	dir := seedPrePublicVersionOne(t)
	db, err := openMigrateDB(dir, migrateOpts(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reshapePrePublic(ctx, db, dir, prePublicVersion); err == nil {
		t.Fatal("want an error from a canceled context")
	}
}

func TestMigrateRelabelReportsReferenceSchemaFailures(t *testing.T) {
	wantErr := errors.New("injected")
	orig := referenceSQLOpen
	t.Cleanup(func() { referenceSQLOpen = orig })
	for _, tc := range []struct {
		name string
		open func(string, string) (*sql.DB, error)
		want string
	}{
		{"open", func(string, string) (*sql.DB, error) { return nil, wantErr }, "open reference schema"},
		{"create", func(driver, dsn string) (*sql.DB, error) {
			db, err := orig(driver, dsn)
			_ = db.Close()
			return db, err
		}, "create reference schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := seedPrePublicVersionOne(t)
			referenceSQLOpen = tc.open
			if _, err := Migrate(t.Context(), dir, migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 1 {
				t.Fatalf("user_version = %d, want 1", v)
			}
		})
	}
}

func requireNonRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
}

func TestHasBeadsTableReportsClosedDatabase(t *testing.T) {
	raw := mustOpenRaw(t, mustMkdirBeads(t))
	_ = raw.Close()
	if _, err := hasBeadsTable(t.Context(), raw); err == nil {
		t.Fatal("want an error from a closed database")
	}
}

func mustOpenRaw(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(DBPath(dir))+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedPreRename(t *testing.T, version string) string {
	t.Helper()
	dir := mustMkdirBeads(t)
	_ = openWritable(t, dir).Close()
	raw := mustOpenRawAt(t, DBPath(dir))
	for _, stmt := range renameInverseDDL() {
		if _, err := raw.ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(t.Context(), `UPDATE policy SET key = 'reseen_window' WHERE key = 'redetect_window'`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(t.Context(), `INSERT INTO beads (id, namespace, title, status, priority, type, created_at, updated_at, short_id, depth, depth_at, dedupe_key, last_seen_at, closed_at_at) VALUES ('abc11111111111111111111111', 'lm', 'x', 'open', 2, 'task', 't', 't', 'abc', 3, 'd1', 'k1', 's1', 'c1')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	setUserVersionRaw(t, dir, version)
	return dir
}

func TestMigrateRenamesPrePublicSchema(t *testing.T) {
	for _, version := range []string{"0", "1"} {
		t.Run("version "+version, func(t *testing.T) {
			dir := seedPreRename(t, version)

			res, err := Migrate(t.Context(), dir, migrateOpts(nil))
			if err != nil {
				t.Fatal(err)
			}
			backup := DBPath(dir) + ".bak-prerename"
			if res.From != map[string]int{"0": 0, "1": 1}[version] || res.To != 0 || res.Backup != backup {
				t.Fatalf("result = %+v", res)
			}
			if v := rawCount(t, DBPath(dir), `PRAGMA user_version`); v != 0 {
				t.Fatalf("user_version = %d, want 0", v)
			}
			if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM beads WHERE reasoning_depth = 3 AND reasoning_depth_set_at = 'd1' AND redetect_key = 'k1' AND last_redetected_at = 's1' AND closed_at_set_at = 'c1'`); n != 1 {
				t.Fatalf("renamed row count = %d, want the row kept under the new column names", n)
			}
			if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM coefficients WHERE key = 'redetect_window'`); n != 1 {
				t.Fatalf("redetect_window rows = %d, want 1", n)
			}
			if n := rawCount(t, backup, `SELECT count(*) FROM policy WHERE key = 'reseen_window'`); n != 1 {
				t.Fatalf("backup reseen_window rows = %d, want the old table kept", n)
			}
			migrated := mustOpenRawAt(t, DBPath(dir))
			freshDir := mustMkdirBeads(t)
			_ = openWritable(t, freshDir).Close()
			if got, want := mustSchemaShape(t, migrated), mustSchemaShape(t, mustOpenRaw(t, freshDir)); got != want {
				t.Fatalf("renamed schema differs from schemaDDL:\nrenamed:\n%s\nfresh:\n%s", got, want)
			}
			db, err := Open(t.Context(), dir, migrateOpts(nil))
			if err != nil {
				t.Fatalf("open after rename: %v", err)
			}
			_ = db.Close()
		})
	}
}

func TestMigrateLeavesUnknownVersionZeroSchema(t *testing.T) {
	dir := seedPreRename(t, "0")
	raw := mustOpenRawAt(t, DBPath(dir))
	if _, err := raw.ExecContext(t.Context(), `DROP INDEX idx_beads_short_id`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	res, err := Migrate(t.Context(), dir, migrateOpts(nil))
	if err != nil || res.From != 0 || res.To != 0 || res.Backup != "" {
		t.Fatalf("result = %+v, err = %v, want a no-op", res, err)
	}
	if n := rawCount(t, DBPath(dir), `SELECT count(*) FROM pragma_table_info('beads') WHERE name = 'dedupe_key'`); n != 1 {
		t.Fatalf("dedupe_key columns = %d, want the unknown schema left alone", n)
	}
}

func TestMigrateRenameStopsOnFailures(t *testing.T) {
	t.Run("backup", func(t *testing.T) {
		dir := seedPreRename(t, "0")
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing", "x"), DBPath(dir)+".bak-prerename"); err != nil {
			t.Fatal(err)
		}
		if _, err := Migrate(t.Context(), dir, migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), "back up") {
			t.Fatalf("err = %v, want a backup error", err)
		}
	})
	t.Run("begin", func(t *testing.T) {
		orig := migrateBeginTx
		t.Cleanup(func() { migrateBeginTx = orig })
		migrateBeginTx = func(context.Context, *sql.DB) (*sql.Tx, error) { return nil, errors.New("injected") }
		if _, err := Migrate(t.Context(), seedPreRename(t, "0"), migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), "begin rename") {
			t.Fatalf("err = %v, want a begin rename error", err)
		}
	})
	t.Run("statement", func(t *testing.T) {
		dir := seedPreRename(t, "0")
		orig := migrateBeginTx
		t.Cleanup(func() { migrateBeginTx = orig })
		migrateBeginTx = func(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
			if _, err := db.ExecContext(ctx, `CREATE TABLE coefficients (key TEXT)`); err != nil {
				return nil, err
			}
			return db.BeginTx(ctx, nil)
		}
		if _, err := Migrate(t.Context(), dir, migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), "rename schema") {
			t.Fatalf("err = %v, want a rename schema error", err)
		}
	})
	t.Run("pre-rename reference", func(t *testing.T) {
		orig := referenceSQLOpen
		t.Cleanup(func() { referenceSQLOpen = orig })
		calls := 0
		referenceSQLOpen = func(driver, dsn string) (*sql.DB, error) {
			calls++
			db, err := orig(driver, dsn)
			if calls == 2 {
				_ = db.Close()
			}
			return db, err
		}
		if _, err := Migrate(t.Context(), seedPreRename(t, "0"), migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), "pre-rename reference") {
			t.Fatalf("err = %v, want a pre-rename reference error", err)
		}
	})
	t.Run("pre-rename open", func(t *testing.T) {
		orig := referenceSQLOpen
		t.Cleanup(func() { referenceSQLOpen = orig })
		calls := 0
		referenceSQLOpen = func(driver, dsn string) (*sql.DB, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("injected")
			}
			return orig(driver, dsn)
		}
		if _, err := Migrate(t.Context(), seedPreRename(t, "0"), migrateOpts(nil)); err == nil || !strings.Contains(err.Error(), "open reference schema") {
			t.Fatalf("err = %v, want an open reference schema error", err)
		}
	})
}
