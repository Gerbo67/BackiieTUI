package usecases

import (
	"context"
	"fmt"
	"time"

	dbfactory "BackiieTUI/adapters/database"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"BackiieTUI/internal/idgen"
)

// InstanceUseCase groups all instance management operations.
type InstanceUseCase struct {
	repo      ports.InstanceRepository
	s3cfgRepo ports.S3ConfigRepository
}

func NewInstanceUseCase(repo ports.InstanceRepository, s3cfgRepo ports.S3ConfigRepository) *InstanceUseCase {
	return &InstanceUseCase{repo: repo, s3cfgRepo: s3cfgRepo}
}

func (uc *InstanceUseCase) Create(ctx context.Context, inst *entities.Instance) error {
	if err := validateInstance(inst); err != nil {
		return err
	}
	inst.ID = idgen.New()
	inst.CreatedAt = time.Now().UTC()
	inst.UpdatedAt = inst.CreatedAt
	if inst.Port == 0 {
		inst.Port = entities.DefaultPort(inst.Engine)
	}
	return uc.repo.Save(ctx, inst)
}

func (uc *InstanceUseCase) Update(ctx context.Context, inst *entities.Instance) error {
	if err := validateInstance(inst); err != nil {
		return err
	}
	inst.UpdatedAt = time.Now().UTC()
	return uc.repo.Update(ctx, inst)
}

func (uc *InstanceUseCase) Delete(ctx context.Context, id string) error {
	return uc.repo.Delete(ctx, id)
}

func (uc *InstanceUseCase) FindAll(ctx context.Context) ([]*entities.Instance, error) {
	return uc.repo.FindAll(ctx)
}

func (uc *InstanceUseCase) FindByID(ctx context.Context, id string) (*entities.Instance, error) {
	return uc.repo.FindByID(ctx, id)
}

// TestConnection attempts to open and ping the instance.
func (uc *InstanceUseCase) TestConnection(ctx context.Context, inst *entities.Instance) (time.Duration, error) {
	var s3Cfg *dbfactory.S3Config
	if s3cfg, err := uc.s3cfgRepo.Get(ctx); err == nil {
		s3Cfg = &dbfactory.S3Config{
			Endpoint:        s3cfg.Endpoint,
			Bucket:          s3cfg.Bucket,
			AccessKeyID:     s3cfg.AccessKeyID,
			SecretAccessKey: s3cfg.SecretAccessKey,
			PathPrefix:      s3cfg.PathPrefix,
		}
	}

	adapter, err := dbfactory.NewAdapter(inst, s3Cfg)
	if err != nil {
		return 0, err
	}
	defer adapter.Close()

	start := time.Now()
	if err := adapter.Ping(ctx); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func validateInstance(inst *entities.Instance) error {
	if inst.Name == "" {
		return fmt.Errorf("el nombre de la instancia es requerido")
	}
	if !inst.Engine.IsValid() {
		return fmt.Errorf("motor inválido: %q", inst.Engine)
	}
	if inst.Host == "" {
		return fmt.Errorf("el host es requerido")
	}
	return nil
}
