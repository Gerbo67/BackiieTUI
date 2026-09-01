package usecases

import (
	"context"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// SchedulerUseCase manages the persisted cron configuration (full backup cadence, log backup
// cadence, timezone).
type SchedulerUseCase struct {
	repo ports.SchedulerConfigRepository
}

func NewSchedulerUseCase(repo ports.SchedulerConfigRepository) *SchedulerUseCase {
	return &SchedulerUseCase{repo: repo}
}

func (uc *SchedulerUseCase) Get(ctx context.Context) (*entities.SchedulerConfig, error) {
	cfg, err := uc.repo.Get(ctx)
	if err != nil {
		def := entities.DefaultSchedulerConfig()
		return &def, nil
	}
	return cfg, nil
}

func (uc *SchedulerUseCase) Save(ctx context.Context, cfg *entities.SchedulerConfig) error {
	return uc.repo.Save(ctx, cfg)
}
