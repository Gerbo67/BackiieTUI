package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"BackiieTUI/adapters/persistence/bbolt"
	s3adapter "BackiieTUI/adapters/storage/s3"
	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"BackiieTUI/internal/scheduler"
	"BackiieTUI/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "recover" {
		runRecover()
		return
	}

	// -- Logger --
	// Writes to stderr always (journald captures stderr in systemd).
	// If BACKIIE_LOG_FILE is set, also appends to that file.
	var logDest io.Writer = os.Stderr
	if logFile := envOr("BACKIIE_LOG_FILE", ""); logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err == nil {
			logDest = io.MultiWriter(os.Stderr, f)
			defer f.Close()
		} else {
			fmt.Fprintf(os.Stderr, "advertencia: no se pudo abrir log file %q: %v\n", logFile, err)
		}
	}
	logger := slog.New(slog.NewTextHandler(logDest, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// -- Persistence (BBolt) --
	dbPath := envOr("BACKIIE_DB_PATH", "backiie.db")
	store, err := bbolt.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error al abrir la base de datos: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	instanceRepo := store.Instances()
	backupRepo := store.BackupRecords()
	s3cfgRepo := store.S3Config()
	retentionRepo := store.Retention()
	schedulerCfgRepo := store.SchedulerConfig()
	restoreRepo := store.RestoreRecords()

	ctx := context.Background()
	if _, err := schedulerCfgRepo.Get(ctx); err != nil {
		def := entities.DefaultSchedulerConfig()
		_ = schedulerCfgRepo.Save(ctx, &def)
	}

	// -- S3 Factory (creates fresh adapters from current config on each use) --
	factory := s3Factory{}

	// -- Use Cases --
	instanceUC := usecases.NewInstanceUseCase(instanceRepo, s3cfgRepo)
	s3configUC := usecases.NewS3ConfigUseCase(s3cfgRepo)
	retentionUC := usecases.NewRetentionUseCase(retentionRepo)
	schedulerUC := usecases.NewSchedulerUseCase(schedulerCfgRepo)
	backupQueryUC := usecases.NewBackupQueryUseCase(backupRepo, factory, s3cfgRepo, retentionRepo)
	lifecycleUC := usecases.NewLifecycleUseCase(factory, retentionRepo, instanceRepo, s3cfgRepo)
	restoreUC := usecases.NewRestoreUseCase(instanceRepo, backupRepo, restoreRepo, factory, s3cfgRepo)
	selfBackupUC := usecases.NewSelfBackupUseCase(store, factory, s3cfgRepo, retentionRepo, logger)

	notifierProxy := &notifierProxy{}

	runBackupUC := usecases.NewRunBackupUseCase(
		instanceRepo,
		backupRepo,
		factory,
		s3cfgRepo,
		retentionRepo,
		notifierProxy,
		logger,
	)

	// -- Scheduler --
	sched := scheduler.New(runBackupUC, backupQueryUC, selfBackupUC, schedulerCfgRepo, logger)

	if err := sched.Start(ctx); err != nil {
		logger.Warn("no se pudo iniciar el scheduler", "err", err)
	}
	defer sched.Stop()

	// -- Headless (sin TTY) o TUI interactiva --
	if !hasTTY() {
		logger.Info("modo servicio: TUI desactivada, scheduler activo", "db", dbPath)
		runHeadless(ctx, backupQueryUC, logger)
		return
	}

	// Modo interactivo: construir la TUI
	model := tui.NewModel(tui.AppDeps{
		InstanceUC:  instanceUC,
		RunBackupUC: runBackupUC,
		BackupQUC:   backupQueryUC,
		S3ConfigUC:  s3configUC,
		RetentionUC: retentionUC,
		LifecycleUC: lifecycleUC,
		RestoreUC:   restoreUC,
		SchedulerUC: schedulerUC,
		Scheduler:   sched,
	})

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	notifierProxy.notifier = tui.NewTUINotifier(program)

	go func() {
		if n, err := backupQueryUC.ApplyRetention(ctx); err == nil && n > 0 {
			logger.Info("retención aplicada al inicio", "eliminados", n)
		}
	}()

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error en TUI: %v\n", err)
		os.Exit(1)
	}
}

// runHeadless blocks until SIGTERM or SIGINT, keeping the scheduler alive.
// This is the path taken when stdout is not a TTY (systemd service, container, pipe).
func runHeadless(ctx context.Context, backupQueryUC *usecases.BackupQueryUseCase, logger *slog.Logger) {
	go func() {
		if n, err := backupQueryUC.ApplyRetention(ctx); err == nil && n > 0 {
			logger.Info("retención aplicada al inicio", "eliminados", n)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit
	logger.Info("señal recibida, apagando", "señal", sig.String())
}

// hasTTY returns true when stdout is an interactive terminal.
// Returns false when running under systemd, in a pipe, or redirected to a file.
func hasTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// notifierProxy allows wiring the TUI notifier after the program is created.
// In headless mode the notifier stays nil and all calls are silently dropped.
type notifierProxy struct {
	notifier interface {
		NotifyBackupStarted(string, string)
		NotifyBackupProgress(string, string, int64)
		NotifyBackupCompleted(string, string, string)
		NotifyBackupFailed(string, string, error)
	}
}

func (n *notifierProxy) NotifyBackupStarted(inst, db string) {
	if n.notifier != nil {
		n.notifier.NotifyBackupStarted(inst, db)
	}
}
func (n *notifierProxy) NotifyBackupProgress(inst, db string, bytes int64) {
	if n.notifier != nil {
		n.notifier.NotifyBackupProgress(inst, db, bytes)
	}
}
func (n *notifierProxy) NotifyBackupCompleted(inst, db, id string) {
	if n.notifier != nil {
		n.notifier.NotifyBackupCompleted(inst, db, id)
	}
}
func (n *notifierProxy) NotifyBackupFailed(inst, db string, err error) {
	if n.notifier != nil {
		n.notifier.NotifyBackupFailed(inst, db, err)
	}
}

// s3Factory creates fresh S3 adapter instances so config changes made via the
// TUI are picked up by the scheduler on the next run without a restart.
type s3Factory struct{}

func (s3Factory) NewStorage(cfg *entities.S3Config) (ports.StorageAdapter, error) {
	return s3adapter.New(cfg)
}

func (s3Factory) NewLifecycle(cfg *entities.S3Config) (ports.LifecycleManager, error) {
	return s3adapter.New(cfg)
}
