package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const CareerVectorLength = 122

type Account struct {
	PID  uint32
	Name string
}

type AccountCredential struct {
	Password  []byte
	PreHashed bool
}

type LeaderboardRow struct {
	PID      uint32
	Cash     float32
	ModeStat float32
	MapID    uint32
}

type DB struct {
	sql *sql.DB
}

func Open(path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, schema); err != nil {
		database.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(database, ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &DB{sql: database}, nil
}

func (d *DB) Close() error {
	return d.sql.Close()
}

func (d *DB) FindOrCreateAccount(ctx context.Context, name string) (Account, error) {
	var account Account
	err := d.sql.QueryRowContext(ctx, "SELECT pid, psn_id FROM accounts WHERE psn_id=?", name).
		Scan(&account.PID, &account.Name)
	if err == nil {
		_, _ = d.sql.ExecContext(ctx, "UPDATE accounts SET last_login_at=? WHERE pid=?", time.Now().Unix(), account.PID)
		return account, nil
	}
	if err != sql.ErrNoRows {
		return Account{}, err
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	var nextPID uint32
	if err := tx.QueryRowContext(ctx, "SELECT value FROM counters WHERE name='next_pid'").Scan(&nextPID); err != nil {
		return Account{}, err
	}
	if nextPID < 200 {
		nextPID = 200
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, "UPDATE counters SET value=? WHERE name='next_pid'", nextPID+1); err != nil {
		return Account{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO accounts(pid,psn_id,created_at,last_login_at) VALUES(?,?,?,?)",
		nextPID, name, now, now); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return Account{PID: nextPID, Name: name}, nil
}

// PreloadAccount inserts a named account and its optional plain-text password
// without modifying an existing account. It returns true only when a new row
// was inserted. A nil password uses the server default during login.
func (d *DB) PreloadAccount(ctx context.Context, name string, password []byte) (bool, error) {
	var exists int
	err := d.sql.QueryRowContext(ctx, "SELECT 1 FROM accounts WHERE psn_id=?", name).Scan(&exists)
	if err == nil {
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var nextPID uint32
	if err := tx.QueryRowContext(ctx, "SELECT value FROM counters WHERE name='next_pid'").Scan(&nextPID); err != nil {
		return false, err
	}
	if nextPID < 200 {
		nextPID = 200
	}
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO accounts
		 (pid,psn_id,created_at,last_login_at,password,password_prehashed)
		 VALUES(?,?,?,?,?,0)`,
		nextPID, name, now, now, password)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		return false, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, "UPDATE counters SET value=? WHERE name='next_pid'", nextPID+1); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (d *DB) NameForPID(ctx context.Context, pid uint32) string {
	var name string
	if err := d.sql.QueryRowContext(ctx, "SELECT psn_id FROM accounts WHERE pid=?", pid).Scan(&name); err != nil {
		return fmt.Sprintf("PID%d", pid)
	}
	return name
}

func (d *DB) SetAccountCredential(ctx context.Context, pid uint32, password []byte, preHashed bool) error {
	_, err := d.sql.ExecContext(ctx,
		"UPDATE accounts SET password=?, password_prehashed=? WHERE pid=?",
		append([]byte(nil), password...), preHashed, pid)
	return err
}

func (d *DB) AccountCredential(ctx context.Context, pid uint32) (AccountCredential, bool, error) {
	var credential AccountCredential
	var preHashed bool
	err := d.sql.QueryRowContext(ctx,
		"SELECT password,password_prehashed FROM accounts WHERE pid=? AND password IS NOT NULL",
		pid).Scan(&credential.Password, &preHashed)
	if err == sql.ErrNoRows {
		return AccountCredential{}, false, nil
	}
	if err != nil {
		return AccountCredential{}, false, err
	}
	credential.PreHashed = preHashed
	return credential, true, nil
}

func (d *DB) Career(ctx context.Context, pid uint32) ([]float32, bool, error) {
	var blob []byte
	err := d.sql.QueryRowContext(ctx, "SELECT vec0 FROM career_stats WHERE pid=?", pid).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return unpackVector(blob), true, nil
}

func (d *DB) CareerCash(ctx context.Context, pid uint32) float32 {
	vector, ok, err := d.Career(ctx, pid)
	if err != nil || !ok || len(vector) <= 5 {
		return 0
	}
	return vector[5]
}

func (d *DB) Leaderboard(ctx context.Context, pid, category uint32) (LeaderboardRow, bool, error) {
	var row LeaderboardRow
	row.PID = pid
	err := d.sql.QueryRowContext(ctx,
		"SELECT cash,mode_stat,map_id FROM leaderboard_stats WHERE pid=? AND category=?",
		pid, category).Scan(&row.Cash, &row.ModeStat, &row.MapID)
	if err == sql.ErrNoRows {
		return LeaderboardRow{}, false, nil
	}
	return row, err == nil, err
}

func (d *DB) Ranked(ctx context.Context, category uint32) ([]LeaderboardRow, error) {
	rows, err := d.sql.QueryContext(ctx,
		"SELECT pid,cash,mode_stat,map_id FROM leaderboard_stats WHERE category=? AND cash>0 ORDER BY cash DESC",
		category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaderboardRow
	for rows.Next() {
		var row LeaderboardRow
		if err := rows.Scan(&row.PID, &row.Cash, &row.ModeStat, &row.MapID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (d *DB) PersistMatch(ctx context.Context, pid uint32, vector []float32, rows map[uint32]LeaderboardRow) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO career_stats(pid,vec0,updated_at) VALUES(?,?,?)
		 ON CONFLICT(pid) DO UPDATE SET vec0=excluded.vec0,updated_at=excluded.updated_at`,
		pid, packVector(vector), now); err != nil {
		return err
	}
	for category, row := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO leaderboard_stats(pid,category,cash,mode_stat,map_id,updated_at)
			 VALUES(?,?,?,?,?,?)
			 ON CONFLICT(pid,category) DO UPDATE SET cash=excluded.cash,
			 mode_stat=excluded.mode_stat,map_id=excluded.map_id,updated_at=excluded.updated_at`,
			pid, category, row.Cash, row.ModeStat, row.MapID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func packVector(vector []float32) []byte {
	out := make([]byte, CareerVectorLength*4)
	for index := 0; index < CareerVectorLength && index < len(vector); index++ {
		binary.LittleEndian.PutUint32(out[index*4:index*4+4], math.Float32bits(vector[index]))
	}
	return out
}

func unpackVector(blob []byte) []float32 {
	count := len(blob) / 4
	out := make([]float32, count)
	for index := range out {
		out[index] = math.Float32frombits(binary.LittleEndian.Uint32(blob[index*4 : index*4+4]))
	}
	return out
}

const schema = `
PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS accounts (
  pid INTEGER PRIMARY KEY,
  psn_id TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  last_login_at INTEGER
) STRICT;

INSERT OR IGNORE INTO accounts(pid,psn_id,created_at) VALUES
  (1,'_server',strftime('%s','now')),
  (2,'_authentication',strftime('%s','now'));

CREATE TABLE IF NOT EXISTS counters (
  name TEXT PRIMARY KEY,
  value INTEGER NOT NULL
) STRICT;
INSERT OR IGNORE INTO counters(name,value) VALUES ('next_pid',10000);

CREATE TABLE IF NOT EXISTS career_stats (
  pid INTEGER PRIMARY KEY REFERENCES accounts(pid) ON DELETE CASCADE,
  vec0 BLOB NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS leaderboard_stats (
  pid INTEGER NOT NULL REFERENCES accounts(pid) ON DELETE CASCADE,
  category INTEGER NOT NULL,
  cash REAL NOT NULL DEFAULT 0,
  mode_stat REAL NOT NULL DEFAULT 0,
  map_id INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(pid,category)
) STRICT;
CREATE INDEX IF NOT EXISTS leaderboard_by_cat ON leaderboard_stats(category,cash DESC);
`

func migrate(database *sql.DB, ctx context.Context) error {
	columns := []struct {
		name        string
		declaration string
	}{
		{"password", "BLOB"},
		{"password_prehashed", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		var count int
		if err := database.QueryRowContext(ctx,
			"SELECT count(*) FROM pragma_table_info('accounts') WHERE name=?", column.name).
			Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := database.ExecContext(ctx,
				fmt.Sprintf("ALTER TABLE accounts ADD COLUMN %s %s", column.name, column.declaration)); err != nil {
				return err
			}
		}
	}
	return nil
}
