package apperrors

import "fmt"

// ConnectionError wraps a database connection failure.
type ConnectionError struct {
	Engine string
	Host   string
	Cause  error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("no se pudo conectar a %s en %s: %v", e.Engine, e.Host, e.Cause)
}
func (e *ConnectionError) Unwrap() error { return e.Cause }

// BackupError wraps a backup operation failure.
type BackupError struct {
	Database string
	Cause    error
}

func (e *BackupError) Error() string {
	return fmt.Sprintf("error al respaldar base de datos %q: %v", e.Database, e.Cause)
}
func (e *BackupError) Unwrap() error { return e.Cause }

// StorageError wraps an S3 operation failure.
type StorageError struct {
	Operation string
	Key       string
	Cause     error
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("error de almacenamiento (%s) en %q: %v", e.Operation, e.Key, e.Cause)
}
func (e *StorageError) Unwrap() error { return e.Cause }

// NotFoundError indicates a resource was not found in persistence.
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s con id %q no encontrado", e.Resource, e.ID)
}

// ValidationError indicates invalid input data.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validación fallida en %q: %s", e.Field, e.Message)
}

var (
	ErrInstanceNotFound     = &NotFoundError{Resource: "instancia"}
	ErrBackupRecordNotFound = &NotFoundError{Resource: "registro de respaldo"}
	ErrS3ConfigNotFound     = &NotFoundError{Resource: "configuración S3"}
)
