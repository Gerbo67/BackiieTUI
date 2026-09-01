package backups

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/internal/timeutil"
	"BackiieTUI/tui/styles"
)

type viewMode int

const (
	modeList viewMode = iota
	modeDetail
	modeConfirmDelete
	modeRestore
	modeRestoring
	modeRestoreAll
	modeRestoringAll
	modeHistory
)

// confirmWord is the exact word the user must type to authorize a destructive/restore action.
const confirmWord = usecases.ConfirmPhrase

// Model manages the backups tab.
type Model struct {
	ctx       context.Context
	backupQUC *usecases.BackupQueryUseCase
	restoreUC *usecases.RestoreUseCase
	mode      viewMode
	records   []*entities.BackupRecord
	selected  int
	err       string
	message   string

	// Loading state
	isLoading bool
	spinner   spinner.Model

	// Pending delete
	deleteID     string
	confirmInput textinput.Model

	// Restore form
	restoreBackupID  string
	restoreInstInput textinput.Model
	restoreDBInput   textinput.Model
	restoreConfirm   textinput.Model
	restoreFocused   int // 0 = instance, 1 = database, 2 = confirmar
	instances        []*entities.Instance

	// Restore-all
	restoreAllRows []usecases.RestoreAllRow

	// History
	history []*entities.RestoreRecord
}

func NewModel(ctx context.Context, backupQUC *usecases.BackupQueryUseCase, restoreUC *usecases.RestoreUseCase) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	instIn := textinput.New()
	instIn.Placeholder = "nombre de la instancia destino"

	dbIn := textinput.New()
	dbIn.Placeholder = "nombre de la base de datos destino"

	restoreConfirm := textinput.New()
	restoreConfirm.Placeholder = confirmWord

	confirmIn := textinput.New()
	confirmIn.Placeholder = confirmWord

	return &Model{
		ctx:              ctx,
		backupQUC:        backupQUC,
		restoreUC:        restoreUC,
		spinner:          sp,
		restoreInstInput: instIn,
		restoreDBInput:   dbIn,
		restoreConfirm:   restoreConfirm,
		confirmInput:     confirmIn,
	}
}

// ---- public API ----

func (m *Model) Init() tea.Cmd {
	return m.Refresh()
}

// Refresh triggers a data reload. Called by the root model on tab activation.
func (m *Model) Refresh() tea.Cmd {
	m.isLoading = true
	return tea.Batch(m.spinner.Tick, m.loadCmd())
}

// ---- internal messages ----

type loadedMsg struct {
	records   []*entities.BackupRecord
	instances []*entities.Instance
	err       error
}

type deleteMsg struct{ err error }
type restoreDoneMsg struct{ err error }
type restoreAllPreviewMsg struct {
	rows []usecases.RestoreAllRow
	err  error
}
type restoreAllDoneMsg struct{ err error }
type historyLoadedMsg struct {
	records []*entities.RestoreRecord
	err     error
}

// ---- update ----

var listKeys = struct{ Up, Down, Detail, Delete, Restore, RestoreAll, History, Refresh key.Binding }{
	Up:         key.NewBinding(key.WithKeys("up", "k")),
	Down:       key.NewBinding(key.WithKeys("down", "j")),
	Detail:     key.NewBinding(key.WithKeys("enter", "v")),
	Delete:     key.NewBinding(key.WithKeys("d", "delete")),
	Restore:    key.NewBinding(key.WithKeys("R")),
	RestoreAll: key.NewBinding(key.WithKeys("A")),
	History:    key.NewBinding(key.WithKeys("H")),
	Refresh:    key.NewBinding(key.WithKeys("r", "f5")),
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case loadedMsg:
		m.isLoading = false
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.records = msg.records
			m.instances = msg.instances
			if m.selected >= len(m.records) && len(m.records) > 0 {
				m.selected = len(m.records) - 1
			}
		}
		return m, nil

	case deleteMsg:
		m.isLoading = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.message = "Respaldo eliminado correctamente."
			m.mode = modeList
		}
		return m, m.Refresh()

	case restoreDoneMsg:
		m.isLoading = false
		m.mode = modeList
		if msg.err != nil {
			m.err = "Error al restaurar: " + msg.err.Error()
		} else {
			m.message = "Restauración completada correctamente."
		}
		return m, nil

	case restoreAllPreviewMsg:
		m.isLoading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.mode = modeList
		} else {
			m.restoreAllRows = msg.rows
			m.confirmInput.SetValue("")
			m.confirmInput.Focus()
		}
		return m, nil

	case restoreAllDoneMsg:
		m.isLoading = false
		m.mode = modeList
		if msg.err != nil {
			m.err = "Error al restaurar todas las BD: " + msg.err.Error()
		} else {
			m.message = "Restauración de todas las bases de datos completada."
		}
		return m, m.Refresh()

	case historyLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.history = msg.records
		}
		return m, nil

	case spinner.TickMsg:
		if m.isLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeDetail:
			return m.updateDetail(msg)
		case modeConfirmDelete:
			return m.updateConfirmDelete(msg)
		case modeRestore:
			return m.updateRestore(msg)
		case modeRestoreAll:
			return m.updateRestoreAll(msg)
		case modeHistory:
			return m.updateHistory(msg)
		}
	}
	return m, nil
}

