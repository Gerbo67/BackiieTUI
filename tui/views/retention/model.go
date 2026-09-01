package retention

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/internal/scheduler"
	"BackiieTUI/tui/styles"
)

type subTab int

const (
	subTabRetention subTab = 0
	subTabScheduler subTab = 1
	subTabLifecycle subTab = 2
)

// Model manages the retention/scheduler/lifecycle tab.
type Model struct {
	ctx         context.Context
	retentionUC *usecases.RetentionUseCase
	lifecycleUC *usecases.LifecycleUseCase
	schedulerUC *usecases.SchedulerUseCase
	sched       *scheduler.BackupScheduler
	activeTab   subTab

	// Retention form
	retDays  textinput.Model
	retFocus int
	policies []*entities.RetentionPolicy

	// Scheduler form
	cronExpr0  textinput.Model
	cronExpr1  textinput.Model
	timezone   textinput.Model
	schedCfg   *entities.SchedulerConfig
	schedFocus int

	// Lifecycle
	lcRules    []entities.LifecycleRule
	lcSelected int
	lcLoading  bool
	lcConfirm  bool
	lcSpinner  spinner.Model

	err     string
	message string
}

func NewModel(
	ctx context.Context,
	retentionUC *usecases.RetentionUseCase,
	sched *scheduler.BackupScheduler,
	lifecycleUC *usecases.LifecycleUseCase,
	schedulerUC *usecases.SchedulerUseCase,
) *Model {
	retDays := textinput.New()
	retDays.Placeholder = "3"
	retDays.SetValue("3")

	cron0 := textinput.New()
	cron0.Placeholder = "0 0 0 * * *"
	cron0.SetValue("0 0 0 * * *")

	cron1 := textinput.New()
	cron1.Placeholder = "0 0 * * * *"
	cron1.SetValue("0 0 * * * *")

	tz := textinput.New()
	tz.Placeholder = "UTC"
	tz.SetValue("UTC")

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return &Model{
		ctx:         ctx,
		retentionUC: retentionUC,
		lifecycleUC: lifecycleUC,
		schedulerUC: schedulerUC,
		sched:       sched,
		retDays:     retDays,
		cronExpr0:   cron0,
		cronExpr1:   cron1,
		timezone:    tz,
		lcSpinner:   sp,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.Refresh()
}

func (m *Model) Refresh() tea.Cmd {
	return m.loadCmd()
}

// ---- messages ----

type loadedMsg struct {
	policies []*entities.RetentionPolicy
	cfg      *entities.SchedulerConfig
	err      error
}

type savedMsg struct{ err error }

type lcLoadedMsg struct {
	rules []entities.LifecycleRule
	err   error
}

type lcSyncedMsg struct{ err error }
type lcDeletedMsg struct{ err error }

// ---- update ----

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case loadedMsg:
		if msg.err == nil {
			m.policies = msg.policies
		}
		if msg.cfg != nil {
			m.schedCfg = msg.cfg
			if len(msg.cfg.CronExprsFull) > 0 {
				m.cronExpr0.SetValue(msg.cfg.CronExprsFull[0])
			}
			if len(msg.cfg.CronExprsLog) > 0 {
				m.cronExpr1.SetValue(msg.cfg.CronExprsLog[0])
			}
			m.timezone.SetValue(msg.cfg.TimeZone)
		}
		return m, nil

	case savedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.message = ""
		} else {
			m.message = "Guardado exitosamente"
			m.err = ""
		}
		return m, nil

	case lcLoadedMsg:
		m.lcLoading = false
		if msg.err != nil {
			m.err = lifecycleErrHint(msg.err)
		} else {
			m.lcRules = msg.rules
			if m.lcSelected >= len(m.lcRules) && len(m.lcRules) > 0 {
				m.lcSelected = len(m.lcRules) - 1
			}
		}
		return m, nil

	case lcSyncedMsg:
		m.lcLoading = false
		if msg.err != nil {
			m.err = lifecycleErrHint(msg.err)
			m.message = ""
		} else {
			m.message = "Reglas S3 sincronizadas correctamente"
			m.err = ""
		}
		return m, m.lcLoadCmd()

	case lcDeletedMsg:
		m.lcLoading = false
		m.lcConfirm = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.message = ""
		} else {
			m.message = "Regla eliminada"
			m.err = ""
		}
		return m, m.lcLoadCmd()

	case spinner.TickMsg:
		if m.lcLoading {
			var cmd tea.Cmd
			m.lcSpinner, cmd = m.lcSpinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			m.activeTab = subTabRetention
			m.clearStatus()
		case "2":
			m.activeTab = subTabScheduler
			m.clearStatus()
		case "3":
			m.activeTab = subTabLifecycle
			m.clearStatus()
			if len(m.lcRules) == 0 && !m.lcLoading {
				m.lcLoading = true
				return m, tea.Batch(m.lcSpinner.Tick, m.lcLoadCmd())
			}
		}

		if m.activeTab == subTabLifecycle {
			return m.updateLifecycle(msg)
		}

		switch msg.String() {
		case "tab", "down":
			m.nextFocus()
			return m, nil
		case "shift+tab", "up":
			m.prevFocus()
			return m, nil
		case "enter":
			return m, m.saveCmd()
		case "r":
			return m, m.loadCmd()
		default:
			return m.updateField(msg)
		}
	}
	return m, nil
}

