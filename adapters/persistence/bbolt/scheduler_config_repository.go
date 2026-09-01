package bbolt

import (
	"context"
	"fmt"

	"BackiieTUI/domain/entities"
	bolt "go.etcd.io/bbolt"
)

var keySchedulerConfig = []byte("default")

type SchedulerConfigRepository struct {
	db *bolt.DB
}

func (r *SchedulerConfigRepository) Save(_ context.Context, cfg *entities.SchedulerConfig) error {
	data, err := encode(cfg)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSchedulerConfig).Put(keySchedulerConfig, data)
	})
}

func (r *SchedulerConfigRepository) Get(_ context.Context) (*entities.SchedulerConfig, error) {
	var cfg entities.SchedulerConfig
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketSchedulerConfig).Get(keySchedulerConfig)
		if data == nil {
			return fmt.Errorf("configuración del scheduler no encontrada")
		}
		return decode(data, &cfg)
	})
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
