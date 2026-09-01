package entities

type BackupStatus string

const (
	StatusPending   BackupStatus = "pending"
	StatusRunning   BackupStatus = "running"
	StatusCompleted BackupStatus = "completed"
	StatusFailed    BackupStatus = "failed"
	StatusExpired   BackupStatus = "expired"
)

func (s BackupStatus) String() string {
	switch s {
	case StatusPending:
		return "Pendiente"
	case StatusRunning:
		return "Ejecutando"
	case StatusCompleted:
		return "Completado"
	case StatusFailed:
		return "Fallido"
	case StatusExpired:
		return "Expirado"
	}
	return string(s)
}