func (m *Model) updateLifecycle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.lcConfirm {
		switch msg.String() {
		case "y", "Y", "s", "S":
			if len(m.lcRules) > 0 {
				ruleID := m.lcRules[m.lcSelected].ID
				m.lcLoading = true
				return m, tea.Batch(m.lcSpinner.Tick, func() tea.Msg {
					err := m.lifecycleUC.DeleteRule(m.ctx, ruleID)
					return lcDeletedMsg{err: err}
				})
			}
		default:
			m.lcConfirm = false
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.lcSelected > 0 {
			m.lcSelected--
		}
	case "down", "j":
		if m.lcSelected < len(m.lcRules)-1 {
			m.lcSelected++
		}
	case "s":
		m.clearStatus()
		m.lcLoading = true
		return m, tea.Batch(m.lcSpinner.Tick, func() tea.Msg {
			err := m.lifecycleUC.SyncWithRetention(m.ctx)
			return lcSyncedMsg{err: err}
		})
	case "d", "delete":
		if len(m.lcRules) > 0 {
			m.lcConfirm = true
		}
	case "r", "f5":
		m.lcLoading = true
		return m, tea.Batch(m.lcSpinner.Tick, m.lcLoadCmd())
	}
	return m, nil
}

func (m *Model) clearStatus() {
	m.err = ""
	m.message = ""
}

func (m *Model) nextFocus() {
	switch m.activeTab {
	case subTabRetention:
		m.retFocus = (m.retFocus + 1) % 2
		m.blurRetention()
		m.focusRetention()
	case subTabScheduler:
		m.schedFocus = (m.schedFocus + 1) % 4
		m.blurScheduler()
		m.focusScheduler()
	}
}

func (m *Model) prevFocus() {
	switch m.activeTab {
	case subTabRetention:
		m.retFocus = (m.retFocus - 1 + 2) % 2
		m.blurRetention()
		m.focusRetention()
	case subTabScheduler:
		m.schedFocus = (m.schedFocus - 1 + 4) % 4
		m.blurScheduler()
		m.focusScheduler()
	}
}

func (m *Model) blurRetention() { m.retDays.Blur() }
func (m *Model) focusRetention() {
	if m.retFocus == 0 {
		m.retDays.Focus()
	}
}

func (m *Model) blurScheduler() {
	m.cronExpr0.Blur()
	m.cronExpr1.Blur()
	m.timezone.Blur()
}
func (m *Model) focusScheduler() {
	switch m.schedFocus {
	case 0:
		m.cronExpr0.Focus()
	case 1:
		m.cronExpr1.Focus()
	case 2:
		m.timezone.Focus()
	}
}

func (m *Model) updateField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activeTab {
	case subTabRetention:
		if m.retFocus == 0 {
			m.retDays, cmd = m.retDays.Update(msg)
		}
	case subTabScheduler:
		switch m.schedFocus {
		case 0:
			m.cronExpr0, cmd = m.cronExpr0.Update(msg)
		case 1:
			m.cronExpr1, cmd = m.cronExpr1.Update(msg)
		case 2:
			m.timezone, cmd = m.timezone.Update(msg)
		}
	}
	return m, cmd
}

