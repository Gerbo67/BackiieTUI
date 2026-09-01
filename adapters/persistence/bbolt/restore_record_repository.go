package bbolt

import (
	"context"
	"fmt"

	"BackiieTUI/domain/entities"
	bolt "go.etcd.io/bbolt"
)

type RestoreRecordRepository struct {
	db *bolt.DB
}

func (r *RestoreRecordRepository) Save(_ context.Context, rec *entities.RestoreRecord) error {
	data, err := encode(rec)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRestoreRecords).Put([]byte(rec.ID), data)
	})
}

func (r *RestoreRecordRepository) FindByID(_ context.Context, id string) (*entities.RestoreRecord, error) {
	var rec entities.RestoreRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketRestoreRecords).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("registro de restauración %q no encontrado", id)
		}
		return decode(data, &rec)
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *RestoreRecordRepository) FindAll(_ context.Context) ([]*entities.RestoreRecord, error) {
	var result []*entities.RestoreRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRestoreRecords).ForEach(func(k, v []byte) error {
			var rec entities.RestoreRecord
			if err := decode(v, &rec); err != nil {
				return err
			}
			result = append(result, &rec)
			return nil
		})
	})
	return result, err
}

func (r *RestoreRecordRepository) Update(_ context.Context, rec *entities.RestoreRecord) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		data, err := encode(rec)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketRestoreRecords).Put([]byte(rec.ID), data)
	})
}
