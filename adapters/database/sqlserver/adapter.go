package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	_ "github.com/microsoft/go-mssqldb"
)

// Adapter implements ports.DatabaseAdapter (and ports.LogCapableAdapter) for
// SQL Server 2022/2025. Backup and restore use the native
// BACKUP/RESTORE ... TO/FROM URL = 's3://...' feature, so the data goes
// directly between SQL Server and the S3-compatible bucket without passing
// through the machine running BackiieTUI.
type Adapter struct {
	db       *sql.DB
	instance *entities.Instance
	s3cfg    *S3BackupConfig
}

// S3BackupConfig holds the S3 parameters needed for SQL Server's native S3 backup.
type S3BackupConfig struct {
	Endpoint        string // e.g. "s3.amazonaws.com" or "minio.local:9000"
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PathPrefix      string
}

func New(inst *entities.Instance, s3cfg *S3BackupConfig) (*Adapter, error) {
	dsn := buildDSN(inst)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión sqlserver: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Adapter{db: db, instance: inst, s3cfg: s3cfg}, nil
}

// defaultConnTimeoutSeconds is generous on purpose: go-mssqldb's "connection timeout" doubles as
// an idle Read/Write deadline for the entire life of the connection (it resets the deadline on
// every TDS packet, not just at login — see go-mssqldb's timeoutConn). BACKUP/RESTORE ... TO/FROM
// URL can go quiet for a while between STATS progress messages while SQL Server talks to S3, so a
// short timeout here causes spurious "i/o timeout" failures on real backups, not just slow logins.
func buildDSN(inst *entities.Instance) string {
	// trustservercertificate=true: los despliegues on-prem/Docker usan el certificado
	// autofirmado que SQL Server genera al arrancar, no uno emitido por una CA pública.
	trustCert := "true"
	connTimeout := 300 // default
	if inst.Extra != nil {
		if v, ok := inst.Extra["trust_cert"]; ok {
			trustCert = v
		}
		if v, ok := inst.Extra["conn_timeout_seconds"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				connTimeout = n
			}
		}
	}

	u := url.QueryEscape(inst.Username)
	p := url.QueryEscape(inst.Password)

	return fmt.Sprintf("sqlserver://%s:%s@%s:%d?connection+timeout=%d&trustservercertificate=%s",
		u, p, inst.Host, inst.Port, connTimeout, trustCert)
}

