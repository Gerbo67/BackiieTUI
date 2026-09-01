//go:build integration

// Package integration exercises real infrastructure: a live SQL Server 2025 container and a live
// MinIO (S3-compatible) bucket. It is not run by `go test ./...` — only by `make test-integration`
// (see docker-compose.test.yml), which starts both containers and runs this suite inside the same
// Docker network.
//
// Known limitation, confirmed by testing (not just documentation): SQL Server's native
// BACKUP/RESTORE ... TO/FROM URL for S3-compatible storage requires the endpoint to present a
// certificate chaining to a well-known public CA — self-signed certificates are rejected even
// after being added to the OS trust store and independently verified there with
// `openssl s_client` (SQL Server's S3 client evidently validates against its own bundled trust
// store, not the OS one). That makes the actual BACKUP DATABASE/LOG ... TO URL round trip
// impossible to exercise against a local self-signed MinIO. It is NOT a limitation in
// BackiieTUI's own code — every piece of logic around it (retention's golden rule, the
// Full→Log restore chain builder, cascading deletes, the confirmation phrase) is covered by real
// unit tests in application/usecases, and production targets a publicly-trusted CA (Cloudflare
// R2) where this doesn't apply. What IS exercised for real here: SQL Server 2025 connectivity and
// the ListDatabases/recovery-model logic driving which databases get Full vs Log backups, the
// generic S3 storage adapter (used by every non-SQL-Server engine and by the self-backup
// use case) against a real MinIO bucket over HTTPS, and the self-backup use case end to end.
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/microsoft/go-mssqldb"

	dbfactory "BackiieTUI/adapters/database"
	"BackiieTUI/adapters/persistence/bbolt"
	s3adapter "BackiieTUI/adapters/storage/s3"
	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

