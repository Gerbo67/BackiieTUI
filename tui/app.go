package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"BackiieTUI/internal/scheduler"
	"BackiieTUI/tui/styles"
	"BackiieTUI/tui/views/backups"
	"BackiieTUI/tui/views/instances"
	retentionview "BackiieTUI/tui/views/retention"
	"BackiieTUI/tui/views/s3config"
)

const (
	TabDashboard = 0
	TabInstances = 1
	TabBackups   = 2
	TabS3Config  = 3
	TabRetention = 4
)

var tabNames = []string{
	"  Panel  ",
	"  Instancias  ",
	"  Respaldos  ",
	"  S3  ",
	"  Retención  ",
}

// ---- notifier ----

// TUINotifier implements ports.Notifier and sends messages to the TUI program.
type TUINotifier struct {
	program *tea.Program
}

func NewTUINotifier(p *tea.Program) *TUINotifier { return &TUINotifier{program: p} }

func (n *TUINotifier) NotifyBackupStarted(inst, db string) {
	n.program.Send(BackupStartedMsg{InstanceName: inst, DBName: db})
}
func (n *TUINotifier) NotifyBackupProgress(inst, db string, bytes int64) {
	n.program.Send(BackupProgressMsg{InstanceName: inst, DBName: db, BytesWritten: bytes})
}
func (n *TUINotifier) NotifyBackupCompleted(inst, db, id string) {
	n.program.Send(BackupCompletedMsg{InstanceName: inst, DBName: db, BackupID: id, At: time.Now()})
}
func (n *TUINotifier) NotifyBackupFailed(inst, db string, err error) {
	n.program.Send(BackupFailedMsg{InstanceName: inst, DBName: db, Err: err})
}

var _ ports.Notifier = (*TUINotifier)(nil)

// ---- deps ----

type AppDeps struct {
	InstanceUC  *usecases.InstanceUseCase
	RunBackupUC *usecases.RunBackupUseCase
	BackupQUC   *usecases.BackupQueryUseCase
	S3ConfigUC  *usecases.S3ConfigUseCase
	RetentionUC *usecases.RetentionUseCase
	LifecycleUC *usecases.LifecycleUseCase
	RestoreUC   *usecases.RestoreUseCase
	SchedulerUC *usecases.SchedulerUseCase
	Scheduler   *scheduler.BackupScheduler
}

// ---- internal messages ----

// appTickMsg drives notification pruning and periodic dashboard refresh.
type appTickMsg struct{ t time.Time }

func appTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return appTickMsg{t: t}
	})
}

// dashLoadedMsg carries fresh dashboard data.
type dashLoadedMsg struct {
	instances    []*entities.Instance
	backups      []*entities.BackupRecord
	s3Configured bool
}

// ---- model ----

// Model is the root Bubble Tea model.
type Model struct {
	activeTab     int
	prevTab       int
	width, height int
	deps          AppDeps
	tickCount     int

	// Sub-views
	instancesView *instances.Model
	backupsView   *backups.Model
	s3configView  *s3config.Model
	retentionView *retentionview.Model

	// Dashboard cached state (loaded async, never inside View())
	dashInstances []*entities.Instance
	dashBackups   []*entities.BackupRecord
	dashSpinner   spinner.Model
	dashLoading   bool
	s3Configured  bool

	// Cross-cutting state
	notifications []notification
	activeJobs    []string
}

type notification struct {
	text    string
	isError bool
	expires time.Time
}

func NewModel(deps AppDeps) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	ctx := context.Background()
	return &Model{
		deps:          deps,
		dashSpinner:   sp,
		instancesView: instances.NewModel(ctx, deps.InstanceUC, deps.RunBackupUC),
		backupsView:   backups.NewModel(ctx, deps.BackupQUC, deps.RestoreUC),
		s3configView:  s3config.NewModel(ctx, deps.S3ConfigUC),
		retentionView: retentionview.NewModel(ctx, deps.RetentionUC, deps.Scheduler, deps.LifecycleUC, deps.SchedulerUC),
	}
}