func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// ListDatabases returns all databases eligible for backup, including the system databases
// master and msdb (needed for full server disaster recovery). tempdb can't be backed up and
// model doesn't add DR value, so those stay excluded. Callers/instances can further exclude
// databases via Instance.ExcludedDatabases.
func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT name FROM sys.databases
		 WHERE name NOT IN ('tempdb','model')
		   AND state_desc = 'ONLINE'
		 ORDER BY name`)
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

// recoveryModel returns the recovery model of a database (FULL, SIMPLE or BULK_LOGGED).
func (a *Adapter) recoveryModel(ctx context.Context, database string) (string, error) {
	var model string
	err := a.db.QueryRowContext(ctx,
		`SELECT recovery_model_desc FROM sys.databases WHERE name = @p1`, database).Scan(&model)
	if err != nil {
		return "", fmt.Errorf("consultar recovery model de %q: %w", database, err)
	}
	return model, nil
}

// ensureCredential creates the S3 credential used by BACKUP/RESTORE ... TO/FROM URL, if it
// doesn't already exist.
func (a *Adapter) ensureCredential(ctx context.Context, s3cfg *S3BackupConfig) (credName string, err error) {
	host := stripScheme(s3cfg.Endpoint)
	credName = fmt.Sprintf("s3://%s/%s", host, s3cfg.Bucket)
	credSecret := fmt.Sprintf("%s:%s", s3cfg.AccessKeyID, s3cfg.SecretAccessKey)

	_, err = a.db.ExecContext(ctx, fmt.Sprintf(`
		IF NOT EXISTS (SELECT * FROM sys.credentials WHERE name = '%s')
		BEGIN
			CREATE CREDENTIAL [%s]
			WITH IDENTITY = 'S3 Access Key',
			SECRET = '%s'
		END
	`, credName, credName, credSecret))
	if err != nil {
		return "", fmt.Errorf("crear credencial S3: %w", err)
	}
	return credName, nil
}

// objectKey builds the S3 key for a backup file, grouped by database:
// sqlserver/{instancia}/{database}/{database}_{YYYYMMDD_HHMM}.{ext}
//
// This is a bare key (no S3 PathPrefix) — same convention BackupRecord.FileName uses for every
// other engine, since the generic S3 storage adapter (adapters/storage/s3) already prepends its
// configured prefix on every Upload/Download/Delete/List call. SQL Server writes directly to S3
// via T-SQL and bypasses that adapter, so withPrefix below adds the prefix back only for the
// literal s3:// URL handed to BACKUP/RESTORE.
func (a *Adapter) objectKey(database, ext string) string {
	ts := time.Now().UTC().Format("20060102_1504")
	return fmt.Sprintf("%s/%s/%s/%s_%s.%s",
		string(entities.EngineSQLServer), a.instance.Name, database, database, ts, ext)
}

// withPrefix re-applies an S3Config's PathPrefix to a bare object key, for building the
// s3:// URL SQL Server needs in BACKUP/RESTORE ... TO/FROM URL.
func withPrefix(prefix, key string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

func stripScheme(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	return endpoint
}

// Backup performs a full backup (BACKUP DATABASE) directly to S3.
func (a *Adapter) Backup(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error) {
	return a.backup(ctx, database, "bak", func(url string) string {
		return fmt.Sprintf(`BACKUP DATABASE [%s] TO URL = '%s' WITH FORMAT, COMPRESSION, STATS = 25, CHECKSUM`, database, url)
	})
}

// BackupLog performs a transaction log backup (BACKUP LOG) directly to S3.
// Returns ports.ErrLogBackupNotApplicable if the database isn't in FULL/BULK_LOGGED recovery.
func (a *Adapter) BackupLog(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error) {
	model, err := a.recoveryModel(ctx, database)
	if err != nil {
		return nil, ports.BackupMeta{}, err
	}
	if model == "SIMPLE" {
		return nil, ports.BackupMeta{}, ports.ErrLogBackupNotApplicable
	}
	return a.backup(ctx, database, "trn", func(url string) string {
		return fmt.Sprintf(`BACKUP LOG [%s] TO URL = '%s' WITH COMPRESSION, STATS = 25, CHECKSUM`, database, url)
	})
}

func (a *Adapter) backup(ctx context.Context, database, ext string, sqlFor func(url string) string) (io.ReadCloser, ports.BackupMeta, error) {
	key := a.objectKey(database, ext)
	meta := ports.BackupMeta{DatabaseName: database, SuggestedKey: key}

	if _, err := a.ensureCredential(ctx, a.s3cfg); err != nil {
		return nil, meta, err
	}

	host := stripScheme(a.s3cfg.Endpoint)
	backupURL := fmt.Sprintf("s3://%s/%s/%s", host, a.s3cfg.Bucket, withPrefix(a.s3cfg.PathPrefix, key))
	if _, err := a.db.ExecContext(ctx, sqlFor(backupURL)); err != nil {
		return nil, meta, fmt.Errorf("backup sql server (%s): %w", ext, err)
	}

	// SQL Server wrote directly to S3; return an empty reader so the caller can still hash it.
	return io.NopCloser(strings.NewReader("")), meta, nil
}

// Restore runs RESTORE DATABASE FROM URL on the SQL Server side.
// The backup file stays in S3 — SQL Server fetches it directly.
// reader is ignored (SQL Server cannot restore from a Go io.Reader pipe).
func (a *Adapter) Restore(ctx context.Context, database string, _ io.Reader, opts ports.RestoreOptions) error {
	if opts.S3Config == nil {
		return fmt.Errorf("restaurar sql server: S3Config requerido en opts")
	}
	s3cfg := &S3BackupConfig{
		Endpoint:        opts.S3Config.Endpoint,
		Bucket:          opts.S3Config.Bucket,
		AccessKeyID:     opts.S3Config.AccessKeyID,
		SecretAccessKey: opts.S3Config.SecretAccessKey,
		PathPrefix:      opts.S3Config.PathPrefix,
	}
	if _, err := a.ensureCredential(ctx, s3cfg); err != nil {
		return err
	}

	host := stripScheme(s3cfg.Endpoint)
	restoreURL := fmt.Sprintf("s3://%s/%s/%s", host, s3cfg.Bucket, withPrefix(s3cfg.PathPrefix, opts.SourceKey))

	recoveryClause := "RECOVERY"
	if opts.NoRecovery {
		recoveryClause = "NORECOVERY"
	}
	_, err := a.db.ExecContext(ctx, fmt.Sprintf(`
		RESTORE DATABASE [%s]
		FROM URL = '%s'
		WITH REPLACE, STATS = 10, %s
	`, database, restoreURL, recoveryClause))
	if err != nil {
		return fmt.Errorf("restaurar sql server: %w", err)
	}
	return nil
}

// RestoreLog applies a transaction log backup on top of a database left WITH NORECOVERY.
func (a *Adapter) RestoreLog(ctx context.Context, database, sourceKey string, s3Config *entities.S3Config, recovery bool) error {
	if s3Config == nil {
		return fmt.Errorf("restaurar log sql server: S3Config requerido")
	}
	s3cfg := &S3BackupConfig{
		Endpoint:        s3Config.Endpoint,
		Bucket:          s3Config.Bucket,
		AccessKeyID:     s3Config.AccessKeyID,
		SecretAccessKey: s3Config.SecretAccessKey,
		PathPrefix:      s3Config.PathPrefix,
	}
	if _, err := a.ensureCredential(ctx, s3cfg); err != nil {
		return err
	}

	host := stripScheme(s3cfg.Endpoint)
	restoreURL := fmt.Sprintf("s3://%s/%s/%s", host, s3cfg.Bucket, withPrefix(s3cfg.PathPrefix, sourceKey))

	recoveryClause := "NORECOVERY"
	if recovery {
		recoveryClause = "RECOVERY"
	}
	_, err := a.db.ExecContext(ctx, fmt.Sprintf(`
		RESTORE LOG [%s]
		FROM URL = '%s'
		WITH STATS = 10, %s
	`, database, restoreURL, recoveryClause))
	if err != nil {
		return fmt.Errorf("restaurar log sql server: %w", err)
	}
	return nil
}

func (a *Adapter) Close() error {
	return a.db.Close()
}

var (
	_ ports.DatabaseAdapter   = (*Adapter)(nil)
	_ ports.LogCapableAdapter = (*Adapter)(nil)
)
