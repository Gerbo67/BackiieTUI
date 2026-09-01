package entities

import "time"

// Instance represents a database server connection.
type Instance struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Engine            EngineType        `json:"engine"`
	Host              string            `json:"host"`
	Port              int               `json:"port"`
	Username          string            `json:"username"`
	Password          string            `json:"password"`
	Database          string            `json:"database"` // default DB; for SQL Server: instance name suffix
	Extra             map[string]string `json:"extra,omitempty"`
	Enabled           bool              `json:"enabled"`
	ExcludedDatabases []string          `json:"excluded_databases,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// DefaultPort returns the conventional port for the engine.
func DefaultPort(e EngineType) int {
	switch e {
	case EngineSQLServer:
		return 1433
	case EngineMySQL, EngineMariaDB:
		return 3306
	case EnginePostgres:
		return 5432
	case EngineRedis:
		return 6379
	}
	return 0
}