func (m *Model) saveCmd() tea.Cmd {
	switch m.activeTab {
	case subTabRetention:
		var days int
		fmt.Sscanf(m.retDays.Value(), "%d", &days)
		if days < 1 {
			days = 7
		}
		pol := &entities.RetentionPolicy{RetainDays: days}
		return func() tea.Msg {
			err := m.retentionUC.Save(m.ctx, pol)
			return savedMsg{err: err}
		}
	case subTabScheduler:
		var full, log []string
		if v := m.cronExpr0.Value(); v != "" {
			full = append(full, v)
		}
		if v := m.cronExpr1.Value(); v != "" {
			log = append(log, v)
		}
		tz := m.timezone.Value()
		if tz == "" {
			tz = "UTC"
		}
		cfg := &entities.SchedulerConfig{
			Enabled:       true,
			CronExprsFull: full,
			CronExprsLog:  log,
			TimeZone:      tz,
		}
		return func() tea.Msg {
			if err := m.schedulerUC.Save(m.ctx, cfg); err != nil {
				return savedMsg{err: err}
			}
			err := m.sched.Reload(m.ctx)
			return savedMsg{err: err}
		}
	}
	return nil
}

func (m *Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		pols, err := m.retentionUC.FindAll(m.ctx)
		cfg, cfgErr := m.schedulerUC.Get(m.ctx)
		if err == nil {
			err = cfgErr
		}
		return loadedMsg{policies: pols, cfg: cfg, err: err}
	}
}

func (m *Model) lcLoadCmd() tea.Cmd {
	return func() tea.Msg {
		if m.lifecycleUC == nil {
			return lcLoadedMsg{err: fmt.Errorf("S3 no configurado")}
		}
		rules, err := m.lifecycleUC.GetRules(m.ctx)
		return lcLoadedMsg{rules: rules, err: err}
	}
}

// ---- view ----

func (m *Model) View() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Retención, Programación y Ciclo de vida S3") + "\n\n")

	tab1 := styles.StyleTabInactive.Render(" [1] Retención ")
	tab2 := styles.StyleTabInactive.Render(" [2] Programador ")
	tab3 := styles.StyleTabInactive.Render(" [3] Lifecycle S3 ")
	switch m.activeTab {
	case subTabRetention:
		tab1 = styles.StyleTabActive.Render(" [1] Retención ")
	case subTabScheduler:
		tab2 = styles.StyleTabActive.Render(" [2] Programador ")
	case subTabLifecycle:
		tab3 = styles.StyleTabActive.Render(" [3] Lifecycle S3 ")
	}
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tab1, tab2, tab3))
	sb.WriteString("\n\n")

	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	}
	if m.message != "" {
		sb.WriteString(styles.StyleSuccess.Render("✓ "+m.message) + "\n\n")
	}

	switch m.activeTab {
	case subTabRetention:
		sb.WriteString(m.viewRetention())
	case subTabScheduler:
		sb.WriteString(m.viewScheduler())
	case subTabLifecycle:
		sb.WriteString(m.viewLifecycle())
	}

	return sb.String()
}

