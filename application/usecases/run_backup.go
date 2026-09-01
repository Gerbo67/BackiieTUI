package usecases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	dbfactory "BackiieTUI/adapters/database"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"BackiieTUI/internal/hash"
	"BackiieTUI/internal/idgen"
)

// RunBackupUseCase orchestrates backup operations end-to-end: full backups for every engine,
// and (SQL Server only) transaction log backups chained off the latest full.
type RunBackupUseCase struct {
	instanceRepo   ports.InstanceRepository
	backupRepo     ports.BackupRecordRepository
	storageFactory ports.S3Factory
	s3cfgRepo      ports.S3ConfigRepository
	retentionRepo  ports.RetentionRepository
	notifier       ports.Notifier
	logger         *slog.Logger
}

func NewRunBackupUseCase(
	instanceRepo ports.InstanceRepository,
	backupRepo ports.BackupRecordRepository,
	storageFactory ports.S3Factory,
	s3cfgRepo ports.S3ConfigRepository,
	retentionRepo ports.RetentionRepository,
	notifier ports.Notifier,
	logger *slog.Logger,
) *RunBackupUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &RunBackupUseCase{
		instanceRepo:   instanceRepo,
		backupRepo:     backupRepo,
		storageFactory: storageFactory,
		s3cfgRepo:      s3cfgRepo,
		retentionRepo:  retentionRepo,
		notifier:       notifier,
		logger:         logger,
	}
}

// RunForInstance backs up (full) all databases on the given instance.
func (uc *RunBackupUseCase) RunForInstance(ctx context.Context, instanceID string) error {
	return uc.runForInstance(ctx, instanceID, entities.BackupTypeFull)
}

// RunForAll backs up (full) all enabled instances.
func (uc *RunBackupUseCase) RunForAll(ctx context.Context) error {
	return uc.runForAll(ctx, entities.BackupTypeFull)
}

// RunLogsForInstance backs up transaction logs for every SQL Server database on the given
// instance that has a completed full backup to chain from. No-op for other engines.
func (uc *RunBackupUseCase) RunLogsForInstance(ctx context.Context, instanceID string) error {
	return uc.runForInstance(ctx, instanceID, entities.BackupTypeLog)
}

// RunLogsForAll backs up transaction logs for every enabled SQL Server instance.
func (uc *RunBackupUseCase) RunLogsForAll(ctx context.Context) error {
	return uc.runForAll(ctx, entities.BackupTypeLog)
}

func (uc *RunBackupUseCase) runForAll(ctx context.Context, backupType entities.BackupType) error {
	instances, err := uc.instanceRepo.FindAll(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Enabled {
			_ = uc.runForInstance(ctx, inst.ID, backupType)
		}
	}
	return nil
}

func (uc *RunBackupUseCase) runForInstance(ctx context.Context, instanceID string, backupType entities.BackupType) error {
	inst, err := uc.instanceRepo.FindByID(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("instancia %q: %w", instanceID, err)
	}
	if !inst.Enabled {
		return nil
	}
	if backupType == entities.BackupTypeLog && inst.Engine != entities.EngineSQLServer {
		return nil // solo SQL Server soporta backup de log
	}

	// Always read S3 config fresh so scheduler picks up changes made via TUI
	// without requiring a service restart.
	s3cfg, err := uc.s3cfgRepo.Get(ctx)
	if err != nil {
		return fmt.Errorf("config S3: %w", err)
	}
	storage, err := uc.storageFactory.NewStorage(s3cfg)
	if err != nil {
		return fmt.Errorf("conectar S3: %w", err)
	}

	retainDays := 7
	if pol, err := uc.retentionRepo.FindByInstance(ctx, instanceID); err == nil {
		retainDays = pol.RetainDays
	} else if global, err := uc.retentionRepo.FindGlobal(ctx); err == nil {
		retainDays = global.RetainDays
	}

	s3Cfg := &dbfactory.S3Config{
		Endpoint:        s3cfg.Endpoint,
		Bucket:          s3cfg.Bucket,
		AccessKeyID:     s3cfg.AccessKeyID,
		SecretAccessKey: s3cfg.SecretAccessKey,
		PathPrefix:      s3cfg.PathPrefix,
	}

	adapter, err := dbfactory.NewAdapter(inst, s3Cfg)
	if err != nil {
		return err
	}
	defer adapter.Close()

	logAdapter, canLog := adapter.(ports.LogCapableAdapter)
	if backupType == entities.BackupTypeLog && !canLog {
		return nil
	}

	databases, err := adapter.ListDatabases(ctx)
	if err != nil {
		return fmt.Errorf("listar bases de datos: %w", err)
	}

	excluded := make(map[string]bool, len(inst.ExcludedDatabases))
	for _, db := range inst.ExcludedDatabases {
		excluded[db] = true
	}

	for _, dbName := range databases {
		if excluded[dbName] {
			continue
		}

		if backupType == entities.BackupTypeFull {
			if err := uc.backupDatabase(ctx, inst, dbName, entities.BackupTypeFull, "",
				adapter.Backup, storage, retainDays); err != nil {
				uc.logFailure(inst, dbName, err)
			}
			continue
		}

		// Log backup: needs a completed full to chain from.
		parent, err := uc.latestCompletedFull(ctx, inst.ID, dbName)
		if err != nil || parent == nil {
			continue // sin full previo, no hay cadena que respaldar
		}
		if err := uc.backupDatabase(ctx, inst, dbName, entities.BackupTypeLog, parent.ID,
			logAdapter.BackupLog, storage, retainDays); err != nil {
			if errors.Is(err, ports.ErrLogBackupNotApplicable) {
				continue // recovery model SIMPLE (master, msdb por defecto) — no es un fallo
			}
			uc.logFailure(inst, dbName, err)
		}
	}
	return nil
}

