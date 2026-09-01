package usecases

import (
	"context"
	"fmt"
	"testing"

	"BackiieTUI/domain/entities"
)

type fakeInstanceRepo struct {
	instances map[string]*entities.Instance
}

func newFakeInstanceRepo(instances ...*entities.Instance) *fakeInstanceRepo {
	r := &fakeInstanceRepo{instances: map[string]*entities.Instance{}}
	for _, i := range instances {
		r.instances[i.ID] = i
	}
	return r
}
func (r *fakeInstanceRepo) Save(_ context.Context, i *entities.Instance) error {
	r.instances[i.ID] = i
	return nil
}
func (r *fakeInstanceRepo) FindByID(_ context.Context, id string) (*entities.Instance, error) {
	i, ok := r.instances[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return i, nil
}
func (r *fakeInstanceRepo) FindAll(_ context.Context) ([]*entities.Instance, error) {
	var out []*entities.Instance
	for _, i := range r.instances {
		out = append(out, i)
	}
	return out, nil
}
func (r *fakeInstanceRepo) FindByEngine(_ context.Context, engine entities.EngineType) ([]*entities.Instance, error) {
	var out []*entities.Instance
	for _, i := range r.instances {
		if i.Engine == engine {
			out = append(out, i)
		}
	}
	return out, nil
}
func (r *fakeInstanceRepo) Update(_ context.Context, i *entities.Instance) error {
	r.instances[i.ID] = i
	return nil
}
func (r *fakeInstanceRepo) Delete(_ context.Context, id string) error {
	delete(r.instances, id)
	return nil
}

type fakeRestoreRecordRepo struct {
	records map[string]*entities.RestoreRecord
}

func newFakeRestoreRecordRepo() *fakeRestoreRecordRepo {
	return &fakeRestoreRecordRepo{records: map[string]*entities.RestoreRecord{}}
}
func (r *fakeRestoreRecordRepo) Save(_ context.Context, rec *entities.RestoreRecord) error {
	r.records[rec.ID] = rec
	return nil
}
func (r *fakeRestoreRecordRepo) FindByID(_ context.Context, id string) (*entities.RestoreRecord, error) {
	rec, ok := r.records[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return rec, nil
}
func (r *fakeRestoreRecordRepo) FindAll(_ context.Context) ([]*entities.RestoreRecord, error) {
	var out []*entities.RestoreRecord
	for _, rec := range r.records {
		out = append(out, rec)
	}
	return out, nil
}
func (r *fakeRestoreRecordRepo) Update(_ context.Context, rec *entities.RestoreRecord) error {
	r.records[rec.ID] = rec
	return nil
}

func newTestRestoreUseCase(backupRepo *fakeBackupRepo) *RestoreUseCase {
	return NewRestoreUseCase(
		newFakeInstanceRepo(),
		backupRepo,
		newFakeRestoreRecordRepo(),
		fakeS3Factory{storage: newFakeStorage()},
		fakeS3ConfigRepo{},
	)
}

// buildChain para un Full target debe devolver sólo ese Full.
func TestBuildChain_FullOnly(t *testing.T) {
	repo := newFakeBackupRepo()
	full := mkFull("full-1", 2, "full-key")
	_ = repo.Save(context.Background(), full)

	uc := newTestRestoreUseCase(repo)
	chain, err := uc.buildChain(context.Background(), full)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain) != 1 || chain[0].ID != "full-1" {
		t.Fatalf("expected chain=[full-1], got %+v", chain)
	}
}

// buildChain para un Log debe resolver el Full padre y todos los logs intermedios en orden,
// sin incluir logs posteriores al objetivo.
func TestBuildChain_FullPlusLogsInOrder(t *testing.T) {
	repo := newFakeBackupRepo()
	full := mkFull("full-1", 5, "full-key")
	full.InstanceID = "inst-1"
	log1 := mkLog("log-1", "full-1", 3, "log-1-key")
	log1.InstanceID = "inst-1"
	log2 := mkLog("log-2", "full-1", 2, "log-2-key")
	log2.InstanceID = "inst-1"
	logAfterTarget := mkLog("log-3", "full-1", 1, "log-3-key")
	logAfterTarget.InstanceID = "inst-1"
	for _, r := range []*entities.BackupRecord{full, log1, log2, logAfterTarget} {
		_ = repo.Save(context.Background(), r)
	}

	uc := newTestRestoreUseCase(repo)
	// Restaurar hasta log2 (una hora antes que log-3): la cadena no debe incluir log-3.
	chain, err := uc.buildChain(context.Background(), log2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected chain of 3 (full+2 logs), got %d: %+v", len(chain), chain)
	}
	if chain[0].ID != "full-1" || chain[1].ID != "log-1" || chain[2].ID != "log-2" {
		t.Fatalf("unexpected chain order: %+v", chain)
	}
}

// RestoreAll exige la palabra de confirmación exacta.
func TestRestoreAll_RequiresConfirmPhrase(t *testing.T) {
	uc := newTestRestoreUseCase(newFakeBackupRepo())
	if err := uc.RestoreAll(context.Background(), "si"); err == nil {
		t.Fatalf("expected error for wrong confirmation phrase")
	}
	if err := uc.RestoreAll(context.Background(), "Confirmar"); err != nil {
		t.Fatalf("expected case-insensitive confirmation to be accepted, got: %v", err)
	}
}

// latestRestorePoints debe preferir el Log más reciente sobre su Full, y excluir master.
func TestLatestRestorePoints_PrefersLatestLogOverFullAndExcludesMaster(t *testing.T) {
	repo := newFakeBackupRepo()
	full := mkFull("full-1", 2, "full-key")
	full.InstanceID = "inst-1"
	full.DatabaseName = "ventas"
	log1 := mkLog("log-1", "full-1", 1, "log-1-key")
	log1.InstanceID = "inst-1"
	log1.DatabaseName = "ventas"

	masterFull := mkFull("master-full", 2, "master-key")
	masterFull.InstanceID = "inst-1"
	masterFull.DatabaseName = "master"

	for _, r := range []*entities.BackupRecord{full, log1, masterFull} {
		_ = repo.Save(context.Background(), r)
	}

	uc := newTestRestoreUseCase(repo)
	points, err := uc.latestRestorePoints(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := points["master"]; ok {
		t.Fatalf("master should be excluded from restore points")
	}
	if points["ventas"] != "log-1" {
		t.Fatalf("expected latest point for ventas to be log-1, got %q", points["ventas"])
	}
}
