package mysql

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	_ "github.com/go-sql-driver/mysql"
)

// Adapter implements ports.DatabaseAdapter for MySQL / MariaDB.
type Adapter struct {
	db       *sql.DB
	instance *entities.Instance
}

func New(inst *entities.Instance) (*Adapter, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=10s&parseTime=true",
		inst.Username, inst.Password, inst.Host, inst.Port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión mysql: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Adapter{db: db, instance: inst}, nil
}

func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		 WHERE SCHEMA_NAME NOT IN ('information_schema','performance_schema','mysql','sys')
		 ORDER BY SCHEMA_NAME`)
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
	engineStr := string(entities.EngineMySQL)
	if a.instance.Engine == entities.EngineMariaDB {
		engineStr = string(entities.EngineMariaDB)
	}
	key := fmt.Sprintf("%s/%s/%s_%s.sql.gz", engineStr, a.instance.Name, database, ts)

	meta := ports.BackupMeta{
		DatabaseName: database,
		SuggestedKey: key,
	}

	dump := exec.CommandContext(ctx, "mysqldump",
		"--host="+a.instance.Host,
		fmt.Sprintf("--port=%d", a.instance.Port),
		"--user="+a.instance.Username,
		"--password="+a.instance.Password,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--compress",
		"--skip-lock-tables",
		database,
	)

	// Pipe mysqldump → gzip
	gzip := exec.CommandContext(ctx, "gzip", "-c")
	var err error
	gzip.Stdin, err = dump.StdoutPipe()
	if err != nil {
		return nil, meta, err
	}
	stdout, err := gzip.StdoutPipe()
	if err != nil {
		return nil, meta, err
	}
	if err := dump.Start(); err != nil {
		return nil, meta, fmt.Errorf("iniciar mysqldump: %w", err)
	}
	if err := gzip.Start(); err != nil {
		_ = dump.Process.Kill()
		return nil, meta, fmt.Errorf("iniciar gzip: %w", err)
	}

	return &pipeReader{ReadCloser: stdout, cmds: []*exec.Cmd{dump, gzip}}, meta, nil
}

// Restore loads a backup into the named database.
// The reader contains gzip-compressed SQL produced by mysqldump | gzip.
func (a *Adapter) Restore(ctx context.Context, database string, reader io.Reader, _ ports.RestoreOptions) error {
	// Create target DB if not exists.
	_, _ = a.db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", database))

	gr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("descomprimir backup mysql: %w", err)
	}
	defer gr.Close()

	mysql := exec.CommandContext(ctx, "mysql",
		fmt.Sprintf("--host=%s", a.instance.Host),
		fmt.Sprintf("--port=%d", a.instance.Port),
		fmt.Sprintf("--user=%s", a.instance.Username),
		fmt.Sprintf("--password=%s", a.instance.Password),
		database,
	)
	mysql.Stdin = gr

	out, err := mysql.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restaurar mysql: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (a *Adapter) Close() error {
	return a.db.Close()
}

type pipeReader struct {
	io.ReadCloser
	cmds []*exec.Cmd
}

func (r *pipeReader) Close() error {
	err := r.ReadCloser.Close()
	for _, cmd := range r.cmds {
		if werr := cmd.Wait(); werr != nil && err == nil {
			err = werr
		}
	}
	return err
}
