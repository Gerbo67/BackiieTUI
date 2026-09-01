package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Adapter implements ports.DatabaseAdapter for PostgreSQL.
type Adapter struct {
	db       *sql.DB
	instance *entities.Instance
}

func New(inst *entities.Instance) (*Adapter, error) {
	dsn := buildDSN(inst)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión postgres: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Adapter{db: db, instance: inst}, nil
}

func buildDSN(inst *entities.Instance) string {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s sslmode=disable connect_timeout=10",
		inst.Host, inst.Port, inst.Username, inst.Password)
	if inst.Database != "" {
		dsn += " dbname=" + inst.Database
	}
	return dsn
}

func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres') ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

func (a *Adapter) Backup(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error) {
	ts := time.Now().UTC().Format("20060102_150405")
	key := fmt.Sprintf("%s/%s/%s_%s.sql.gz",
		string(entities.EnginePostgres), a.instance.Name, database, ts)

	meta := ports.BackupMeta{
		DatabaseName: database,
		SuggestedKey: key,
	}

	// pg_dump pipes to gzip
	pgDump := exec.CommandContext(ctx, "pg_dump",
		"-h", a.instance.Host,
		"-p", fmt.Sprintf("%d", a.instance.Port),
		"-U", a.instance.Username,
		"-d", database,
		"--no-password",
		"--compress=9",
		"--format=plain",
		"--no-owner",
		"--no-privileges",
	)
	pgDump.Env = append(os.Environ(), "PGPASSWORD="+a.instance.Password)

	stdout, err := pgDump.StdoutPipe()
	if err != nil {
		return nil, meta, fmt.Errorf("stdout pipe: %w", err)
	}

	reader := &cmdReader{ReadCloser: stdout, cmd: pgDump}
	pgDump.Stderr = &reader.stderr

	if err := pgDump.Start(); err != nil {
		return nil, meta, fmt.Errorf("iniciar pg_dump: %w", err)
	}

	return reader, meta, nil
}

// Restore loads a backup into the named database.
// The reader contains gzip-compressed SQL (pg_dump plain+compress) or plain SQL.
func (a *Adapter) Restore(ctx context.Context, database string, reader io.Reader, _ ports.RestoreOptions) error {
	// Create target DB if not exists (best-effort — ignore if already there).
	_, _ = a.db.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, database))

	// Detect gzip by magic bytes, fall back to plain SQL if not compressed.
	peek := make([]byte, 2)
	n, _ := io.ReadFull(reader, peek)
	combined := io.MultiReader(bytes.NewReader(peek[:n]), reader)

	var sqlReader io.Reader = combined
	if n == 2 && peek[0] == 0x1f && peek[1] == 0x8b {
		gr, err := gzip.NewReader(combined)
		if err == nil {
			defer gr.Close()
			sqlReader = gr
		}
	}

	psql := exec.CommandContext(ctx, "psql",
		"-h", a.instance.Host,
		"-p", fmt.Sprintf("%d", a.instance.Port),
		"-U", a.instance.Username,
		"-d", database,
		"--no-password",
	)
	psql.Env = append(os.Environ(), "PGPASSWORD="+a.instance.Password)
	psql.Stdin = sqlReader

	out, err := psql.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restaurar postgres: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a *Adapter) Close() error {
	return a.db.Close()
}

// cmdReader wraps the stdout pipe of a command and waits for exit on close.
type cmdReader struct {
	io.ReadCloser
	cmd    *exec.Cmd
	stderr bytes.Buffer
}

func (r *cmdReader) Close() error {
	err := r.ReadCloser.Close()
	if waitErr := r.cmd.Wait(); waitErr != nil && err == nil {
		if msg := strings.TrimSpace(r.stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", waitErr, msg)
		} else {
			err = waitErr
		}
	}
	return err
}
