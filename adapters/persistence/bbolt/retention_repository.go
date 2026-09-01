package bbolt

import (
	"context"
	"fmt"

	"BackiieTUI/domain/entities"
	bolt "go.etcd.io/bbolt"
)

var keyGlobalRetention = []byte("__global__")

type RetentionRepository struct {
	db *bolt.DB
}

func (r *RetentionRepository) Save(_ context.Context, p *entities.RetentionPolicy) error {
	data, err := encode(p)
	if err != nil {
		return err
	}
	key := []byte(p.InstanceID)
	if p.InstanceID == "" {
		key = keyGlobalRetention
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRetention).Put(key, data)
	})
}

func (r *RetentionRepository) FindByInstance(_ context.Context, instanceID string) (*entities.RetentionPolicy, error) {
	var p entities.RetentionPolicy
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketRetention).Get([]byte(instanceID))
		if data == nil {
			return fmt.Errorf("política de retención no encontrada para instancia %q", instanceID)
		}
		return decode(data, &p)
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *RetentionRepository) FindGlobal(_ context.Context) (*entities.RetentionPolicy, error) {
	var p entities.RetentionPolicy
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketRetention).Get(keyGlobalRetention)
		if data == nil {
			return fmt.Errorf("política de retención global no encontrada")
		}
		return decode(data, &p)
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *RetentionRepository) FindAll(_ context.Context) ([]*entities.RetentionPolicy, error) {
	var result []*entities.RetentionPolicy
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRetention).ForEach(func(k, v []byte) error {
			var p entities.RetentionPolicy
			if err := decode(v, &p); err != nil {
				return err
			}
			result = append(result, &p)
			return nil
		})
	})
	return result, err
}

func (r *RetentionRepository) Update(_ context.Context, p *entities.RetentionPolicy) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		data, err := encode(p)
		if err != nil {
			return err
		}
		key := []byte(p.InstanceID)
		if p.InstanceID == "" {
			key = keyGlobalRetention
		}
		return tx.Bucket(bucketRetention).Put(key, data)
	})
}

func (r *RetentionRepository) Delete(_ context.Context, id string) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRetention).Delete([]byte(id))
	})
}
