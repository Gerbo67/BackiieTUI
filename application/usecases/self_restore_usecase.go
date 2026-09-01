package usecases

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

type SelfRestoreUseCase struct {
	storageFactory ports.S3Factory
	logger         *slog.Logger
	localDBPath    string
}

func NewSelfRestoreUseCase(
	storageFactory ports.S3Factory,
	localDBPath string,
	logger *slog.Logger,
) *SelfRestoreUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &SelfRestoreUseCase{
		storageFactory: storageFactory,
		localDBPath:    localDBPath,
		logger:         logger,
	}
}

// Run list all backup files for the given hostname, downloads the latest one, and overwrites the local DB.
func (uc *SelfRestoreUseCase) Run(ctx context.Context, s3cfg *entities.S3Config, hostname string) error {
	if hostname == "" {
		host := os.Getenv("BACKIIE_HOSTNAME")
		if host == "" {
			h, err := os.Hostname()
			if err != nil {
				host = "unknown"
			} else {
				host = h
			}
		}
		hostname = host
	}

	storage, err := uc.storageFactory.NewStorage(s3cfg)
	if err != nil {
		return fmt.Errorf("conectar S3: %w", err)
	}

	prefix := fmt.Sprintf("%s/%s", selfBackupPrefix, hostname)
	objs, err := storage.ListObjects(ctx, prefix)
	if err != nil {
		return fmt.Errorf("listar self-backups: %w", err)
	}

	var dbs []ports.StorageObject
	for _, o := range objs {
		if strings.HasSuffix(o.Key, ".db") {
			dbs = append(dbs, o)
		}
	}

	if len(dbs) == 0 {
		return fmt.Errorf("no se encontraron respaldos de BackiieTUI para el host %q", hostname)
	}

	// Sort by LastModified descending
	sort.Slice(dbs, func(i, j int) bool {
		return dbs[i].LastModified.After(dbs[j].LastModified)
	})

	latest := dbs[0]
	uc.logger.Info("descargando self-backup", "key", latest.Key, "fecha", latest.LastModified)

	reader, err := storage.Download(ctx, latest.Key)
	if err != nil {
		return fmt.Errorf("descargar %s: %w", latest.Key, err)
	}
	defer reader.Close()

	// Create a temporary file to avoid corrupting the DB if download fails mid-way
	tmpPath := uc.localDBPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("crear archivo temporal: %w", err)
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("escribir archivo temporal: %w", err)
	}
	tmpFile.Close()

	// Replace the old DB with the new one
	if err := os.Rename(tmpPath, uc.localDBPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("reemplazar base de datos local: %w", err)
	}

	uc.logger.Info("restauración de BackiieTUI completada con éxito")
	return nil
}
