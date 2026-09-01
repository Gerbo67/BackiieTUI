package entities

import "time"

// BackupType distinguishes a full backup from a transaction log backup.
type BackupType string

const (
	BackupTypeFull BackupType = "full"
	BackupTypeLog  BackupType = "log"
)

func (t BackupType) String() string {
	switch t {
	case BackupTypeLog:
		return "Log"
	default:
		return "Full"
	}
}

// BackupRecord is the audit entry stored in BBolt for every backup operation.
type BackupRecord struct {
	ID           string     `json:"id"`
	InstanceID   string     `json:"instance_id"`
	InstanceName string     `json:"instance_name"`
	DatabaseName string     `json:"database_name"`
	FileName     string     `json:"file_name"` // full S3 key/path
	Engine       EngineType `json:"engine"`
	Type         BackupType `json:"type"` // full (.bak) o log (.trn); vacío = full (compat)
	// ParentBackupID: vacío para un Full. Para un Log, apunta al ID del Full
	// más reciente completado en el momento de crearse — así la cadena
	// Full→Logs (y el borrado en cascada de retención) no depende de fechas.
	ParentBackupID string       `json:"parent_backup_id,omitempty"`
	SizeBytes      int64        `json:"size_bytes"`
	HashSHA256     string       `json:"hash_sha256"` // hex-encoded
	Status         BackupStatus `json:"status"`
	ErrorMessage   string       `json:"error_message,omitempty"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    *time.Time   `json:"completed_at,omitempty"`
	ExpiresAt      time.Time    `json:"expires_at"`
	RetainDays     int          `json:"retain_days"` // política de retención aplicada al crear este respaldo
	DurationMs     int64        `json:"duration_ms"`
}

// IsFull reports whether this record is a full backup (default when Type is empty, for records
// created before this field existed).
func (r *BackupRecord) IsFull() bool {
	return r.Type != BackupTypeLog
}
