package entities

import "time"

// MinVerifiedFullBackups is the golden rule for SQL Server retention: a full backup is never
// deleted if doing so would leave fewer than this many verified fulls in the bucket for that
// database. Not configurable on purpose.
const MinVerifiedFullBackups = 2

// RetentionPolicy defines how long full backups are kept for an instance (or globally).
// For SQL Server, transaction logs are not retained independently — they live and die with the
// full backup they chain from (see BackupRecord.ParentBackupID).
type RetentionPolicy struct {
	ID         string    `json:"id"`
	InstanceID string    `json:"instance_id"` // empty = global default
	RetainDays int       `json:"retain_days"` // días de Full a conservar (default 3)
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SchedulerConfig defines when automatic backups run.
type SchedulerConfig struct {
	Enabled       bool     `json:"enabled"`
	CronExprsFull []string `json:"cron_exprs_full"` // respaldo Full, default una vez al día
	CronExprsLog  []string `json:"cron_exprs_log"`  // respaldo Log (solo SQL Server), default cada hora
	TimeZone      string   `json:"timezone"`        // e.g. "America/Bogota"
}

func DefaultSchedulerConfig() SchedulerConfig {
	// 6 campos (sec min hour dom month dow) — el scheduler corre con cron.WithSeconds().
	return SchedulerConfig{
		Enabled:       true,
		CronExprsFull: []string{"0 0 0 * * *"}, // Full: una vez al día, medianoche
		CronExprsLog:  []string{"0 0 * * * *"}, // Log: cada hora, en punto (solo SQL Server)
		TimeZone:      "UTC",
	}
}

func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		RetainDays: 3,
	}
}