func (m *Model) viewRetention() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleLabel.Render("Días de retención global: "))
	sb.WriteString(m.retDays.View() + "\n")
	sb.WriteString(styles.StyleMuted.Render("  Los respaldos son marcados para expiración en S3 transcurridos N días.") + "\n\n")

	if len(m.policies) > 0 {
		sb.WriteString(styles.StyleValue.Render("Políticas configuradas:") + "\n")
		for _, p := range m.policies {
			instLabel := "Global"
			if p.InstanceID != "" {
				instLabel = p.InstanceID
			}
			sb.WriteString(fmt.Sprintf("  %-20s  %d días\n", instLabel, p.RetainDays))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(styles.StyleButton.Render("[ Guardar (enter) ]"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.StyleHelp.Render("tab navegar   enter guardar   1/2/3 sección"))
	return sb.String()
}

func (m *Model) viewScheduler() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleMuted.Render("Formato cron con segundos: segundo minuto hora día_mes mes día_semana") + "\n\n")
	sb.WriteString(styles.StyleLabel.Render("Full  (diario, ej. medianoche):     "))
	sb.WriteString(m.cronExpr0.View() + "\n")
	sb.WriteString(styles.StyleLabel.Render("Log   (horario, solo SQL Server):   "))
	sb.WriteString(m.cronExpr1.View() + "\n")
	sb.WriteString(styles.StyleLabel.Render("Zona horaria:       "))
	sb.WriteString(m.timezone.View() + "\n\n")

	nextRuns := m.sched.NextRunTimes()
	if len(nextRuns) > 0 {
		sb.WriteString(styles.StyleAccent.Render("Próximas ejecuciones:") + "\n")
		for _, t := range nextRuns {
			sb.WriteString(fmt.Sprintf("  %s\n", t.Local().Format("Mon 02 Jan 2006 15:04:05 MST")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(styles.StyleButton.Render("[ Guardar y Recargar Scheduler (enter) ]"))
	sb.WriteString("\n\n")
	sb.WriteString(styles.StyleHelp.Render("tab navegar   enter guardar   1/2/3 sección"))
	return sb.String()
}

func (m *Model) viewLifecycle() string {
	var sb strings.Builder

	if m.lcConfirm {
		return styles.StyleModalOverlay.Render(strings.Join([]string{
			styles.StyleDanger.Render("Confirmar eliminación de regla de ciclo de vida"),
			"",
			"  Esta acción elimina la regla del bucket S3.",
			"",
			styles.StyleWarning.Render("  [S] Confirmar     [N / esc] Cancelar"),
		}, "\n"))
	}

	sb.WriteString(styles.StyleMuted.Render(
		"Reglas de ciclo de vida configuradas en el bucket S3.") + "\n")
	sb.WriteString(styles.StyleMuted.Render(
		"Las reglas backiie-* se crean y gestionan desde aquí.") + "\n\n")

	if m.lcLoading {
		sb.WriteString(m.lcSpinner.View() + "  " +
			styles.StyleMuted.Render("Consultando bucket...") + "\n\n")
	}

	if len(m.lcRules) == 0 && !m.lcLoading {
		sb.WriteString(styles.StyleMuted.Render("No hay reglas configuradas en el bucket.") + "\n")
		sb.WriteString(styles.StyleMuted.Render("Presiona [s] para sincronizar con las políticas de retención.") + "\n\n")
	}

	if len(m.lcRules) > 0 {
		header := fmt.Sprintf("  %-3s  %-28s  %-35s  %6s  %-8s  %s",
			"", "ID", "Prefijo S3", "Días", "Estado", "Origen")
		sb.WriteString(styles.StyleTableHeader.Render(header) + "\n")

		for i, r := range m.lcRules {
			origin := ""
			if r.ManagedBy == "backiie" {
				origin = styles.StyleSuccess.Render("BackiieTUI")
			} else {
				origin = styles.StyleMuted.Render("externo")
			}
			prefix := r.Prefix
			if prefix == "" {
				prefix = styles.StyleMuted.Render("(todo el bucket)")
			}
			row := fmt.Sprintf("  %-3d  %-28s  %-35s  %6d  %-8s  %s",
				i+1,
				trunc(r.ID, 28),
				trunc(prefix, 35),
				r.ExpiryDays,
				r.Status,
				origin,
			)
			if i == m.lcSelected {
				sb.WriteString(styles.StyleTableRowSelected.Render(row) + "\n")
			} else {
				sb.WriteString(styles.StyleTableRow.Render(row) + "\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(styles.StyleHelp.Render(
		"s sincronizar con retención   d eliminar seleccionada   r refrescar   ↑/↓ navegar   1/2/3 sección"))
	return sb.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// lifecycleErrHint adds a Hetzner-specific hint when lifecycle is not available on the bucket.
func lifecycleErrHint(err error) string {
	msg := err.Error()
	hint := ""
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not implemented") ||
		strings.Contains(lower, "notimplemented") ||
		strings.Contains(lower, "methodnotallowed") ||
		strings.Contains(lower, "403") ||
		strings.Contains(lower, "access denied") {
		hint = "\n  → El bucket puede haber sido creado sin lifecycle policies." +
			"\n  → En Hetzner: elimina y vuelve a crear el bucket activando la opción de lifecycle."
	}
	return msg + hint
}
