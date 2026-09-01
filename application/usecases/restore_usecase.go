package usecases

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	dbfactory "BackiieTUI/adapters/database"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"BackiieTUI/internal/idgen"
)

// ConfirmPhrase is the exact word the TUI (or any caller) must supply to authorize a restore.
const ConfirmPhrase = "confirmar"

// RestoreUseCase downloads (or, for SQL Server, points directly at) a backup in S3 and restores
// it to a target instance, applying transaction logs in order when the target is a log backup
// or when restoring "all databases". Every attempt is recorded in RestoreRecordRepository.
type RestoreUseCase struct {
	instanceRepo   ports.InstanceRepository
	backupRepo     ports.BackupRecordRepository
	restoreRepo    ports.RestoreRecordRepository
	storageFactory ports.S3Factory
	s3cfgRepo      ports.S3ConfigRepository
}

func NewRestoreUseCase(
	instanceRepo ports.InstanceRepository,
	backupRepo ports.BackupRecordRepository,
	restoreRepo ports.RestoreRecordRepository,
	storageFactory ports.S3Factory,
	s3cfgRepo ports.S3ConfigRepository,
) *RestoreUseCase {
	return &RestoreUseCase{
		instanceRepo:   instanceRepo,
		backupRepo:     backupRepo,
		restoreRepo:    restoreRepo,
		storageFactory: storageFactory,
		s3cfgRepo:      s3cfgRepo,
	}
}

// FindAll returns all instances (used by the TUI to populate the instance selector).
func (uc *RestoreUseCase) FindAll(ctx context.Context) ([]*entities.Instance, error) {
	return uc.instanceRepo.FindAll(ctx)
}

// FindHistory returns every restore attempt ever recorded, newest first.
func (uc *RestoreUseCase) FindHistory(ctx context.Context) ([]*entities.RestoreRecord, error) {
	list, err := uc.restoreRepo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].StartedAt.After(list[j].StartedAt) })
	return list, nil
}

// RestoreBackup restores the backup identified by backupID into targetInstanceName / targetDatabase.
// If backupID names a log backup (SQL Server), the full it chains from and every intervening log
// up to (and including) it are applied in order automatically.
func (uc *RestoreUseCase) RestoreBackup(ctx context.Context, backupID, targetInstanceName, targetDatabase string) error {
	rec, err := uc.backupRepo.FindByID(ctx, backupID)
	if err != nil {
		return fmt.Errorf("registro de respaldo no encontrado: %w", err)
	}

	target, err := uc.resolveInstance(ctx, targetInstanceName)
	if err != nil {
		return err
	}

	s3cfg, err := uc.s3cfgRepo.Get(ctx)
	if err != nil {
		return fmt.Errorf("configuración S3 no disponible: %w", err)
	}

	s3Cfg := &dbfactory.S3Config{
		Endpoint:        s3cfg.Endpoint,
		Bucket:          s3cfg.Bucket,
		AccessKeyID:     s3cfg.AccessKeyID,
		SecretAccessKey: s3cfg.SecretAccessKey,
		PathPrefix:      s3cfg.PathPrefix,
	}

	dbAdapter, err := dbfactory.NewAdapter(target, s3Cfg)
	if err != nil {
		return fmt.Errorf("conectar instancia destino: %w", err)
	}
	defer dbAdapter.Close()

	restoreRec := uc.startRestoreRecord(ctx, target, targetDatabase, backupID)

	var chainIDs []string
	var restoreErr error

	var chain []*entities.BackupRecord
	if target.Engine == entities.EngineSQLServer {
		chain, restoreErr = uc.buildChain(ctx, rec)
	} else {
		chain = []*entities.BackupRecord{rec}
	}

	if restoreErr == nil {
		var storage ports.StorageAdapter
		storage, restoreErr = uc.storageFactory.NewStorage(s3cfg)
		if restoreErr == nil {
			for i, c := range chain {
				chainIDs = append(chainIDs, c.ID)
				
				var reader io.ReadCloser
				reader, restoreErr = storage.Download(ctx, c.FileName)
				if restoreErr != nil {
					break
				}
				
				isLast := i == len(chain)-1
				if i == 0 {
					// Restaurar FULL
					err := dbAdapter.Restore(ctx, targetDatabase, reader, ports.RestoreOptions{
						NoRecovery: !isLast && target.Engine == entities.EngineSQLServer,
					})
					reader.Close()
					if err != nil {
						restoreErr = err
						break
					}
				} else {
					// Restaurar LOG
					logAdapter, ok := dbAdapter.(ports.LogCapableAdapter)
					if !ok {
						reader.Close()
						restoreErr = fmt.Errorf("el adaptador no soporta logs")
						break
					}
					err := logAdapter.RestoreLog(ctx, targetDatabase, reader, isLast)
					reader.Close()
					if err != nil {
						restoreErr = err
						break
					}
				}
			}
		}
	}

	uc.finishRestoreRecord(ctx, restoreRec, chainIDs, restoreErr)
	return restoreErr
}

