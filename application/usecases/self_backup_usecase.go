package usecases

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"BackiieTUI/domain/ports"
)

// selfBackupPrefix groups BackiieTUI's own state snapshots away from the databases it backs up.
const selfBackupPrefix = "_backiie-meta"

// SelfBackupUseCase uploads a hot snapshot of BackiieTUI's own BBolt database to S3 every hour,
// so its instances/backups/restore-history state can be rebuilt if the server hosting it is
// lost. Retention here is simple age-based cleanup (no golden-rule minimum): this is metadata
// BackiieTUI itself is reconstructible from S3 backup records, not the critical customer data.
type SelfBackupUseCase struct {
	source         ports.DBBackupSource
	storageFactory ports.S3Factory
	s3cfgRepo      ports.S3ConfigRepository
	retentionRepo  ports.RetentionRepository
	hostname       string
	logger         *slog.Logger
}

func NewSelfBackupUseCase(
	source ports.DBBackupSource,
	storageFactory ports.S3Factory,
	s3cfgRepo ports.S3ConfigRepository,
	retentionRepo ports.RetentionRepository,
	logger *slog.Logger,
) *SelfBackupUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	hostname := os.Getenv("BACKIIE_HOSTNAME")
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		} else {
			hostname = h
		}
	}
	return &SelfBackupUseCase{
		source:         source,
		storageFactory: storageFactory,
		s3cfgRepo:      s3cfgRepo,
		retentionRepo:  retentionRepo,
		hostname:       hostname,
		logger:         logger,
	}
}

// Run uploads a fresh snapshot and prunes ones older than the global retention window.
func (uc *SelfBackupUseCase) Run(ctx context.Context) error {
	s3cfg, err := uc.s3cfgRepo.Get(ctx)
	if err != nil {
		return fmt.Errorf("config S3: %w", err)
	}
	storage, err := uc.storageFactory.NewStorage(s3cfg)
	if err != nil {
		return fmt.Errorf("conectar S3: %w", err)
	}

	prefix := fmt.Sprintf("%s/%s", selfBackupPrefix, uc.hostname)
	key := fmt.Sprintf("%s/backiie_%s.db", prefix, time.Now().UTC().Format("20060102_1504"))

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(uc.source.Backup(pw))
	}()

	if _, err := storage.Upload(ctx, key, pr); err != nil {
		return fmt.Errorf("subir self-backup: %w", err)
	}
	uc.logger.Info("self-backup de BackiieTUI subido", "key", key)

	if err := uc.pruneOld(ctx, storage, prefix); err != nil {
		uc.logger.Warn("no se pudo aplicar retención a self-backups", "err", err)
	}
	return nil
}

func (uc *SelfBackupUseCase) pruneOld(ctx context.Context, storage ports.StorageAdapter, prefix string) error {
	retainDays := 3
	if global, err := uc.retentionRepo.FindGlobal(ctx); err == nil {
		retainDays = global.RetainDays
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retainDays)

	objs, err := storage.ListObjects(ctx, prefix)
	if err != nil {
		return err
	}
	for _, o := range objs {
		if !strings.HasSuffix(o.Key, ".db") {
			continue
		}
		if o.LastModified.Before(cutoff) {
			_ = storage.DeleteObject(ctx, o.Key)
		}
	}
	return nil
}
