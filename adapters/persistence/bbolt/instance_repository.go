package bbolt

import (
	"context"
	"fmt"

	"BackiieTUI/domain/entities"
	bolt "go.etcd.io/bbolt"
)

type InstanceRepository struct {
	db *bolt.DB
}

func (r *InstanceRepository) Save(_ context.Context, inst *entities.Instance) error {
	data, err := encode(inst)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInstances).Put([]byte(inst.ID), data)
	})
}

func (r *InstanceRepository) FindByID(_ context.Context, id string) (*entities.Instance, error) {
	var inst entities.Instance
	err := r.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketInstances).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("instancia %q no encontrada", id)
		}
		return decode(data, &inst)
	})
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (r *InstanceRepository) FindAll(_ context.Context) ([]*entities.Instance, error) {
	var result []*entities.Instance
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInstances).ForEach(func(k, v []byte) error {
			var inst entities.Instance
			if err := decode(v, &inst); err != nil {
				return err
			}
			result = append(result, &inst)
			return nil
		})
	})
	return result, err
}

func (r *InstanceRepository) FindByEngine(_ context.Context, engine entities.EngineType) ([]*entities.Instance, error) {
	var result []*entities.Instance
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInstances).ForEach(func(k, v []byte) error {
			var inst entities.Instance
			if err := decode(v, &inst); err != nil {
				return err
			}
			if inst.Engine == engine {
				result = append(result, &inst)
			}
			return nil
		})
	})
	return result, err
}

func (r *InstanceRepository) Update(_ context.Context, inst *entities.Instance) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucketInstances)
		if bkt.Get([]byte(inst.ID)) == nil {
			return fmt.Errorf("instancia %q no encontrada", inst.ID)
		}
		data, err := encode(inst)
		if err != nil {
			return err
		}
		return bkt.Put([]byte(inst.ID), data)
	})
}

func (r *InstanceRepository) Delete(_ context.Context, id string) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketInstances).Delete([]byte(id))
	})
}
