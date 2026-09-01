package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	_ "github.com/microsoft/go-mssqldb"
)

// Adapter implements ports.DatabaseAdapter (and ports.LogCapableAdapter) for
// SQL Server. It backups up to local disk and uses docker exec to stream.
type Adapter struct {
	db       *sql.DB
	instance *entities.Instance
}

func New(inst *entities.Instance) (*Adapter, error) {
	dsn := buildDSN(inst)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión sqlserver: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Adapter{db: db, instance: inst}, nil
}

// defaultConnTimeoutSeconds is generous on purpose: go-mssqldb's "connection timeout" doubles as
// an idle Read/Write deadline for the entire life of the connection (it resets the deadline on
// every TDS packet, not just at login — see go-mssqldb's timeoutConn). BACKUP/RESTORE ... TO/FROM
// URL can go quiet for a while between STATS progress messages while SQL Server talks to S3, so a
// short timeout here causes spurious "i/o timeout" failures on real backups, not just slow logins.
func buildDSN(inst *entities.Instance) string {
	// trustservercertificate=true: los despliegues on-prem/Docker usan el certificado
	// autofirmado que SQL Server genera al arrancar, no uno emitido por una CA pública.
	trustCert := "true"
	connTimeout := 300 // default
	if inst.Extra != nil {
		if v, ok := inst.Extra["trust_cert"]; ok {
			trustCert = v
		}
		if v, ok := inst.Extra["conn_timeout_seconds"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				connTimeout = n
			}
		}
	}

	u := url.QueryEscape(inst.Username)
	p := url.QueryEscape(inst.Password)

	return fmt.Sprintf("sqlserver://%s:%s@%s:%d?connection+timeout=%d&trustservercertificate=%s",
		u, p, inst.Host, inst.Port, connTimeout, trustCert)
}

func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// ListDatabases returns all databases eligible for backup, including the system databases
// master and msdb (needed for full server disaster recovery). tempdb can't be backed up and
// model doesn't add DR value, so those stay excluded. Callers/instances can further exclude
// databases via Instance.ExcludedDatabases.
func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT name FROM sys.databases
		 WHERE name NOT IN ('tempdb','model')
		   AND state_desc = 'ONLINE'
		 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

// recoveryModel returns the recovery model of a database (FULL, SIMPLE or BULK_LOGGED).
func (a *Adapter) recoveryModel(ctx context.Context, database string) (string, error) {
	var model string
	err := a.db.QueryRowContext(ctx,
		`SELECT recovery_model_desc FROM sys.databases WHERE name = @p1`, database).Scan(&model)
	if err != nil {
		return "", fmt.Errorf("consultar recovery model de %q: %w", database, err)
	}
	return model, nil
}

// findDockerContainerByPort attempts to auto-detect the Docker container name for the SQL Server instance.
func findDockerContainerByPort(port int) (string, error) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("docker ps --format '{{.Names}}' --filter publish=%d", port))
	out, err := cmd.Output()
	if err == nil {
		names := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(names) > 0 && names[0] != "" {
			return names[0], nil
		}
	}
	cmd = exec.Command("sh", "-c", "docker ps --format '{{.Names}}' --filter ancestor=mcr.microsoft.com/mssql/server:2025-latest")
	out, err = cmd.Output()
	if err == nil {
		names := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(names) > 0 && names[0] != "" {
			return names[0], nil
		}
	}
	cmd = exec.Command("sh", "-c", "docker ps --format '{{.Names}}' --filter ancestor=mcr.microsoft.com/mssql/server")
	out, _ = cmd.Output()
	names := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(names) > 0 && names[0] != "" {
		return names[0], nil
	}
	return "", fmt.Errorf("no se encontró el contenedor de docker para sql server")
}

type dockerReader struct {
	io.ReadCloser
	cmd       *exec.Cmd
	container string
	tmpPath   string
}

func (r *dockerReader) Close() error {
	err := r.ReadCloser.Close()
	_ = r.cmd.Wait() // Esperar a que 'cat' termine
	exec.Command("docker", "exec", r.container, "rm", "-f", r.tmpPath).Run()
	return err
}

func (a *Adapter) objectKey(database, ext string) string {
	ts := time.Now().UTC().Format("20060102_1504")
	return fmt.Sprintf("%s/%s/%s/%s_%s.%s",
		string(entities.EngineSQLServer), a.instance.Name, database, database, ts, ext)
}

// Backup streams a full backup by saving it to the container's disk and reading it via docker exec.
func (a *Adapter) Backup(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error) {
	return a.backup(ctx, database, "bak", func(path string) string {
		return fmt.Sprintf(`BACKUP DATABASE [%s] TO DISK = '%s' WITH INIT, COMPRESSION, STATS = 25, CHECKSUM`, database, path)
	})
}