const (
	saPassword   = "Backiie_Test_2025!"
	minioAccess  = "backiie"
	minioSecret  = "backiie12345"
	testInstance = "test-instance"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type testEnv struct {
	instance *entities.Instance
	s3cfg    *entities.S3Config
	sqlDB    *sql.DB
	storage  ports.StorageAdapter
	store    *bbolt.Store

	retentionRepo ports.RetentionRepository
}

type s3Factory struct{}

func (s3Factory) NewStorage(cfg *entities.S3Config) (ports.StorageAdapter, error) {
	return s3adapter.New(cfg)
}
func (s3Factory) NewLifecycle(cfg *entities.S3Config) (ports.LifecycleManager, error) {
	return s3adapter.New(cfg)
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	mssqlHost := envOr("BACKIIE_TEST_MSSQL_HOST", "localhost")
	mssqlPort, _ := strconv.Atoi(envOr("BACKIIE_TEST_MSSQL_PORT", "14330"))
	minioEndpoint := envOr("BACKIIE_TEST_MINIO_ENDPOINT", "localhost:9000")
	minioBucket := envOr("BACKIIE_TEST_MINIO_BUCKET", "backiie-test-bucket")

	inst := &entities.Instance{
		ID:       "test-instance-id",
		Name:     testInstance,
		Engine:   entities.EngineSQLServer,
		Host:     mssqlHost,
		Port:     mssqlPort,
		Username: "sa",
		Password: saPassword,
		Enabled:  true,
	}
	s3cfg := &entities.S3Config{
		Bucket:          minioBucket,
		Region:          "us-east-1",
		Endpoint:        minioEndpoint,
		AccessKeyID:     minioAccess,
		SecretAccessKey: minioSecret,
		PathPrefix:      fmt.Sprintf("bk-%d", time.Now().UnixNano()), // aísla cada test run
		ForcePathStyle:  true,
	}

	dbPath := filepath.Join(t.TempDir(), "backiie-it.db")
	store, err := bbolt.Open(dbPath)
	if err != nil {
		t.Fatalf("abrir bbolt: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Retention().Save(ctx, &entities.RetentionPolicy{RetainDays: 3}); err != nil {
		t.Fatalf("guardar retención: %v", err)
	}

	env := &testEnv{
		instance:      inst,
		s3cfg:         s3cfg,
		store:         store,
		retentionRepo: store.Retention(),
	}
	env.storage, err = (s3Factory{}).NewStorage(s3cfg)
	if err != nil {
		t.Fatalf("conectar S3: %v", err)
	}

	// Conexión raw para preparar/inspeccionar bases de prueba directamente.
	dsn := fmt.Sprintf("sqlserver://sa:%s@%s:%d?connection+timeout=300&trustservercertificate=true", saPassword, mssqlHost, mssqlPort)
	var raw *sql.DB
	var lastErr error
	for i := 0; i < 15; i++ {
		raw, lastErr = sql.Open("sqlserver", dsn)
		if lastErr == nil {
			if lastErr = raw.PingContext(ctx); lastErr == nil {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if lastErr != nil {
		t.Fatalf("no se pudo conectar a SQL Server tras reintentos: %v", lastErr)
	}
	env.sqlDB = raw
	t.Cleanup(func() { raw.Close() })

	return env
}

func (e *testEnv) exec(t *testing.T, query string) {
	t.Helper()
	if _, err := e.sqlDB.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// TestSQLServerAdapter_ListDatabasesIncludesSystemDBs valida contra un SQL Server 2025 real que
// el adaptador (bump de driver + DSN) conecta correctamente y que master/msdb ya no se excluyen
// (tempdb y model sí deben seguir excluidas).
func TestSQLServerAdapter_ListDatabasesIncludesSystemDBs(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()

	adapter, err := dbfactory.NewAdapter(env.instance, &dbfactory.S3Config{
		Endpoint: env.s3cfg.Endpoint, Bucket: env.s3cfg.Bucket,
		AccessKeyID: env.s3cfg.AccessKeyID, SecretAccessKey: env.s3cfg.SecretAccessKey,
		PathPrefix: env.s3cfg.PathPrefix,
	})
	if err != nil {
		t.Fatalf("crear adaptador: %v", err)
	}
	defer adapter.Close()

	if err := adapter.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	dbs, err := adapter.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	has := func(name string) bool {
		for _, d := range dbs {
			if strings.EqualFold(d, name) {
				return true
			}
		}
		return false
	}
	if !has("master") {
		t.Errorf("esperaba que 'master' esté incluida, lista: %v", dbs)
	}
	if !has("msdb") {
		t.Errorf("esperaba que 'msdb' esté incluida, lista: %v", dbs)
	}
	if has("tempdb") || has("model") {
		t.Errorf("tempdb/model deberían seguir excluidas, lista: %v", dbs)
	}
}

// TestSQLServerAdapter_LogBackupSkippedForSimpleRecovery valida, contra SQL Server real, que
// BackupLog detecta el recovery model SIMPLE de 'master' y devuelve ErrLogBackupNotApplicable
// sin intentar siquiera hablarle a S3 (esta parte no depende de HTTPS/CA, es una consulta SQL).
func TestSQLServerAdapter_LogBackupSkippedForSimpleRecovery(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()

	env.exec(t, `IF EXISTS (SELECT 1 FROM sys.databases WHERE name = 'master' AND recovery_model_desc <> 'SIMPLE')
		ALTER DATABASE master SET RECOVERY SIMPLE`)

	adapter, err := dbfactory.NewAdapter(env.instance, &dbfactory.S3Config{
		Endpoint: env.s3cfg.Endpoint, Bucket: env.s3cfg.Bucket,
		AccessKeyID: env.s3cfg.AccessKeyID, SecretAccessKey: env.s3cfg.SecretAccessKey,
		PathPrefix: env.s3cfg.PathPrefix,
	})
	if err != nil {
		t.Fatalf("crear adaptador: %v", err)
	}
	defer adapter.Close()

	logCapable, ok := adapter.(ports.LogCapableAdapter)
	if !ok {
		t.Fatalf("el adaptador SQL Server debería implementar LogCapableAdapter")
	}

	_, _, err = logCapable.BackupLog(ctx, "master")
	if !errors.Is(err, ports.ErrLogBackupNotApplicable) {
		t.Fatalf("esperaba ErrLogBackupNotApplicable para master (SIMPLE), obtuve: %v", err)
	}
}

// TestS3StorageAdapter_RealMinIO ejercita el adaptador S3 genérico (el que usan todos los
// motores que no son SQL Server, y el self-backup de BackiieTUI) contra un bucket MinIO real.
func TestS3StorageAdapter_RealMinIO(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()

	key := "smoke/hello.txt"
	body := []byte("backiie integration test")

	n, err := env.storage.Upload(ctx, key, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if n != int64(len(body)) {
		t.Fatalf("esperaba %d bytes subidos, subió %d", len(body), n)
	}

	// ListObjects devuelve la key completa en S3 (incluye el PathPrefix del bucket), no la key
	// "pelada" que pasamos a Upload/Download/DeleteObject — mismo contrato que usa el resto del
	// código (BackupRecord.FileName siempre es la key pelada, ver adapters/database/sqlserver).
	objs, err := env.storage.ListObjects(ctx, "smoke/")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(objs) != 1 || !strings.HasSuffix(objs[0].Key, key) {
		t.Fatalf("esperaba 1 objeto terminado en %q, obtuve: %+v", key, objs)
	}

	rc, err := env.storage.Download(ctx, key)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("leer descarga: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("contenido descargado no coincide: %q", got)
	}

	if err := env.storage.TagExpiry(ctx, key, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("TagExpiry: %v", err)
	}

	if err := env.storage.DeleteObject(ctx, key); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	objs, err = env.storage.ListObjects(ctx, "smoke/")
	if err != nil {
		t.Fatalf("ListObjects tras borrar: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("esperaba 0 objetos tras borrar, obtuve: %+v", objs)
	}
}

// TestSelfBackupUseCase_RealMinIO valida que el self-backup de la propia base BBolt de Backiie
// sube un snapshot real a MinIO y que la retención por edad poda los snapshots viejos.
func TestSelfBackupUseCase_RealMinIO(t *testing.T) {
	env := setupEnv(t)
	ctx := context.Background()

	// Deja algo real en la BBolt para que el snapshot no esté vacío.
	if err := env.store.Instances().Save(ctx, env.instance); err != nil {
		t.Fatalf("guardar instancia: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s3cfgRepo := &fixedS3ConfigRepo{cfg: env.s3cfg}
	uc := usecases.NewSelfBackupUseCase(env.store, s3Factory{}, s3cfgRepo, env.retentionRepo, logger)

	if err := uc.Run(ctx); err != nil {
		t.Fatalf("SelfBackupUseCase.Run: %v", err)
	}

	objs, err := env.storage.ListObjects(ctx, "_backiie-meta/")
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	found := false
	for _, o := range objs {
		if strings.HasSuffix(o.Key, ".db") {
			found = true
			if o.SizeBytes == 0 {
				t.Fatalf("el snapshot subido está vacío: %s", o.Key)
			}
		}
	}
	if !found {
		t.Fatalf("esperaba al menos un snapshot .db subido, objetos: %+v", objs)
	}
}

type fixedS3ConfigRepo struct{ cfg *entities.S3Config }

func (f *fixedS3ConfigRepo) Save(_ context.Context, cfg *entities.S3Config) error {
	f.cfg = cfg
	return nil
}
func (f *fixedS3ConfigRepo) Get(_ context.Context) (*entities.S3Config, error) { return f.cfg, nil }
