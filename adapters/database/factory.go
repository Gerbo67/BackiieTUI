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
		s3cfg := &sqlserver.S3BackupConfig{}
		if s3 != nil {
			s3cfg.Endpoint = s3.Endpoint
			s3cfg.Bucket = s3.Bucket
			s3cfg.AccessKeyID = s3.AccessKeyID
			s3cfg.SecretAccessKey = s3.SecretAccessKey
			s3cfg.PathPrefix = s3.PathPrefix
		}
		return sqlserver.New(inst, s3cfg)
	case entities.EngineMySQL, entities.EngineMariaDB:
		return mysql.New(inst)
	case entities.EnginePostgres:
		return postgres.New(inst)
	case entities.EngineRedis:
		return redis.New(inst)
	}
	return nil, fmt.Errorf("motor no soportado: %q", inst.Engine)
}
