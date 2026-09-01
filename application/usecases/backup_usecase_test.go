package usecases

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// ---- fakes ----

type fakeBackupRepo struct {
	records map[string]*entities.BackupRecord
}

func newFakeBackupRepo() *fakeBackupRepo {
	return &fakeBackupRepo{records: map[string]*entities.BackupRecord{}}
}

func (f *fakeBackupRepo) Save(_ context.Context, r *entities.BackupRecord) error {
	f.records[r.ID] = r
	return nil
}
func (f *fakeBackupRepo) FindByID(_ context.Context, id string) (*entities.BackupRecord, error) {
	r, ok := f.records[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return r, nil
}
func (f *fakeBackupRepo) FindAll(_ context.Context) ([]*entities.BackupRecord, error) {
	var out []*entities.BackupRecord
	for _, r := range f.records {
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeBackupRepo) FindByInstance(_ context.Context, instanceID string) ([]*entities.BackupRecord, error) {
	var out []*entities.BackupRecord
	for _, r := range f.records {
		if r.InstanceID == instanceID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeBackupRepo) FindExpired(_ context.Context) ([]*entities.BackupRecord, error) {
	return nil, nil
}
func (f *fakeBackupRepo) UpdateStatus(_ context.Context, id string, status entities.BackupStatus, errMsg string) error {
	r, ok := f.records[id]
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	r.Status = status
	r.ErrorMessage = errMsg
	return nil
}
func (f *fakeBackupRepo) Update(_ context.Context, r *entities.BackupRecord) error {
	f.records[r.ID] = r
	return nil
}
func (f *fakeBackupRepo) Delete(_ context.Context, id string) error {
	delete(f.records, id)
	return nil
}

type fakeStorage struct {
	existing map[string]bool
	deleted  map[string]bool
}

func newFakeStorage(keys ...string) *fakeStorage {
	s := &fakeStorage{existing: map[string]bool{}, deleted: map[string]bool{}}
	for _, k := range keys {
		s.existing[k] = true
	}
	return s
}
func (s *fakeStorage) Upload(_ context.Context, key string, _ io.Reader) (int64, error) {
	s.existing[key] = true
	return 0, nil
}
func (s *fakeStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}
func (s *fakeStorage) ListObjects(_ context.Context, prefix string) ([]ports.StorageObject, error) {
	var out []ports.StorageObject
	for k := range s.existing {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, ports.StorageObject{Key: k, LastModified: time.Now()})
		}
	}
	return out, nil
}
func (s *fakeStorage) DeleteObject(_ context.Context, key string) error {
	delete(s.existing, key)
	s.deleted[key] = true
	return nil
}
func (s *fakeStorage) TagExpiry(_ context.Context, key string, _ time.Time) error { return nil }

type fakeS3Factory struct{ storage *fakeStorage }

func (f fakeS3Factory) NewStorage(_ *entities.S3Config) (ports.StorageAdapter, error) {
	return f.storage, nil
}
func (f fakeS3Factory) NewLifecycle(_ *entities.S3Config) (ports.LifecycleManager, error) {
	return nil, fmt.Errorf("not implemented")
}

type fakeS3ConfigRepo struct{}

func (fakeS3ConfigRepo) Save(_ context.Context, _ *entities.S3Config) error { return nil }
func (fakeS3ConfigRepo) Get(_ context.Context) (*entities.S3Config, error) {
	return &entities.S3Config{Bucket: "test-bucket", Endpoint: "minio.local:9000"}, nil
}

type fakeRetentionRepo struct{ retainDays int }

func (f fakeRetentionRepo) Save(_ context.Context, _ *entities.RetentionPolicy) error { return nil }
func (f fakeRetentionRepo) FindByInstance(_ context.Context, _ string) (*entities.RetentionPolicy, error) {
	return nil, fmt.Errorf("none")
}
func (f fakeRetentionRepo) FindGlobal(_ context.Context) (*entities.RetentionPolicy, error) {
	return &entities.RetentionPolicy{RetainDays: f.retainDays}, nil
}
func (f fakeRetentionRepo) FindAll(_ context.Context) ([]*entities.RetentionPolicy, error) {
	return nil, nil
}
func (f fakeRetentionRepo) Update(_ context.Context, _ *entities.RetentionPolicy) error { return nil }
func (f fakeRetentionRepo) Delete(_ context.Context, _ string) error                    { return nil }

// ---- helpers ----

func mkFull(id string, daysAgo int, key string) *entities.BackupRecord {
	return &entities.BackupRecord{
		ID:           id,
		InstanceID:   "inst-1",
		DatabaseName: "ventas",
		Engine:       entities.EngineSQLServer,
		Type:         entities.BackupTypeFull,
		Status:       entities.StatusCompleted,
		FileName:     key,
		StartedAt:    time.Now().UTC().AddDate(0, 0, -daysAgo),
	}
}

func mkLog(id, parentID string, daysAgo int, key string) *entities.BackupRecord {
	r := mkFull(id, daysAgo, key)
	r.Type = entities.BackupTypeLog
	r.ParentBackupID = parentID
	return r
}

// ---- tests ----

// La regla de oro: con 4 Fulls viejos, sólo se borran los suficientes para dejar
// exactamente entities.MinVerifiedFullBackups (2) verificados en S3.
func TestApplySQLServerRetention_KeepsMinimumTwoVerified(t *testing.T) {
	repo := newFakeBackupRepo()
	c1 := mkFull("c1", 10, "k1")
	c2 := mkFull("c2", 8, "k2")
	c3 := mkFull("c3", 6, "k3")
	c4 := mkFull("c4", 4, "k4")
	for _, r := range []*entities.BackupRecord{c1, c2, c3, c4} {
		_ = repo.Save(context.Background(), r)
	}
	storage := newFakeStorage("k1", "k2", "k3", "k4")
	uc := NewBackupQueryUseCase(repo, fakeS3Factory{storage: storage}, fakeS3ConfigRepo{}, fakeRetentionRepo{retainDays: 3})

	n, err := uc.ApplySQLServerRetention(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deletions, got %d", n)
	}
	if repo.records["c1"].Status != entities.StatusExpired || repo.records["c2"].Status != entities.StatusExpired {
		t.Fatalf("expected c1 and c2 to be expired")
	}
	if repo.records["c3"].Status != entities.StatusCompleted || repo.records["c4"].Status != entities.StatusCompleted {
		t.Fatalf("expected c3 and c4 to remain completed (golden rule: min 2 verified)")
	}
}

// Si uno de los backups que quedarían no está realmente en S3 (no verificado), el barrido
// se detiene sin borrar nada — nunca se confía en un backup "fantasma".
func TestApplySQLServerRetention_StopsWhenRemainingNotVerified(t *testing.T) {
	repo := newFakeBackupRepo()
	c1 := mkFull("c1", 10, "k1")
	c2 := mkFull("c2", 8, "k2") // este objeto no existirá en S3
	c3 := mkFull("c3", 1, "k3")
	for _, r := range []*entities.BackupRecord{c1, c2, c3} {
		_ = repo.Save(context.Background(), r)
	}
	storage := newFakeStorage("k1", "k3") // k2 falta -> c2 no verificado
	uc := NewBackupQueryUseCase(repo, fakeS3Factory{storage: storage}, fakeS3ConfigRepo{}, fakeRetentionRepo{retainDays: 3})

	n, err := uc.ApplySQLServerRetention(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deletions when a remaining full isn't verified, got %d", n)
	}
}

// Borrar un Full en cascada borra también sus Logs encadenados (ParentBackupID).
func TestDeleteBackup_CascadesToChildLogs(t *testing.T) {
	repo := newFakeBackupRepo()
	full := mkFull("full-1", 5, "full-key")
	log1 := mkLog("log-1", "full-1", 4, "log-key-1")
	log2 := mkLog("log-2", "full-1", 3, "log-key-2")
	otherFull := mkFull("full-2", 1, "other-key") // no debe verse afectado
	for _, r := range []*entities.BackupRecord{full, log1, log2, otherFull} {
		_ = repo.Save(context.Background(), r)
	}
	storage := newFakeStorage("full-key", "log-key-1", "log-key-2", "other-key")
	uc := NewBackupQueryUseCase(repo, fakeS3Factory{storage: storage}, fakeS3ConfigRepo{}, fakeRetentionRepo{retainDays: 3})

	if err := uc.DeleteBackup(context.Background(), "full-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.records["full-1"].Status != entities.StatusExpired ||
		repo.records["log-1"].Status != entities.StatusExpired ||
		repo.records["log-2"].Status != entities.StatusExpired {
		t.Fatalf("expected full and both logs to be expired")
	}
	if repo.records["full-2"].Status != entities.StatusCompleted {
		t.Fatalf("unrelated full should be untouched")
	}
	if storage.existing["full-key"] || storage.existing["log-key-1"] || storage.existing["log-key-2"] {
		t.Fatalf("expected S3 objects to be deleted")
	}
	if !storage.existing["other-key"] {
		t.Fatalf("unrelated S3 object should remain")
	}
}
