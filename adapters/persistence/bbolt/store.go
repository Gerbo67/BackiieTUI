package bbolt

import (
	"fmt"
	"io"

	bolt "go.etcd.io/bbolt"
)

// Store wraps a BBolt database and provides access to all repositories.
type Store struct {
	db *bolt.DB
}

// Open opens (or creates) the BBolt database at the given path.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir bbolt en %q: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("crear bucket %q: %w", name, err)
			}
		}
		return nil
	})
}

// Close closes the underlying database file.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the raw BBolt instance (for repository constructors).
func (s *Store) DB() *bolt.DB {
	return s.db
}

// Instances returns a ready-to-use InstanceRepository.
func (s *Store) Instances() *InstanceRepository {
	return &InstanceRepository{db: s.db}
}

// BackupRecords returns a ready-to-use BackupRecordRepository.
func (s *Store) BackupRecords() *BackupRecordRepository {
	return &BackupRecordRepository{db: s.db}
}

// S3Config returns a ready-to-use S3ConfigRepository.
func (s *Store) S3Config() *S3ConfigRepository {
	return &S3ConfigRepository{db: s.db}
}

// Retention returns a ready-to-use RetentionRepository.
func (s *Store) Retention() *RetentionRepository {
	return &RetentionRepository{db: s.db}
}

// SchedulerConfig returns a ready-to-use SchedulerConfigRepository.
func (s *Store) SchedulerConfig() *SchedulerConfigRepository {
	return &SchedulerConfigRepository{db: s.db}
}

// RestoreRecords returns a ready-to-use RestoreRecordRepository.
func (s *Store) RestoreRecords() *RestoreRecordRepository {
	return &RestoreRecordRepository{db: s.db}
}

// Backup streams a hot (online, non-blocking) snapshot of the whole database to w, using
// BBolt's native read-only transaction copy. Implements ports.DBBackupSource.
func (s *Store) Backup(w io.Writer) error {
	return s.db.View(func(tx *bolt.Tx) error {
		_, err := tx.WriteTo(w)
		return err
	})
}