func (m *Model) Init() tea.Cmd {
	m.dashLoading = true
	return tea.Batch(
		appTickCmd(),
		m.dashSpinner.Tick,
		m.loadDashCmd(),
		m.instancesView.Init(),
		m.backupsView.Init(),
		m.s3configView.Init(),
		m.retentionView.Init(),
	)
}

// ---- update ----

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	// Periodic tick: prune notifications + periodic dashboard refresh
	case appTickMsg:
		m.tickCount++
		cmds = append(cmds, appTickCmd()) // reschedule

		// Prune expired notifications
		now := time.Now()
		var active []notification
		for _, n := range m.notifications {
			if n.expires.After(now) {
				active = append(active, n)
			}
		}
		m.notifications = active

		// Refresh dashboard data every 10 s while on dashboard tab
		if m.tickCount%10 == 0 && m.activeTab == TabDashboard {
			cmds = append(cmds, m.loadDashCmd())
		}
		return m, tea.Batch(cmds...)

	// Dashboard spinner tick (only while loading)
	case spinner.TickMsg:
		if m.dashLoading {
			var cmd tea.Cmd
			m.dashSpinner, cmd = m.dashSpinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	// Dashboard data loaded
	case dashLoadedMsg:
		m.dashInstances = msg.instances
		m.dashBackups = msg.backups
		m.s3Configured = msg.s3Configured
		m.dashLoading = false
		if !m.s3Configured && m.activeTab != TabS3Config {
			m.prevTab = m.activeTab
			m.activeTab = TabS3Config
			m.push("Por favor, configura S3 antes de continuar.", true, 5*time.Second)
			cmds = append(cmds, m.onTabActivated(m.activeTab))
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, GlobalKeys.Quit):
			return m, tea.Quit

		case key.Matches(msg, GlobalKeys.TabNext):
			if !m.s3Configured {
				m.push("Por favor, configura S3 antes de continuar.", true, 3*time.Second)
				return m, nil
			}
			m.prevTab = m.activeTab
			m.activeTab = (m.activeTab + 1) % len(tabNames)
			if m.activeTab != m.prevTab {
				cmds = append(cmds, m.onTabActivated(m.activeTab))
			}
			return m, tea.Batch(cmds...)

		case key.Matches(msg, GlobalKeys.TabPrev):
			if !m.s3Configured {
				m.push("Por favor, configura S3 antes de continuar.", true, 3*time.Second)
				return m, nil
			}
			m.prevTab = m.activeTab
			m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
			if m.activeTab != m.prevTab {
				cmds = append(cmds, m.onTabActivated(m.activeTab))
			}
			return m, tea.Batch(cmds...)
		}

	case NavigateMsg:
		if !m.s3Configured && msg.Tab != TabS3Config {
			m.push("Por favor, configura S3 antes de continuar.", true, 3*time.Second)
			return m, nil
		}
		m.prevTab = m.activeTab
		m.activeTab = msg.Tab
		cmds = append(cmds, m.onTabActivated(m.activeTab))
		return m, tea.Batch(cmds...)

	case NotificationMsg:
		m.push(msg.Text, msg.IsError, 4*time.Second)

	case BackupStartedMsg:
		job := fmt.Sprintf("%s en %s", msg.DBName, msg.InstanceName)
		m.activeJobs = appendUnique(m.activeJobs, fmt.Sprintf("%s @ %s", msg.DBName, msg.InstanceName))
		m.push("Iniciando respaldo: "+job, false, 3*time.Second)

	case BackupCompletedMsg:
		m.removeJob(msg.DBName, msg.InstanceName)
		m.push(fmt.Sprintf("Respaldo completado: %s — %s", msg.InstanceName, msg.DBName), false, 5*time.Second)
		cmds = append(cmds, m.loadDashCmd())

	case BackupFailedMsg:
		m.removeJob(msg.DBName, msg.InstanceName)
		m.push(fmt.Sprintf("Error en respaldo de %s (%s): %v", msg.DBName, msg.InstanceName, msg.Err), true, 8*time.Second)
		cmds = append(cmds, m.loadDashCmd())

	case s3config.SavedMsg:
		if msg.Err == nil {
			m.s3Configured = true
		}
	}

	// Route msg to the active view
	switch m.activeTab {
	case TabInstances:
		newView, cmd := m.instancesView.Update(msg)
		m.instancesView = newView.(*instances.Model)
		cmds = append(cmds, cmd)
	case TabBackups:
		newView, cmd := m.backupsView.Update(msg)
		m.backupsView = newView.(*backups.Model)
		cmds = append(cmds, cmd)
	case TabS3Config:
		newView, cmd := m.s3configView.Update(msg)
		m.s3configView = newView.(*s3config.Model)
		cmds = append(cmds, cmd)
	case TabRetention:
		newView, cmd := m.retentionView.Update(msg)
		m.retentionView = newView.(*retentionview.Model)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// onTabActivated triggers data refresh when a tab becomes active.
func (m *Model) onTabActivated(tab int) tea.Cmd {
	switch tab {
	case TabDashboard:
		m.dashLoading = true
		return tea.Batch(m.dashSpinner.Tick, m.loadDashCmd())
	case TabInstances:
		return m.instancesView.Refresh()
	case TabBackups:
		return m.backupsView.Refresh()
	case TabS3Config:
		return m.s3configView.Refresh()
	case TabRetention:
		return m.retentionView.Refresh()
	}
	return nil
}

// loadDashCmd loads dashboard data in a goroutine — never call from View().
func (m *Model) loadDashCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		insts, _ := m.deps.InstanceUC.FindAll(ctx)
		bkps, _ := m.deps.BackupQUC.FindAll(ctx)
		s3cfg, _ := m.deps.S3ConfigUC.Get(ctx)
		isConfigured := s3cfg != nil && s3cfg.Bucket != ""
		return dashLoadedMsg{instances: insts, backups: bkps, s3Configured: isConfigured}
	}
}