// BackupLog streams a transaction log backup identically.
func (a *Adapter) BackupLog(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error) {
	model, err := a.recoveryModel(ctx, database)
	if err != nil {
		return nil, ports.BackupMeta{}, err
	}
	if model == "SIMPLE" {
		return nil, ports.BackupMeta{}, ports.ErrLogBackupNotApplicable
	}
	return a.backup(ctx, database, "trn", func(path string) string {
		return fmt.Sprintf(`BACKUP LOG [%s] TO DISK = '%s' WITH INIT, COMPRESSION, STATS = 25, CHECKSUM`, database, path)
	})
}

func (a *Adapter) backup(ctx context.Context, database, ext string, sqlFor func(path string) string) (io.ReadCloser, ports.BackupMeta, error) {
	key := a.objectKey(database, ext)
	meta := ports.BackupMeta{DatabaseName: database, SuggestedKey: key}

	container, err := findDockerContainerByPort(a.instance.Port)
	if err != nil {
		return nil, meta, fmt.Errorf("detección docker: %w", err)
	}

	tmpPath := "/var/opt/mssql/data/tmp_backiie_" + ext + ".bak"

	// 1. Ejecutar respaldo local dentro de SQL Server
	if _, err := a.db.ExecContext(ctx, sqlFor(tmpPath)); err != nil {
		return nil, meta, fmt.Errorf("respaldo sql server (%s): %w", ext, err)
	}

	// 2. Extraer archivo vía docker exec
	dockerCmd := exec.CommandContext(ctx, "docker", "exec", "-i", container, "cat", tmpPath)
	stdout, err := dockerCmd.StdoutPipe()
	if err != nil {
		return nil, meta, fmt.Errorf("pipe docker exec: %w", err)
	}

	if err := dockerCmd.Start(); err != nil {
		return nil, meta, fmt.Errorf("iniciar extraccion: %w", err)
	}

	return &dockerReader{
		ReadCloser: stdout,
		cmd:        dockerCmd,
		container:  container,
		tmpPath:    tmpPath,
	}, meta, nil
}

// Restore sube el flujo binario a SQL Server y ejecuta RESTORE.
func (a *Adapter) Restore(ctx context.Context, database string, reader io.Reader, opts ports.RestoreOptions) error {
	if reader == nil {
		return fmt.Errorf("restaurar sql server: reader requerido (flujo vacío)")
	}
	container, err := findDockerContainerByPort(a.instance.Port)
	if err != nil {
		return fmt.Errorf("detección docker: %w", err)
	}
	tmpPath := "/var/opt/mssql/data/tmp_backiie_rest.bak"

	// 1. Enviar el stream al contenedor de SQL Server
	dockerCmd := exec.CommandContext(ctx, "docker", "exec", "-i", container, "sh", "-c", "cat > "+tmpPath)
	dockerCmd.Stdin = reader
	if err := dockerCmd.Run(); err != nil {
		return fmt.Errorf("enviar archivo al contenedor: %w", err)
	}
	defer exec.Command("docker", "exec", container, "rm", "-f", tmpPath).Run()

	recoveryClause := "RECOVERY"
	if opts.NoRecovery {
		recoveryClause = "NORECOVERY"
	}
	
	// 2. Ejecutar restauración local
	_, err = a.db.ExecContext(ctx, fmt.Sprintf(`
		RESTORE DATABASE [%s]
		FROM DISK = '%s'
		WITH REPLACE, STATS = 10, %s
	`, database, tmpPath, recoveryClause))
	if err != nil {
		return fmt.Errorf("restaurar base de datos: %w", err)
	}
	return nil
}

// RestoreLog sube el flujo binario del log y ejecuta RESTORE LOG.
func (a *Adapter) RestoreLog(ctx context.Context, database string, reader io.Reader, recovery bool) error {
	if reader == nil {
		return fmt.Errorf("restaurar log sql server: reader requerido")
	}
	container, err := findDockerContainerByPort(a.instance.Port)
	if err != nil {
		return fmt.Errorf("detección docker: %w", err)
	}
	tmpPath := "/var/opt/mssql/data/tmp_backiie_rest.trn"

	dockerCmd := exec.CommandContext(ctx, "docker", "exec", "-i", container, "sh", "-c", "cat > "+tmpPath)
	dockerCmd.Stdin = reader
	if err := dockerCmd.Run(); err != nil {
		return fmt.Errorf("enviar archivo de log al contenedor: %w", err)
	}
	defer exec.Command("docker", "exec", container, "rm", "-f", tmpPath).Run()

	recoveryClause := "NORECOVERY"
	if recovery {
		recoveryClause = "RECOVERY"
	}
	_, err = a.db.ExecContext(ctx, fmt.Sprintf(`
		RESTORE LOG [%s]
		FROM DISK = '%s'
		WITH STATS = 10, %s
	`, database, tmpPath, recoveryClause))
	if err != nil {
		return fmt.Errorf("restaurar log: %w", err)
	}
	return nil
}

func (a *Adapter) Close() error {
	return a.db.Close()
}

var (
	_ ports.DatabaseAdapter   = (*Adapter)(nil)
	_ ports.LogCapableAdapter = (*Adapter)(nil)
)