func (m *Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, listKeys.Up):
		if m.selected > 0 {
			m.selected--
		}
	case key.Matches(msg, listKeys.Down):
		if m.selected < len(m.records)-1 {
			m.selected++
		}
	case key.Matches(msg, listKeys.Detail):
		if len(m.records) > 0 {
			m.mode = modeDetail
		}
	case key.Matches(msg, listKeys.Delete):
		if len(m.records) > 0 {
			m.startDelete(m.records[m.selected].ID)
		}
	case key.Matches(msg, listKeys.Restore):
		if len(m.records) > 0 {
			m.startRestore(m.records[m.selected])
		}
	case key.Matches(msg, listKeys.RestoreAll):
		return m, m.startRestoreAll()
	case key.Matches(msg, listKeys.History):
		m.mode = modeHistory
		m.isLoading = true
		return m, tea.Batch(m.spinner.Tick, m.historyCmd())
	case key.Matches(msg, listKeys.Refresh):
		return m, m.Refresh()
	}
	return m, nil
}

func (m *Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v":
		m.mode = modeList
	case "d", "delete":
		if len(m.records) > 0 {
			m.startDelete(m.records[m.selected].ID)
		}
	case "R":
		if len(m.records) > 0 {
			m.startRestore(m.records[m.selected])
		}
	}
	return m, nil
}

func (m *Model) updateHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "H":
		m.mode = modeList
	case "r", "f5":
		m.isLoading = true
		return m, tea.Batch(m.spinner.Tick, m.historyCmd())
	}
	return m, nil
}

func (m *Model) updateRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "tab", "down":
		m.restoreFocused = (m.restoreFocused + 1) % 3
		m.focusRestoreField()
		return m, nil
	case "shift+tab", "up":
		m.restoreFocused = (m.restoreFocused + 2) % 3
		m.focusRestoreField()
		return m, nil
	case "enter":
		if m.restoreFocused < 2 {
			m.restoreFocused++
			m.focusRestoreField()
			return m, nil
		}
		// Campo de confirmación
		instName := strings.TrimSpace(m.restoreInstInput.Value())
		dbName := strings.TrimSpace(m.restoreDBInput.Value())
		if instName == "" || dbName == "" {
			m.err = "Instancia y nombre de BD son requeridos"
			return m, nil
		}
		if !strings.EqualFold(strings.TrimSpace(m.restoreConfirm.Value()), confirmWord) {
			m.err = fmt.Sprintf("Debes escribir %q para confirmar", confirmWord)
			return m, nil
		}
		m.isLoading = true
		m.mode = modeRestoring
		backupID := m.restoreBackupID
		return m, tea.Batch(m.spinner.Tick, m.doRestore(backupID, instName, dbName))
	}

	var cmd tea.Cmd
	switch m.restoreFocused {
	case 0:
		m.restoreInstInput, cmd = m.restoreInstInput.Update(msg)
	case 1:
		m.restoreDBInput, cmd = m.restoreDBInput.Update(msg)
	case 2:
		m.restoreConfirm, cmd = m.restoreConfirm.Update(msg)
	}
	return m, cmd
}