// ---- view ----

func (m *Model) View() string {
	if m.width == 0 {
		return "Cargando BackiieTUI..."
	}

	var sb strings.Builder
	sb.WriteString(m.renderTabBar())
	sb.WriteString("\n\n") // Spacing

	// Render the active view
	sb.WriteString(m.renderActiveView())
	sb.WriteString("\n")

	// Render notifications at the bottom, before help
	if len(m.notifications) > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.renderNotifications())
	} else {
		// Empty space so the screen doesn't jump
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(m.renderHelp())
	return sb.String()
}

func (m *Model) renderTabBar() string {
	var tabs []string
	for i, name := range tabNames {
		if i == m.activeTab {
			tabs = append(tabs, styles.StyleTabActive.Render(name))
		} else {
			tabs = append(tabs, styles.StyleTabInactive.Render(name))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	if len(m.activeJobs) > 0 {
		bar += styles.StyleWarning.Render(fmt.Sprintf("  ⟳ %d en progreso", len(m.activeJobs)))
	}
	return bar
}

func (m *Model) renderNotifications() string {
	var lines []string
	for _, n := range m.notifications {
		s := styles.StyleSuccess
		if n.isError {
			s = styles.StyleDanger
		}
		lines = append(lines, s.Render(n.text))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderActiveView() string {
	switch m.activeTab {
	case TabDashboard:
		return m.renderDashboard() // pure: uses only cached m.dash* fields
	case TabInstances:
		return m.instancesView.View()
	case TabBackups:
		return m.backupsView.View()
	case TabS3Config:
		return m.s3configView.View()
	case TabRetention:
		return m.retentionView.View()
	}
	return ""
}

// renderDashboard is pure: reads only cached fields on m, no I/O.
func (m *Model) renderDashboard() string {
	var stats strings.Builder
	stats.WriteString(styles.StyleTitle.Render("BackiieTUI") + "\n")
	stats.WriteString(styles.StyleMuted.Render("Sistema de Gestión de Respaldos") + "\n\n")

	if m.dashLoading && len(m.dashInstances) == 0 {
		stats.WriteString(m.dashSpinner.View() + " " + styles.StyleMuted.Render("Cargando..."))
		return styles.StyleCard.Render(stats.String())
	}

	// Loading indicator while refreshing (data already present)
	if m.dashLoading {
		stats.WriteString(m.dashSpinner.View() + " " + styles.StyleMuted.Render("Actualizando...") + "\n\n")
	}

	engineCount := map[string]int{}
	for _, inst := range m.dashInstances {
		engineCount[inst.Engine.String()]++
	}

	stats.WriteString(fmt.Sprintf("%s %s\n",
		styles.StyleLabel.Render("Instancias:"),
		styles.StyleValue.Render(fmt.Sprintf("%d", len(m.dashInstances)))))

	for _, eng := range []string{"SQL Server", "MySQL", "MariaDB", "PostgreSQL", "Redis"} {
		if n := engineCount[eng]; n > 0 {
			stats.WriteString(fmt.Sprintf("  %s %d\n",
				styles.StyleMuted.Render(eng+":"), n))
		}
	}

	completed, failed := 0, 0
	for _, b := range m.dashBackups {
		switch b.Status.String() {
		case "Completado":
			completed++
		case "Fallido":
			failed++
		}
	}
	stats.WriteString(fmt.Sprintf("%s %s\n",
		styles.StyleLabel.Render("Respaldos:"),
		styles.StyleValue.Render(fmt.Sprintf("%d", len(m.dashBackups)))))
	stats.WriteString(fmt.Sprintf("  %s %s   %s %s\n",
		styles.StyleMuted.Render("exitosos:"), styles.StyleSuccess.Render(fmt.Sprintf("%d", completed)),
		styles.StyleMuted.Render("fallidos:"), styles.StyleDanger.Render(fmt.Sprintf("%d", failed))))

	if len(m.activeJobs) > 0 {
		stats.WriteString("\n")
		stats.WriteString(styles.StyleWarning.Render(fmt.Sprintf("⟳ %d activo(s)", len(m.activeJobs))) + "\n")
		for _, j := range m.activeJobs {
			stats.WriteString(styles.StyleMuted.Render("  • "+j) + "\n")
		}
	}

	nextRuns := m.deps.Scheduler.NextRunTimes()
	if len(nextRuns) > 0 {
		stats.WriteString("\n")
		stats.WriteString(styles.StyleLabel.Render("Próximos respaldos:") + "\n")
		for _, t := range nextRuns {
			stats.WriteString("  " + styles.StyleAccent.Render(t.Local().Format("Mon 02 Jan 15:04 MST")) + "\n")
		}
	}

	statsCard := styles.StyleCard.Render(stats.String())

	// Activity feed
	var activity strings.Builder
	activity.WriteString(styles.StyleTitle.Render("Actividad Reciente") + "\n\n")

	recent := m.dashBackups
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}
	if len(recent) == 0 {
		activity.WriteString(styles.StyleMuted.Render("No se encontraron respaldos registrados.") + "\n")
		activity.WriteString(styles.StyleMuted.Render("Configure instancias en la pestaña correspondiente."))
	}
	for i := len(recent) - 1; i >= 0; i-- {
		r := recent[i]
		icon := styles.StatusStyle(r.Status.String()).Render(styles.StatusIcon(r.Status.String()))
		activity.WriteString(fmt.Sprintf("%s  %-16s  %-14s  %s\n",
			icon,
			truncateStr(r.InstanceName, 16),
			truncateStr(r.DatabaseName, 14),
			styles.StyleMuted.Render(r.StartedAt.Format("02 Jan 15:04"))))
	}

	activityCard := styles.StyleCard.Render(activity.String())

	w := m.width/2 - 3
	if w < 30 {
		w = 30
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(w).Render(statsCard),
		"  ",
		lipgloss.NewStyle().Width(w).Render(activityCard),
	)
}

func (m *Model) renderHelp() string {
	// Solo teclas globales de navegación. Cada vista gestiona sus propias teclas de acción.
	return styles.StyleHelp.Render("PgDown / PgUp  cambiar pestaña   ctrl+c  salir")
}

// ---- helpers ----

func (m *Model) push(text string, isError bool, ttl time.Duration) {
	n := notification{
		text:    text,
		isError: isError,
		expires: time.Now().Add(ttl),
	}
	// Keep only the most recent notification to avoid stacking/jumping
	m.notifications = []notification{n}
}

func (m *Model) removeJob(db, inst string) {
	job := fmt.Sprintf("%s @ %s", db, inst)
	var out []string
	for _, j := range m.activeJobs {
		if j != job {
			out = append(out, j)
		}
	}
	m.activeJobs = out
}

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
