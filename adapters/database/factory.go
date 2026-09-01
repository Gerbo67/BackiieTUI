package database

import (
	"fmt"

	"BackiieTUI/adapters/database/mysql"
	"BackiieTUI/adapters/database/postgres"
	"BackiieTUI/adapters/database/redis"
	"BackiieTUI/adapters/database/sqlserver"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// S3Config holds the S3 params needed only by SQL Server native backup.
type S3Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PathPrefix      string
}

// NewAdapter creates a DatabaseAdapter for the given instance's engine.
func NewAdapter(inst *entities.Instance, s3 *S3Config) (ports.DatabaseAdapter, error) {
	switch inst.Engine {
	case entities.EngineSQLServer:
		return sqlserver.New(inst)
	case entities.EngineMySQL, entities.EngineMariaDB:
		return mysql.New(inst)
	case entities.EnginePostgres:
		return postgres.New(inst)
	case entities.EngineRedis:
		return redis.New(inst)
	}
	return nil, fmt.Errorf("motor no soportado: %q", inst.Engine)
}
