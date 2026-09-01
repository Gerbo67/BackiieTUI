package usecases

import (
	"context"
	"fmt"

	s3adapter "BackiieTUI/adapters/storage/s3"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// S3ConfigUseCase manages S3 configuration.
type S3ConfigUseCase struct {
	repo ports.S3ConfigRepository
}

func NewS3ConfigUseCase(repo ports.S3ConfigRepository) *S3ConfigUseCase {
	return &S3ConfigUseCase{repo: repo}
}

func (uc *S3ConfigUseCase) Save(ctx context.Context, cfg *entities.S3Config) error {
	if cfg.Bucket == "" {
		return fmt.Errorf("el bucket es requerido")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return fmt.Errorf("las credenciales S3 son requeridas")
	}
	return uc.repo.Save(ctx, cfg)
}

func (uc *S3ConfigUseCase) Get(ctx context.Context) (*entities.S3Config, error) {
	return uc.repo.Get(ctx)
}

// TestConnection verifies S3 connectivity by listing objects with a brief prefix.
func (uc *S3ConfigUseCase) TestConnection(ctx context.Context, cfg *entities.S3Config) error {
	adapter, err := s3adapter.New(cfg)
	if err != nil {
		return err
	}
	_, err = adapter.ListObjects(ctx, "")
	return err
}
