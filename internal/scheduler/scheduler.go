package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"github.com/robfig/cron/v3"
)

// BackupScheduler runs periodic backups using cron expressions: full backups, SQL Server log
// backups, BackiieTUI's own self-backup, and daily retention cleanup.
type BackupScheduler struct {
	cron          *cron.Cron
	backupUC      *usecases.RunBackupUseCase
	retentionUC   *usecases.BackupQueryUseCase
	selfBackupUC  *usecases.SelfBackupUseCase
	schedulerRepo ports.SchedulerConfigRepository
	entryIDs      []cron.EntryID
	logger        *slog.Logger
}

func New(
	backupUC *usecases.RunBackupUseCase,
	retentionUC *usecases.BackupQueryUseCase,
	selfBackupUC *usecases.SelfBackupUseCase,
	schedulerRepo ports.SchedulerConfigRepository,
	logger *slog.Logger,
) *BackupScheduler {
	return &BackupScheduler{
		backupUC:      backupUC,
		retentionUC:   retentionUC,
		selfBackupUC:  selfBackupUC,
		schedulerRepo: schedulerRepo,
		logger:        logger,
	}
}

// Start loads config from persistence and starts the cron scheduler.
func (s *BackupScheduler) Start(ctx context.Context) error {
	cfg, err := s.schedulerRepo.Get(ctx)
	if err != nil {
		// Use defaults if not configured
		def := entities.DefaultSchedulerConfig()
		cfg = &def
	}

	if !cfg.Enabled {
		s.logger.Info("scheduler desactivado, sin jobs registrados")
		return nil
	}

	loc := time.UTC
	if cfg.TimeZone != "" {
		if l, err := time.LoadLocation(cfg.TimeZone); err == nil {
			loc = l
		}
	}

	s.cron = cron.New(cron.WithLocation(loc), cron.WithSeconds())

	for _, expr := range cfg.CronExprsFull {
		id, err := s.cron.AddFunc(expr, func() {
			s.logger.Info("iniciando ciclo de respaldos Full")
			if err := s.backupUC.RunForAll(ctx); err != nil {
				s.logger.Error("error en ciclo de respaldos Full", "err", err)
			}
		})
		if err != nil {
			return fmt.Errorf("agregar cron full %q: %w", expr, err)
		}
		s.entryIDs = append(s.entryIDs, id)
		s.logger.Info("job de respaldo Full registrado", "cron", expr)
	}

	for _, expr := range cfg.CronExprsLog {
		id, err := s.cron.AddFunc(expr, func() {
			s.logger.Info("iniciando ciclo de respaldos de Log (SQL Server)")
			if err := s.backupUC.RunLogsForAll(ctx); err != nil {
				s.logger.Error("error en ciclo de respaldos de Log", "err", err)
			}
		})
		if err != nil {
			return fmt.Errorf("agregar cron log %q: %w", expr, err)
		}
		s.entryIDs = append(s.entryIDs, id)
		s.logger.Info("job de respaldo de Log registrado", "cron", expr)
	}

	// Self-backup de BackiieTUI, 2 veces al día (00:00 y 12:00).
	if s.selfBackupUC != nil {
		id, err := s.cron.AddFunc("0 0 0,12 * * *", func() {
			if err := s.selfBackupUC.Run(ctx); err != nil {
				s.logger.Error("error en self-backup de BackiieTUI", "err", err)
			}
		})
		if err != nil {
			return fmt.Errorf("agregar cron de self-backup: %w", err)
		}
		s.entryIDs = append(s.entryIDs, id)
		s.logger.Info("job de self-backup registrado", "cron", "0 0 0,12 * * *")
	}

	// Limpieza de retención diaria a la 1:00 AM (zona horaria del scheduler).
	if s.retentionUC != nil {
		id, err := s.cron.AddFunc("0 0 1 * * *", func() {
			n, err := s.retentionUC.ApplyRetention(ctx)
			if err != nil {
				s.logger.Error("error aplicando retención", "err", err)
				return
			}
			if n > 0 {
				s.logger.Info("retención diaria aplicada", "eliminados", n)
			}
		})
		if err != nil {
			return fmt.Errorf("agregar cron de retención: %w", err)
		}
		s.entryIDs = append(s.entryIDs, id)
		s.logger.Info("job de limpieza de retención registrado", "cron", "0 1 * * *")
	}

	s.cron.Start()
	return nil
}

// Reload reloads cron expressions from persistence, restarting all jobs.
func (s *BackupScheduler) Reload(ctx context.Context) error {
	s.Stop()
	return s.Start(ctx)
}

// Stop stops all scheduled jobs.
func (s *BackupScheduler) Stop() {
	if s.cron != nil {
		s.cron.Stop()
		s.cron = nil
		s.entryIDs = nil
	}
}

// NextRunTimes returns the next scheduled run times for all jobs.
func (s *BackupScheduler) NextRunTimes() []time.Time {
	if s.cron == nil {
		return nil
	}
	var times []time.Time
	for _, entry := range s.cron.Entries() {
		times = append(times, entry.Next)
	}
	return times
}
