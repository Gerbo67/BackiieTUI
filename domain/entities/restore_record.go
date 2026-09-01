package entities

import "time"

// RestoreRecord is the audit trail of a restore operation, so BackiieTUI can answer
// "a qué se restauró y cuándo" even after the fact.
type RestoreRecord struct {
	ID             string       `json:"id"`
	InstanceID     string       `json:"instance_id"`
	InstanceName   string       `json:"instance_name"`
	DatabaseName   string       `json:"database_name"`
	TargetBackupID string       `json:"target_backup_id"` // backup (full o log) que el usuario pidió restaurar
	ChainBackupIDs []string     `json:"chain_backup_ids"` // full + logs realmente aplicados, en orden
	Status         BackupStatus `json:"status"`
	ErrorMessage   string       `json:"error_message,omitempty"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    *time.Time   `json:"completed_at,omitempty"`
}