func (m *Model) focusRestoreField() {
	m.restoreInstInput.Blur()
	m.restoreDBInput.Blur()
	m.restoreConfirm.Blur()
	switch m.restoreFocused {
	case 0:
		m.restoreInstInput.Focus()
	case 1:
		m.restoreDBInput.Focus()
	case 2:
		m.restoreConfirm.Focus()
	}
}

func (m *Model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.deleteID = ""
		return m, nil
	case "enter":
		if !strings.EqualFold(strings.TrimSpace(m.confirmInput.Value()), confirmWord) {
			m.err = fmt.Sprintf("Debes escribir %q para confirmar", confirmWord)
			return m, nil
		}
		id := m.deleteID
		m.isLoading = true
		m.err = ""
		return m, tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				err := m.backupQUC.DeleteBackup(m.ctx, id)
				return deleteMsg{err: err}
			},
		)
	}
	var cmd tea.Cmd
	m.confirmInput, cmd = m.confirmInput.Update(msg)
	return m, cmd
}

func (m *Model) updateRestoreAll(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "enter":
		if m.isLoading {
			return m, nil // todavía cargando la vista previa
		}
		if !strings.EqualFold(strings.TrimSpace(m.confirmInput.Value()), confirmWord) {
			m.err = fmt.Sprintf("Debes escribir %q para confirmar", confirmWord)
			return m, nil
		}
		m.isLoading = true
		m.mode = modeRestoringAll
		return m, tea.Batch(m.spinner.Tick, m.doRestoreAll())
	}
	if m.isLoading {
		return m, nil
	}
	var cmd tea.Cmd
	m.confirmInput, cmd = m.confirmInput.Update(msg)
	return m, cmd
}

func (m *Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		list, err := m.backupQUC.FindAll(m.ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		// Newest first
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
		var instances []*entities.Instance
		if m.restoreUC != nil {
			instances, _ = m.restoreUC.FindAll(m.ctx)
		}
		return loadedMsg{records: list, instances: instances}
	}
}

func (m *Model) historyCmd() tea.Cmd {
	return func() tea.Msg {
		list, err := m.restoreUC.FindHistory(m.ctx)
		return historyLoadedMsg{records: list, err: err}
	}
}

func (m *Model) startDelete(id string) {
	m.deleteID = id
	m.confirmInput.SetValue("")
	m.confirmInput.Focus()
	m.err = ""
	m.mode = modeConfirmDelete
}

func (m *Model) startRestore(r *entities.BackupRecord) {
	m.restoreBackupID = r.ID
	m.restoreInstInput.SetValue(r.InstanceName)
	m.restoreDBInput.SetValue(r.DatabaseName)
	m.restoreConfirm.SetValue("")
	m.restoreFocused = 0
	m.err = ""
	m.mode = modeRestore
	m.focusRestoreField()
}

func (m *Model) startRestoreAll() tea.Cmd {
	m.mode = modeRestoreAll
	m.err = ""
	m.message = ""
	m.restoreAllRows = nil
	m.isLoading = true
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		rows, err := m.restoreUC.PreviewRestoreAll(m.ctx)
		return restoreAllPreviewMsg{rows: rows, err: err}
	})
}

func (m *Model) doRestore(backupID, instName, dbName string) tea.Cmd {
	return func() tea.Msg {
		err := m.restoreUC.RestoreBackup(m.ctx, backupID, instName, dbName)
		return restoreDoneMsg{err: err}
	}
}

func (m *Model) doRestoreAll() tea.Cmd {
	confirm := m.confirmInput.Value()
	return func() tea.Msg {
		err := m.restoreUC.RestoreAll(m.ctx, confirm)
		return restoreAllDoneMsg{err: err}
	}
}

// ---- view ----

func (m *Model) View() string {
	switch m.mode {
	case modeDetail:
		if len(m.records) > 0 {
			return m.viewDetail(m.records[m.selected])
		}
	case modeConfirmDelete:
		return m.viewConfirmDelete()
	case modeRestore:
		if len(m.records) > 0 {
			return m.viewRestore(m.records[m.selected])
		}
	case modeRestoring:
		return m.viewRestoring("Restaurando base de datos...")
	case modeRestoreAll:
		return m.viewRestoreAll()
	case modeRestoringAll:
		return m.viewRestoring("Restaurando todas las bases de datos...")
	case modeHistory:
		return m.viewHistory()
	}
	return m.viewList()
}

