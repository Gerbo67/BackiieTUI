package entities

import "fmt"

type EngineType string

const (
	EngineSQLServer EngineType = "sqlserver"
	EngineMySQL     EngineType = "mysql"
	EngineMariaDB   EngineType = "mariadb"
	EnginePostgres  EngineType = "postgres"
	EngineRedis     EngineType = "redis"
)

func (e EngineType) String() string {
	switch e {
	case EngineSQLServer:
		return "SQL Server"
	case EngineMySQL:
		return "MySQL"
	case EngineMariaDB:
		return "MariaDB"
	case EnginePostgres:
		return "PostgreSQL"
	case EngineRedis:
		return "Redis"
	default:
		return string(e)
	}
}

func (e EngineType) IsValid() bool {
	switch e {
	case EngineSQLServer, EngineMySQL, EngineMariaDB, EnginePostgres, EngineRedis:
		return true
	}
	return false
}

func ParseEngine(s string) (EngineType, error) {
	e := EngineType(s)
	if !e.IsValid() {
		return "", fmt.Errorf("unknown engine type: %q", s)
	}
	return e, nil
}

func AllEngines() []EngineType {
	return []EngineType{
		EngineSQLServer,
		EngineMySQL,
		EngineMariaDB,
		EnginePostgres,
		EngineRedis,
	}
}
