package ports

import (
	"context"

	"BackiieTUI/domain/entities"
)

// InstanceRepository persists database instance configurations.
type InstanceRepository interface {
	Save(ctx context.Context, inst *entities.Instance) error
	FindByID(ctx context.Context, id string) (*entities.Instance, error)
	FindAll(ctx context.Context) ([]*entities.Instance, error)
	FindByEngine(ctx context.Context, engine entities.EngineType) ([]*entities.Instance, error)
	Update(ctx context.Context, inst *entities.Instance) error
	Delete(ctx context.Context, id string) error
}

// BackupRecordRepository persists backup audit records in BBolt.
type BackupRecordRepository interface {
	Save(ctx context.Context, r *entities.BackupRecord) error
	FindByID(ctx context.Context, id string) (*entities.BackupRecord, error)
	FindAll(ctx context.Context) ([]*entities.BackupRecord, error)
	FindByInstance(ctx context.Context, instanceID string) ([]*entities.BackupRecord, error)
	FindExpired(ctx context.Context) ([]*entities.BackupRecord, error)
	UpdateStatus(ctx context.Context, id string, status entities.BackupStatus, errMsg string) error
	Update(ctx context.Context, r *entities.BackupRecord) error
	Delete(ctx context.Context, id string) error
}

// S3ConfigRepository persists the single S3 configuration.
type S3ConfigRepository interface {
	Save(ctx context.Context, cfg *entities.S3Config) error
	Get(ctx context.Context) (*entities.S3Config, error)
}

// RetentionRepository persists retention policies.
type RetentionRepository interface {
	Save(ctx context.Context, p *entities.RetentionPolicy) error
	FindByInstance(ctx context.Context, instanceID string) (*entities.RetentionPolicy, error)
	FindGlobal(ctx context.Context) (*entities.RetentionPolicy, error)
	FindAll(ctx context.Context) ([]*entities.RetentionPolicy, error)
	Update(ctx context.Context, p *entities.RetentionPolicy) error
	Delete(ctx context.Context, id string) error
}

// SchedulerConfigRepository persists the scheduler configuration.
type SchedulerConfigRepository interface {
	Save(ctx context.Context, cfg *entities.SchedulerConfig) error
	Get(ctx context.Context) (*entities.SchedulerConfig, error)
}

// RestoreRecordRepository persists restore audit records in BBolt.
type RestoreRecordRepository interface {
	Save(ctx context.Context, r *entities.RestoreRecord) error
	FindByID(ctx context.Context, id string) (*entities.RestoreRecord, error)
	FindAll(ctx context.Context) ([]*entities.RestoreRecord, error)
	Update(ctx context.Context, r *entities.RestoreRecord) error
}
