package usecases

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// BackupQueryUseCase handles read operations on backup records, plus deletion and retention.
type BackupQueryUseCase struct {
	backupRepo     ports.BackupRecordRepository
	storageFactory ports.S3Factory
	s3cfgRepo      ports.S3ConfigRepository
	retentionRepo  ports.RetentionRepository
}

func NewBackupQueryUseCase(
	backupRepo ports.BackupRecordRepository,
	storageFactory ports.S3Factory,
	s3cfgRepo ports.S3ConfigRepository,
	retentionRepo ports.RetentionRepository,
) *BackupQueryUseCase {
	return &BackupQueryUseCase{
		backupRepo:     backupRepo,
		storageFactory: storageFactory,
		s3cfgRepo:      s3cfgRepo,
		retentionRepo:  retentionRepo,
	}
}

func (uc *BackupQueryUseCase) FindAll(ctx context.Context) ([]*entities.BackupRecord, error) {
	return uc.backupRepo.FindAll(ctx)
}

func (uc *BackupQueryUseCase) FindByInstance(ctx context.Context, instanceID string) ([]*entities.BackupRecord, error) {
	return uc.backupRepo.FindByInstance(ctx, instanceID)
}

// DeleteBackup removes the S3 object and marks the record as expired. If the backup is a full
// backup, every log backup chained off it (ParentBackupID) is deleted first — a log without its
// full is useless, so they're never left orphaned.
func (uc *BackupQueryUseCase) DeleteBackup(ctx context.Context, id string) error {
	rec, err := uc.backupRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if rec.IsFull() {
		all, err := uc.backupRepo.FindAll(ctx)
		if err != nil {
			return fmt.Errorf("listar respaldos para cascada: %w", err)
		}
		for _, child := range all {
			if child.ParentBackupID == id {
				if err := uc.deleteOne(ctx, child); err != nil {
					return fmt.Errorf("eliminar log %s en cascada: %w", child.ID, err)
				}
			}
		}
	}

	return uc.deleteOne(ctx, rec)
}

func (uc *BackupQueryUseCase) deleteOne(ctx context.Context, rec *entities.BackupRecord) error {
	if rec.FileName != "" && uc.storageFactory != nil {
		if s3cfg, err := uc.s3cfgRepo.Get(ctx); err == nil {
			if storage, err := uc.storageFactory.NewStorage(s3cfg); err == nil {
				if err := storage.DeleteObject(ctx, rec.FileName); err != nil {
					return fmt.Errorf("eliminar objeto S3: %w", err)
				}
			}
		}
	}
	return uc.backupRepo.UpdateStatus(ctx, rec.ID, entities.StatusExpired, "eliminado manualmente")
}

