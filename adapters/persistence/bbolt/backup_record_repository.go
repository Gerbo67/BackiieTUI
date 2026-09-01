package bbolt

import (
	"context"
	"fmt"
	"time"

	"BackiieTUI/domain/entities"
	bolt "go.etcd.io/bbolt"
)

type BackupRecordRepository struct {
	db *bolt.DB
}

func (r *BackupRecordRepository) Save(_ context.Context, rec *entities.BackupRecord) error {
	data, err := encode(rec)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBackupRecords).Put([]byte(rec.ID), data)
	})
}

func (r *BackupRecordRepository) FindByID(_ context.Context, id string) (*entities.BackupRecord, error) {
	var rec entities.BackupRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketBackupRecords).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("registro de respaldo %q no encontrado", id)
		}
		return decode(data, &rec)
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *BackupRecordRepository) FindAll(_ context.Context) ([]*entities.BackupRecord, error) {
	var result []*entities.BackupRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBackupRecords).ForEach(func(k, v []byte) error {
			var rec entities.BackupRecord
			if err := decode(v, &rec); err != nil {
				return err
			}
			result = append(result, &rec)
			return nil
		})
	})
	return result, err
}

func (r *BackupRecordRepository) FindByInstance(_ context.Context, instanceID string) ([]*entities.BackupRecord, error) {
	var result []*entities.BackupRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBackupRecords).ForEach(func(k, v []byte) error {
			var rec entities.BackupRecord
			if err := decode(v, &rec); err != nil {
				return err
			}
			if rec.InstanceID == instanceID {
				result = append(result, &rec)
			}
			return nil
		})
	})
	return result, err
}

func (r *BackupRecordRepository) FindExpired(_ context.Context) ([]*entities.BackupRecord, error) {
	now := time.Now()
	var result []*entities.BackupRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBackupRecords).ForEach(func(k, v []byte) error {
			var rec entities.BackupRecord
			if err := decode(v, &rec); err != nil {
				return err
			}
			if !rec.ExpiresAt.IsZero() && rec.ExpiresAt.Before(now) &&
				rec.Status != entities.StatusExpired {
				result = append(result, &rec)
			}
			return nil
		})
	})
	return result, err
}

func (r *BackupRecordRepository) UpdateStatus(_ context.Context, id string, status entities.BackupStatus, errMsg string) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketBackupRecords)
		data := bkt.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("registro de respaldo %q no encontrado", id)
		}
		var rec entities.BackupRecord
		if err := decode(data, &rec); err != nil {
			return err
		}
		rec.Status = status
		rec.ErrorMessage = errMsg
		if status == entities.StatusCompleted || status == entities.StatusFailed {
			now := time.Now()
			rec.CompletedAt = &now
			rec.DurationMs = now.Sub(rec.StartedAt).Milliseconds()
		}
		encoded, err := encode(rec)
		if err != nil {
			return err
		}
		return bkt.Put([]byte(id), encoded)
	})
}

func (r *BackupRecordRepository) Update(_ context.Context, rec *entities.BackupRecord) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		data, err := encode(rec)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketBackupRecords).Put([]byte(rec.ID), data)
	})
}

func (r *BackupRecordRepository) Delete(_ context.Context, id string) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBackupRecords).Delete([]byte(id))
	})
}