func (m *Model) viewList() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Registro de Respaldos") + "\n\n")

	// Status bar
	switch {
	case m.isLoading && len(m.records) == 0:
		sb.WriteString(m.spinner.View() + "  " + styles.StyleMuted.Render("Recuperando registros de respaldo...") + "\n\n")
	case m.isLoading:
		sb.WriteString(m.spinner.View() + "  " + styles.StyleMuted.Render("Actualizando...") + "\n\n")
	case m.err != "":
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	case m.message != "":
		sb.WriteString(styles.StyleSuccess.Render("✓ "+m.message) + "\n\n")
	}

	if len(m.records) == 0 && !m.isLoading {
		sb.WriteString(styles.StyleMuted.Render("No se encontraron respaldos registrados.") + "\n")
		sb.WriteString(styles.StyleMuted.Render("Configure instancias e inicie un respaldo con [b].") + "\n")
	} else if len(m.records) > 0 {
		header := fmt.Sprintf("  %-3s  %-4s  %-5s  %-16s  %-14s  %-12s  %-10s  %-17s  %s",
			"#", "", "Tipo", "Instancia", "BD", "Motor", "Tamaño", "Fecha", "Dur.")
		sb.WriteString(styles.StyleTableHeader.Render(header) + "\n")

		for i, r := range m.records {
			icon := styles.StatusStyle(r.Status.String()).Render(styles.StatusIcon(r.Status.String()))
			sizeStr := "-"
			if r.SizeBytes > 0 {
				sizeStr = timeutil.FormatSize(r.SizeBytes)
			}
			durStr := "-"
			if r.DurationMs > 0 {
				durStr = timeutil.FormatDuration(r.DurationMs)
			}
			typeStr := r.Type.String()
			row := fmt.Sprintf("  %-3d  %-4s  %-5s  %-16s  %-14s  %-12s  %-10s  %-17s  %s",
				i+1,
				icon,
				typeStr,
				trunc(r.InstanceName, 16),
				trunc(r.DatabaseName, 14),
				trunc(r.Engine.String(), 12),
				sizeStr,
				r.StartedAt.Format("02 Jan 15:04"),
				durStr,
			)
			if i == m.selected {
				sb.WriteString(styles.StyleTableRowSelected.Render(row) + "\n")
			} else {
				sb.WriteString(styles.StyleTableRow.Render(row) + "\n")
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(styles.StyleHelp.Render(
		"enter/v detalles  R restaurar  A restaurar TODAS  H historial  d eliminar  r refrescar  ↑/↓ navegar"))
	return sb.String()
}

func (m *Model) viewDetail(r *entities.BackupRecord) string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Detalle del Respaldo") + "\n\n")

	row := func(label, value string) {
		sb.WriteString(styles.StyleLabel.Render(label) + "  " + styles.StyleValue.Render(value) + "\n")
	}

	statusStr := styles.StatusStyle(r.Status.String()).Render(
		styles.StatusIcon(r.Status.String()) + " " + r.Status.String())

	row("ID:", r.ID)
	row("Tipo:", r.Type.String())
	row("Instancia:", r.InstanceName)
	row("Base de datos:", r.DatabaseName)
	row("Motor:", r.Engine.String())
	row("Estado:", statusStr)
	row("Archivo S3:", r.FileName)
	row("Tamaño:", timeutil.FormatSize(r.SizeBytes))
	row("SHA-256:", r.HashSHA256)
	row("Iniciado:", r.StartedAt.Format("2006-01-02 15:04:05 UTC"))
	if r.CompletedAt != nil {
		row("Completado:", r.CompletedAt.Format("2006-01-02 15:04:05 UTC"))
	}
	row("Duración:", timeutil.FormatDuration(r.DurationMs))
	row("Expira:", r.ExpiresAt.Format("2006-01-02"))
	if r.RetainDays > 0 {
		row("Retención:", fmt.Sprintf("%d días", r.RetainDays))
	}
	if r.ParentBackupID != "" {
		row("Full padre:", r.ParentBackupID)
	}
	if r.ErrorMessage != "" {
		sb.WriteString(styles.StyleLabel.Render("Error:") + "  " +
			styles.StyleDanger.Render(r.ErrorMessage) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styles.StyleHelp.Render("esc/v volver  R restaurar  d eliminar este respaldo"))
	return styles.StyleCard.Render(sb.String())
}

func (m *Model) viewConfirmDelete() string {
	if m.deleteID == "" {
		return m.viewList()
	}
	var sb strings.Builder
	sb.WriteString(styles.StyleDanger.Render("Confirmar eliminación de respaldo") + "\n\n")
	sb.WriteString("  Se eliminará el objeto del bucket S3 y su registro de auditoría.\n")
	sb.WriteString("  Si es un respaldo Full, también se eliminarán sus Logs encadenados.\n\n")
	sb.WriteString(styles.StyleWarning.Render("  Esta operación es irreversible.") + "\n\n")
	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("  ✗ "+m.err) + "\n\n")
	}
	sb.WriteString(fmt.Sprintf("  Escribe %q para confirmar:\n  ", confirmWord))
	sb.WriteString(m.confirmInput.View() + "\n\n")
	sb.WriteString("  [enter] Confirmar     [esc] Cancelar")
	return styles.StyleModalOverlay.Render(sb.String())
}