// ApplyRetention deletes backups that have passed their expiry date. For SQL Server, it defers
// to ApplySQLServerRetention instead, which enforces the golden rule (never fewer than
// entities.MinVerifiedFullBackups verified fulls left in the bucket). Other engines keep the
// simple date-based expiry.
func (uc *BackupQueryUseCase) ApplyRetention(ctx context.Context) (int, error) {
	expired, err := uc.backupRepo.FindExpired(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, rec := range expired {
		if rec.Engine == entities.EngineSQLServer {
			continue // manejado por ApplySQLServerRetention
		}
		if err := uc.DeleteBackup(ctx, rec.ID); err == nil {
			count++
		}
	}

	n, err := uc.ApplySQLServerRetention(ctx)
	count += n
	return count, err
}

// ApplySQLServerRetention walks each (instance, database) chain of completed full backups,
// oldest first, and deletes the ones older than the configured retention window — but only if
// doing so still leaves at least entities.MinVerifiedFullBackups completed fulls whose S3
// object is confirmed to still exist. As soon as that condition can't be met, the sweep for
// that database stops (older backups are left alone too, rather than risk having fewer than the
// minimum). Deleting a full cascades to every log backup chained off it.
func (uc *BackupQueryUseCase) ApplySQLServerRetention(ctx context.Context) (int, error) {
	all, err := uc.backupRepo.FindAll(ctx)
	if err != nil {
		return 0, err
	}

	type groupKey struct{ instanceID, database string }
	groups := map[groupKey][]*entities.BackupRecord{}
	for _, r := range all {
		if r.Engine != entities.EngineSQLServer || !r.IsFull() || r.Status != entities.StatusCompleted {
			continue
		}
		k := groupKey{r.InstanceID, r.DatabaseName}
		groups[k] = append(groups[k], r)
	}

	s3cfg, err := uc.s3cfgRepo.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("config S3: %w", err)
	}
	storage, err := uc.storageFactory.NewStorage(s3cfg)
	if err != nil {
		return 0, fmt.Errorf("conectar S3: %w", err)
	}

	count := 0
	var firstErr error
	for k, fulls := range groups {
		sort.Slice(fulls, func(i, j int) bool { return fulls[i].StartedAt.Before(fulls[j].StartedAt) })

		retainDays := uc.retainDaysFor(ctx, k.instanceID)
		cutoff := time.Now().UTC().AddDate(0, 0, -retainDays)

		deleted := make(map[string]bool)
		for _, candidate := range fulls {
			if !candidate.StartedAt.Before(cutoff) {
				break // ya llegamos a backups dentro de la ventana de retención
			}

			verified := 0
			for _, other := range fulls {
				if other.ID == candidate.ID || deleted[other.ID] {
					continue
				}
				if uc.existsInS3(ctx, storage, other.FileName) {
					verified++
				}
			}
			if verified < entities.MinVerifiedFullBackups {
				break // regla de oro: no bajar de 2 fulls verificados
			}

			if err := uc.DeleteBackup(ctx, candidate.ID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			deleted[candidate.ID] = true
			count++
		}
	}
	return count, firstErr
}

func (uc *BackupQueryUseCase) existsInS3(ctx context.Context, storage ports.StorageAdapter, key string) bool {
	if key == "" {
		return false
	}
	objs, err := storage.ListObjects(ctx, key)
	if err != nil {
		return false
	}
	// ListObjects devuelve la key completa en S3 (incluye el PathPrefix del bucket, si hay uno
	// configurado); key acá es siempre la key "pelada" que se guarda en BackupRecord.FileName.
	for _, o := range objs {
		if strings.HasSuffix(o.Key, key) {
			return true
		}
	}
	return false
}

func (uc *BackupQueryUseCase) retainDaysFor(ctx context.Context, instanceID string) int {
	if pol, err := uc.retentionRepo.FindByInstance(ctx, instanceID); err == nil {
		return pol.RetainDays
	}
	if global, err := uc.retentionRepo.FindGlobal(ctx); err == nil {
		return global.RetainDays
	}
	def := entities.DefaultRetentionPolicy()
	return def.RetainDays
}

// RetentionUseCase manages retention policies.
type RetentionUseCase struct {
	repo ports.RetentionRepository
}

func NewRetentionUseCase(repo ports.RetentionRepository) *RetentionUseCase {
	return &RetentionUseCase{repo: repo}
}

func (uc *RetentionUseCase) Save(ctx context.Context, p *entities.RetentionPolicy) error {
	if p.RetainDays < 1 {
		return fmt.Errorf("los días de retención deben ser al menos 1")
	}
	p.UpdatedAt = time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = p.UpdatedAt
	}
	return uc.repo.Save(ctx, p)
}

func (uc *RetentionUseCase) FindAll(ctx context.Context) ([]*entities.RetentionPolicy, error) {
	return uc.repo.FindAll(ctx)
}

func (uc *RetentionUseCase) FindGlobal(ctx context.Context) (*entities.RetentionPolicy, error) {
	pol, err := uc.repo.FindGlobal(ctx)
	if err != nil {
		// Return default if not configured
		def := entities.DefaultRetentionPolicy()
		return &def, nil
	}
	return pol, nil
}