func (uc *RunBackupUseCase) logFailure(inst *entities.Instance, dbName string, err error) {
	uc.logger.Error("backup fallido",
		"instancia", inst.Name, "bd", dbName, "motor", inst.Engine.String(), "err", err)
	if uc.notifier != nil {
		uc.notifier.NotifyBackupFailed(inst.Name, dbName, err)
	}
}

// latestCompletedFull finds the most recent completed full backup for instance+database, to
// serve as the parent of a new log backup.
func (uc *RunBackupUseCase) latestCompletedFull(ctx context.Context, instanceID, dbName string) (*entities.BackupRecord, error) {
	records, err := uc.backupRepo.FindByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	var latest *entities.BackupRecord
	for _, r := range records {
		if r.DatabaseName != dbName || r.Status != entities.StatusCompleted || !r.IsFull() {
			continue
		}
		if latest == nil || r.StartedAt.After(latest.StartedAt) {
			latest = r
		}
	}
	return latest, nil
}

type backupFn func(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error)

func (uc *RunBackupUseCase) backupDatabase(
	ctx context.Context,
	inst *entities.Instance,
	dbName string,
	backupType entities.BackupType,
	parentBackupID string,
	doBackup backupFn,
	storage ports.StorageAdapter,
	retainDays int,
) error {
	record := &entities.BackupRecord{
		ID:             idgen.New(),
		InstanceID:     inst.ID,
		InstanceName:   inst.Name,
		DatabaseName:   dbName,
		Engine:         inst.Engine,
		Type:           backupType,
		ParentBackupID: parentBackupID,
		Status:         entities.StatusRunning,
		StartedAt:      time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().AddDate(0, 0, retainDays),
		RetainDays:     retainDays,
	}

	if err := uc.backupRepo.Save(ctx, record); err != nil {
		return fmt.Errorf("guardar registro inicial: %w", err)
	}

	if uc.notifier != nil {
		uc.notifier.NotifyBackupStarted(inst.Name, dbName)
	}

	reader, meta, err := doBackup(ctx, dbName)
	if err != nil {
		_ = uc.backupRepo.UpdateStatus(ctx, record.ID, entities.StatusFailed, err.Error())
		return err
	}

	record.FileName = meta.SuggestedKey

	hr := hash.NewHashingReader(reader)

	uploadedBytes, err := storage.Upload(ctx, meta.SuggestedKey, hr)
	if err != nil {
		reader.Close()
		_ = uc.backupRepo.UpdateStatus(ctx, record.ID, entities.StatusFailed, err.Error())
		return err
	}

	// Verify the dump process exited successfully. For tools like pg_dump,
	// mysqldump, and our docker exec sqlserver extraction, a non-zero exit
	// after uploading 0 bytes means the backup silently failed.
	if closeErr := reader.Close(); closeErr != nil {
		_ = storage.DeleteObject(ctx, meta.SuggestedKey) // remove empty/partial file
		_ = uc.backupRepo.UpdateStatus(ctx, record.ID, entities.StatusFailed, closeErr.Error())
		return closeErr
	}

	_ = storage.TagExpiry(ctx, meta.SuggestedKey, record.ExpiresAt)

	now := time.Now().UTC()
	record.Status = entities.StatusCompleted
	record.SizeBytes = uploadedBytes
	record.HashSHA256 = hr.Sum()
	record.CompletedAt = &now
	record.DurationMs = now.Sub(record.StartedAt).Milliseconds()

	if err := uc.backupRepo.Update(ctx, record); err != nil {
		return fmt.Errorf("actualizar registro: %w", err)
	}

	uc.logger.Info("backup completado",
		"instancia", inst.Name,
		"bd", dbName,
		"tipo", string(backupType),
		"size_bytes", record.SizeBytes,
		"duration_ms", record.DurationMs)

	if uc.notifier != nil {
		uc.notifier.NotifyBackupCompleted(inst.Name, dbName, record.ID)
	}
	return nil
}