func (m *Model) viewRestore(r *entities.BackupRecord) string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Restaurar Respaldo") + "\n\n")

	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	}

	// Source info
	sb.WriteString(styles.StyleLabel.Render("Origen:     ") + "  " +
		styles.StyleValue.Render(fmt.Sprintf("%s / %s", r.InstanceName, r.DatabaseName)) + "\n")
	sb.WriteString(styles.StyleLabel.Render("Tipo:       ") + "  " +
		styles.StyleMuted.Render(r.Type.String()) + "\n")
	sb.WriteString(styles.StyleLabel.Render("Motor:      ") + "  " +
		styles.StyleMuted.Render(r.Engine.String()) + "\n")
	sb.WriteString(styles.StyleLabel.Render("Fecha:      ") + "  " +
		styles.StyleMuted.Render(r.StartedAt.Format("02 Jan 2006 15:04 UTC")) + "\n")
	sb.WriteString(styles.StyleLabel.Render("Archivo S3: ") + "  " +
		styles.StyleMuted.Render(trunc(r.FileName, 60)) + "\n\n")
	if !r.IsFull() {
		sb.WriteString(styles.StyleMuted.Render("Se aplicará el Full previo y todos los Logs hasta este punto en orden.") + "\n\n")
	}

	// Restore target fields
	instLabel := styles.StyleLabel.Render("Instancia destino ")
	dbLabel := styles.StyleLabel.Render("BD destino        ")
	confirmLabel := styles.StyleLabel.Render(fmt.Sprintf("Escribe %q     ", confirmWord))
	switch m.restoreFocused {
	case 0:
		instLabel = styles.StyleLabelFocused.Render("Instancia destino ")
	case 1:
		dbLabel = styles.StyleLabelFocused.Render("BD destino        ")
	case 2:
		confirmLabel = styles.StyleLabelFocused.Render(fmt.Sprintf("Escribe %q     ", confirmWord))
	}
	sb.WriteString(instLabel + "  " + m.restoreInstInput.View() + "\n")
	sb.WriteString(dbLabel + "  " + m.restoreDBInput.View() + "\n")
	sb.WriteString(confirmLabel + "  " + m.restoreConfirm.View() + "\n\n")

	// Available instances hint
	if len(m.instances) > 0 {
		sb.WriteString(styles.StyleMuted.Render("Instancias disponibles: "))
		names := make([]string, len(m.instances))
		for i, inst := range m.instances {
			names[i] = inst.Name
		}
		sb.WriteString(styles.StyleMuted.Render(strings.Join(names, "  •  ")) + "\n\n")
	}

	sb.WriteString(styles.StyleWarning.Render("⚠  Si la BD destino existe, sus datos serán reemplazados.") + "\n\n")

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Center,
		styles.StyleButton.Render("[ enter  Siguiente / Restaurar ]"),
		"   ",
		styles.StyleButtonSecondary.Render("[ esc  Cancelar ]"),
	))
	sb.WriteString("\n\n")
	sb.WriteString(styles.StyleHelp.Render("tab/↓ siguiente campo   shift+tab/↑ anterior   enter en confirmación = ejecutar"))
	return styles.StyleCard.Render(sb.String())
}

