package bbolt

import (
	"context"
	"fmt"

	"BackiieTUI/domain/entities"
	bolt "go.etcd.io/bbolt"
)

var keyS3Config = []byte("default")

type S3ConfigRepository struct {
	db *bolt.DB
}

func (r *S3ConfigRepository) Save(_ context.Context, cfg *entities.S3Config) error {
	data, err := encode(cfg)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketS3Config).Put(keyS3Config, data)
	})
}

func (r *S3ConfigRepository) Get(_ context.Context) (*entities.S3Config, error) {
	var cfg entities.S3Config
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketS3Config).Get(keyS3Config)
		if data == nil {
			return fmt.Errorf("configuración S3 no encontrada")
		}
		return decode(data, &cfg)
	})
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
