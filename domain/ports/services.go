package ports

import (
	"context"
	"errors"
	"io"
	"time"

	"BackiieTUI/domain/entities"
)

// ErrLogBackupNotApplicable is returned by LogCapableAdapter.BackupLog when the database is not
// in a recovery model that supports transaction log backups (e.g. SIMPLE — master, msdb by
// default). Callers should skip it silently rather than treat it as a failure.
var ErrLogBackupNotApplicable = errors.New("la base de datos no soporta respaldo de log (recovery model SIMPLE)")

// BackupMeta contains metadata returned alongside the backup stream.
type BackupMeta struct {
	DatabaseName string
	SuggestedKey string // suggested S3 object key
}

// RestoreOptions carries context needed by engine adapters to perform a restore.
type RestoreOptions struct {
	// SourceKey is the original S3 object key from the backup record.
	// SQL Server adapters use this to run RESTORE DATABASE FROM URL directly,
	// without downloading the file through BackiieTUI.
	SourceKey string
	// S3Config is required for SQL Server native restore (FROM URL).
	S3Config *entities.S3Config
	// NoRecovery leaves the database in a restoring state (WITH NORECOVERY) so
	// subsequent transaction log backups can be applied. SQL Server only.
	NoRecovery bool
}

// DatabaseAdapter is implemented by each database engine adapter.
type DatabaseAdapter interface {
	// Ping verifies the connection is alive.
	Ping(ctx context.Context) error
	// ListDatabases returns all user databases on the instance.
	ListDatabases(ctx context.Context) ([]string, error)
	// Backup streams a compressed backup of the given database.
	// The caller is responsible for closing the reader.
	Backup(ctx context.Context, database string) (io.ReadCloser, BackupMeta, error)
	// Restore loads a backup into the named database.
	// reader contains the raw backup bytes (gzip-compressed for most engines).
	// For SQL Server, reader is nil and opts.SourceKey carries the S3 key.
	Restore(ctx context.Context, database string, reader io.Reader, opts RestoreOptions) error
	// Close releases the underlying connection.
	Close() error
}

// LogCapableAdapter is implemented by engines that support transaction log backups
// (currently only SQL Server). Callers type-assert DatabaseAdapter to this interface.
type LogCapableAdapter interface {
	// BackupLog streams a transaction log backup of the given database.
	// Returns ErrLogBackupNotApplicable if the database's recovery model doesn't support it.
	BackupLog(ctx context.Context, database string) (io.ReadCloser, BackupMeta, error)
	// RestoreLog applies a transaction log backup on top of a database left WITH NORECOVERY.
	// recovery=true marks the last log in a chain, bringing the database back online.
	RestoreLog(ctx context.Context, database string, reader io.Reader, recovery bool) error
}

// StorageObject represents a file stored in S3.
type StorageObject struct {
	Key          string
	SizeBytes    int64
	LastModified time.Time
	ETag         string
}

// StorageAdapter abstracts S3-compatible object storage.
type StorageAdapter interface {
	// Upload streams data to the given key using multipart upload.
	Upload(ctx context.Context, key string, body io.Reader) (int64, error)
	// Download streams the object at the given key to the caller.
	// The caller is responsible for closing the returned reader.
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	// ListObjects lists all objects under the given prefix.
	ListObjects(ctx context.Context, prefix string) ([]StorageObject, error)
	// DeleteObject removes the given key.
	DeleteObject(ctx context.Context, key string) error
	// TagExpiry tags the object so an S3 lifecycle rule can expire it.
	TagExpiry(ctx context.Context, key string, expiresAt time.Time) error
}

// LifecycleManager manages S3 lifecycle rules on the configured bucket.
// Get reads all current rules; Put replaces ALL rules atomically (S3 constraint).
type LifecycleManager interface {
	GetLifecycleRules(ctx context.Context) ([]entities.LifecycleRule, error)
	PutLifecycleRules(ctx context.Context, rules []entities.LifecycleRule) error
}

// S3Factory creates fresh S3 adapter instances from a config snapshot.
// Using a factory instead of a shared adapter means callers always pick up the
// latest S3 credentials saved via the TUI — no service restart required.
type S3Factory interface {
	NewStorage(cfg *entities.S3Config) (StorageAdapter, error)
	NewLifecycle(cfg *entities.S3Config) (LifecycleManager, error)
}

// DBBackupSource is implemented by the local persistence store (BBolt) so BackiieTUI can back
// itself up: if the server holding it is lost, its own instance/backup/restore history can be
// recovered from S3 too.
type DBBackupSource interface {
	Backup(w io.Writer) error
}

// Notifier sends backup lifecycle events to the TUI.
type Notifier interface {
	NotifyBackupStarted(instanceName, dbName string)
	NotifyBackupProgress(instanceName, dbName string, bytesWritten int64)
	NotifyBackupCompleted(instanceName, dbName, backupID string)
	NotifyBackupFailed(instanceName, dbName string, err error)
}