func (m *Model) viewRestoreAll() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleDanger.Render("Restaurar TODAS las bases de datos") + "\n\n")
	sb.WriteString("  Se restaurará, en el mismo lugar, el último punto de recuperación\n")
	sb.WriteString("  disponible (Full + Logs encadenados) de cada base de datos.\n")
	sb.WriteString(styles.StyleMuted.Render("  master queda excluida — requiere un procedimiento manual (ver docs/operaciones.md).") + "\n\n")

	if m.isLoading && len(m.restoreAllRows) == 0 {
		sb.WriteString(m.spinner.View() + "  " + styles.StyleMuted.Render("Calculando puntos de recuperación...") + "\n\n")
		return styles.StyleModalOverlay.Render(sb.String())
	}

	if len(m.restoreAllRows) == 0 {
		sb.WriteString(styles.StyleMuted.Render("No hay bases de datos con un respaldo Full completado.") + "\n\n")
		sb.WriteString(styles.StyleHelp.Render("[esc] Volver"))
		return styles.StyleModalOverlay.Render(sb.String())
	}

	header := fmt.Sprintf("  %-16s  %-18s  %-6s  %s", "Instancia", "BD", "Tipo", "Punto de recuperación")
	sb.WriteString(styles.StyleTableHeader.Render(header) + "\n")
	for _, row := range m.restoreAllRows {
		typeStr := "Full"
		if row.IsLog {
			typeStr = "Log"
		}
		sb.WriteString(fmt.Sprintf("  %-16s  %-18s  %-6s  %s\n",
			trunc(row.InstanceName, 16), trunc(row.DatabaseName, 18), typeStr,
			row.At.Format("02 Jan 2006 15:04 UTC")))
	}
	sb.WriteString("\n")

	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	}

	sb.WriteString(styles.StyleWarning.Render(fmt.Sprintf("⚠  Esto reemplaza los datos actuales de %d base(s) de datos.", len(m.restoreAllRows))) + "\n\n")
	sb.WriteString(fmt.Sprintf("  Escribe %q para confirmar:\n  ", confirmWord))
	sb.WriteString(m.confirmInput.View() + "\n\n")
	sb.WriteString(styles.StyleHelp.Render("[enter] Confirmar y restaurar todo   [esc] Cancelar"))
	return styles.StyleModalOverlay.Render(sb.String())
}

func (m *Model) viewHistory() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Historial de Restauraciones") + "\n\n")

	if m.isLoading {
		sb.WriteString(m.spinner.View() + "  " + styles.StyleMuted.Render("Cargando historial...") + "\n\n")
	} else if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	}

	if len(m.history) == 0 && !m.isLoading {
		sb.WriteString(styles.StyleMuted.Render("Todavía no se ha ejecutado ninguna restauración.") + "\n")
	} else if len(m.history) > 0 {
		header := fmt.Sprintf("  %-4s  %-16s  %-14s  %-8s  %-6s  %s",
			"", "Instancia", "BD", "Estado", "Pasos", "Fecha")
		sb.WriteString(styles.StyleTableHeader.Render(header) + "\n")
		for _, r := range m.history {
			icon := styles.StatusStyle(r.Status.String()).Render(styles.StatusIcon(r.Status.String()))
			sb.WriteString(fmt.Sprintf("  %-4s  %-16s  %-14s  %-8s  %-6d  %s\n",
				icon, trunc(r.InstanceName, 16), trunc(r.DatabaseName, 14),
				r.Status.String(), len(r.ChainBackupIDs), r.StartedAt.Format("02 Jan 15:04")))
			if r.ErrorMessage != "" {
				sb.WriteString("      " + styles.StyleDanger.Render(trunc(r.ErrorMessage, 90)) + "\n")
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(styles.StyleHelp.Render("esc/H volver   r refrescar"))
	return sb.String()
}

func (m *Model) viewRestoring(label string) string {
	return styles.StyleCard.Render(
		m.spinner.View() + "  " + label + "\n\n" +
			styles.StyleMuted.Render("Este proceso puede tardar varios minutos dependiendo del tamaño del backup."),
	)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