// RestoreAll restores the latest available recovery point (full + any chained logs) for every
// SQL Server database across every enabled instance, in place. master is excluded — restoring it
// requires stopping the SQL Server service and starting it in single-user mode, which can't be
// automated safely/portably from a live connection (see docs/operaciones.md). Requires the exact
// confirmation phrase; a failure on one database does not stop the rest.
func (uc *RestoreUseCase) RestoreAll(ctx context.Context, confirm string) error {
	if strings.TrimSpace(strings.ToLower(confirm)) != ConfirmPhrase {
		return fmt.Errorf("confirmación inválida: debe escribirse exactamente %q", ConfirmPhrase)
	}

	instances, err := uc.instanceRepo.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("listar instancias: %w", err)
	}

	var failures []string
	for _, inst := range instances {
		if !inst.Enabled {
			continue
		}
		// Allow SQLServer and Postgres/MySQL for restore all.
		if inst.Engine == entities.EngineRedis {
			continue
		}
		points, err := uc.latestRestorePoints(ctx, inst.ID)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", inst.Name, err))
			continue
		}
		for dbName, backupID := range points {
			if err := uc.RestoreBackup(ctx, backupID, inst.Name, dbName); err != nil {
				failures = append(failures, fmt.Sprintf("%s/%s: %v", inst.Name, dbName, err))
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("fallaron %d restauraciones: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

// RestorePointInTime finds the most appropriate backup (Full or Log) that was completed just before
// targetTime, and restores it. For SQL Server, this automatically resolves the chain of Full + Logs.
func (uc *RestoreUseCase) RestorePointInTime(ctx context.Context, targetInstanceName, targetDatabase string, targetTime time.Time) error {
	inst, err := uc.resolveInstance(ctx, targetInstanceName)
	if err != nil {
		return err
	}

	all, err := uc.backupRepo.FindByInstance(ctx, inst.ID)
	if err != nil {
		return err
	}

	var best *entities.BackupRecord
	for _, r := range all {
		if r.DatabaseName != targetDatabase || r.Status != entities.StatusCompleted {
			continue
		}
		if r.StartedAt.After(targetTime) {
			continue // backup is newer than our target time
		}
		if best == nil || r.StartedAt.After(best.StartedAt) {
			best = r
		}
	}

	if best == nil {
		return fmt.Errorf("no se encontró ningún respaldo completado antes de %v para %s/%s", targetTime.Format(time.RFC3339), targetInstanceName, targetDatabase)
	}

	return uc.RestoreBackup(ctx, best.ID, targetInstanceName, targetDatabase)
}


// RestorePoint describes the latest available recovery point for a database, for display before
// confirming a "restore all" operation.
type RestorePoint struct {
	DatabaseName string
	BackupID     string
	At           time.Time
	IsLog        bool
}

// LatestRestorePoints returns the latest recovery point per database for an instance, for the
// TUI to preview before asking for confirmation.
func (uc *RestoreUseCase) LatestRestorePoints(ctx context.Context, instanceID string) ([]RestorePoint, error) {
	points, err := uc.latestRestorePoints(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	var out []RestorePoint
	for dbName, backupID := range points {
		rec, err := uc.backupRepo.FindByID(ctx, backupID)
		if err != nil {
			continue
		}
		out = append(out, RestorePoint{DatabaseName: dbName, BackupID: backupID, At: rec.StartedAt, IsLog: !rec.IsFull()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DatabaseName < out[j].DatabaseName })
	return out, nil
}

// RestoreAllRow previews one database's latest recovery point across every SQL Server instance,
// for the TUI to show before confirming a "restore all" operation.
type RestoreAllRow struct {
	InstanceName string
	DatabaseName string
	BackupID     string
	At           time.Time
	IsLog        bool
}

// PreviewRestoreAll lists the latest recovery point per database, across every enabled SQL
// Server instance, that RestoreAll would restore.
func (uc *RestoreUseCase) PreviewRestoreAll(ctx context.Context) ([]RestoreAllRow, error) {
	instances, err := uc.instanceRepo.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar instancias: %w", err)
	}
	var rows []RestoreAllRow
	for _, inst := range instances {
		if !inst.Enabled {
			continue
		}
		if inst.Engine == entities.EngineRedis {
			continue
		}
		points, err := uc.LatestRestorePoints(ctx, inst.ID)
		if err != nil {
			continue
		}
		for _, p := range points {
			rows = append(rows, RestoreAllRow{
				InstanceName: inst.Name,
				DatabaseName: p.DatabaseName,
				BackupID:     p.BackupID,
				At:           p.At,
				IsLog:        p.IsLog,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].InstanceName != rows[j].InstanceName {
			return rows[i].InstanceName < rows[j].InstanceName
		}
		return rows[i].DatabaseName < rows[j].DatabaseName
	})
	return rows, nil
}

// latestRestorePoints maps database name -> ID of the most recent completed backup to restore
// (the latest log chained off the latest full, or the latest full itself if it has no logs yet).
// master is excluded on purpose (see RestoreAll).
func (uc *RestoreUseCase) latestRestorePoints(ctx context.Context, instanceID string) (map[string]string, error) {
	all, err := uc.backupRepo.FindByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*entities.BackupRecord, len(all))
	for _, r := range all {
		byID[r.ID] = r
	}

	latestFull := map[string]*entities.BackupRecord{}
	for _, r := range all {
		if !r.IsFull() || r.Status != entities.StatusCompleted || strings.EqualFold(r.DatabaseName, "master") {
			continue
		}
		if cur, ok := latestFull[r.DatabaseName]; !ok || r.StartedAt.After(cur.StartedAt) {
			latestFull[r.DatabaseName] = r
		}
	}

	result := make(map[string]string, len(latestFull))
	for db, full := range latestFull {
		result[db] = full.ID
	}
	for _, r := range all {
		if r.Status != entities.StatusCompleted || r.IsFull() {
			continue
		}
		full, ok := latestFull[r.DatabaseName]
		if !ok || r.ParentBackupID != full.ID {
			continue
		}
		cur := byID[result[r.DatabaseName]]
		if cur == nil || r.StartedAt.After(cur.StartedAt) {
			result[r.DatabaseName] = r.ID
		}
	}
	return result, nil
}

// buildChain resolves the ordered list of backups to apply for a restore target: just the full
// if the target is a full, or [full, log1, log2, ..., target] if the target is a log.
func (uc *RestoreUseCase) buildChain(ctx context.Context, target *entities.BackupRecord) ([]*entities.BackupRecord, error) {
	if target.IsFull() {
		return []*entities.BackupRecord{target}, nil
	}

	full, err := uc.backupRepo.FindByID(ctx, target.ParentBackupID)
	if err != nil {
		return nil, fmt.Errorf("full padre del log %s no encontrado: %w", target.ID, err)
	}

	all, err := uc.backupRepo.FindByInstance(ctx, target.InstanceID)
	if err != nil {
		return nil, err
	}
	var logs []*entities.BackupRecord
	for _, r := range all {
		if r.ParentBackupID == full.ID && r.Status == entities.StatusCompleted && !r.StartedAt.After(target.StartedAt) {
			logs = append(logs, r)
		}
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].StartedAt.Before(logs[j].StartedAt) })

	return append([]*entities.BackupRecord{full}, logs...), nil
}



func (uc *RestoreUseCase) startRestoreRecord(ctx context.Context, target *entities.Instance, targetDatabase, backupID string) *entities.RestoreRecord {
	rec := &entities.RestoreRecord{
		ID:             idgen.New(),
		InstanceID:     target.ID,
		InstanceName:   target.Name,
		DatabaseName:   targetDatabase,
		TargetBackupID: backupID,
		Status:         entities.StatusRunning,
		StartedAt:      time.Now().UTC(),
	}
	_ = uc.restoreRepo.Save(ctx, rec)
	return rec
}

func (uc *RestoreUseCase) finishRestoreRecord(ctx context.Context, rec *entities.RestoreRecord, chainIDs []string, restoreErr error) {
	now := time.Now().UTC()
	rec.CompletedAt = &now
	rec.ChainBackupIDs = chainIDs
	if restoreErr != nil {
		rec.Status = entities.StatusFailed
		rec.ErrorMessage = restoreErr.Error()
	} else {
		rec.Status = entities.StatusCompleted
	}
	_ = uc.restoreRepo.Update(ctx, rec)
}

// resolveInstance finds an instance by name (case-insensitive).
func (uc *RestoreUseCase) resolveInstance(ctx context.Context, name string) (*entities.Instance, error) {
	all, err := uc.instanceRepo.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar instancias: %w", err)
	}
	nameLower := strings.ToLower(name)
	for _, inst := range all {
		if strings.ToLower(inst.Name) == nameLower {
			return inst, nil
		}
	}
	return nil, fmt.Errorf("instancia %q no encontrada", name)
}
