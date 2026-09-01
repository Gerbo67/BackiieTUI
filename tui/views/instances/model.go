package instances

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/tui/styles"
)

type viewMode int

const (
	modeList viewMode = iota
	modeCreate
	modeEdit
	modeConfirmDelete
	modeConfirmBackup
	modeTesting
)

// Model manages the instances tab (list + CRUD + test connection).
type Model struct {
	ctx         context.Context
	instanceUC  *usecases.InstanceUseCase
	backupUC    *usecases.RunBackupUseCase
	mode        viewMode
	instances   []*entities.Instance
	selectedIdx int
	err         string
	message     string

	// Loading state
	isLoading bool
	spinner   spinner.Model

	// Form
	formFields   []textinput.Model
	formFocused  int
	editingID    string
	confirmInput textinput.Model

	// Test connection result
	testResult string
	testErr    string
}

const (
	fName     = 0
	fEngine   = 1
	fHost     = 2
	fPort     = 3
	fUsername = 4
	fPassword = 5
	fDatabase = 6
	fExclude  = 7
	fCount    = 8
)

var formLabels = [fCount]string{
	"Nombre        ",
	"Motor         ",
	"Host          ",
	"Puerto        ",
	"Usuario       ",
	"Contraseña    ",
	"BD default    ",
	"Excluir BDs   ",
}

var formHints = [fCount]string{
	"",
	"sqlserver | mysql | mariadb | postgres | redis",
	"",
	"auto-completado al cambiar motor",
	"",
	"",
	"esquema/instancia por defecto",
	"BDs que NO se respaldan, separadas por coma",
}

func NewModel(ctx context.Context, instanceUC *usecases.InstanceUseCase, backupUC *usecases.RunBackupUseCase) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	cIn := textinput.New()
	cIn.Placeholder = usecases.ConfirmPhrase
	return &Model{ctx: ctx, instanceUC: instanceUC, backupUC: backupUC, spinner: sp, confirmInput: cIn}
}

// ---- public API ----

func (m *Model) Init() tea.Cmd {
	return m.Refresh()
}

// Refresh triggers a data reload with a visible loading state.
// Called by the root model whenever this tab is activated.
func (m *Model) Refresh() tea.Cmd {
	m.isLoading = true
	return tea.Batch(m.spinner.Tick, m.loadCmd())
}

// ---- internal messages ----

type loadedMsg struct {
	instances []*entities.Instance
	err       error
}

type testResultMsg struct {
	latency time.Duration
	err     error
}

type backupFinishedMsg struct{ err error }

// ---- update ----

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case loadedMsg:
		m.isLoading = false
		m.err = ""
		m.message = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.instances = msg.instances
			// Keep selected index in bounds
			if m.selectedIdx >= len(m.instances) && len(m.instances) > 0 {
				m.selectedIdx = len(m.instances) - 1
			}
		}
		return m, nil

	case testResultMsg:
		m.mode = modeList
		if msg.err != nil {
			m.testErr = msg.err.Error()
			m.testResult = ""
		} else {
			m.testResult = fmt.Sprintf("OK — %dms", msg.latency.Milliseconds())
			m.testErr = ""
		}
		return m, nil

	case backupFinishedMsg:
		if msg.err != nil {
			m.err = "Error durante el respaldo: " + msg.err.Error()
		} else {
			m.message = "Ciclo de respaldo finalizado correctamente."
		}
		return m, nil

	case spinner.TickMsg:
		// Keep spinner alive while loading or testing
		if m.isLoading || m.mode == modeTesting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeCreate, modeEdit:
			return m.updateForm(msg)
		case modeConfirmDelete:
			return m.updateConfirm(msg)
		case modeConfirmBackup:
			return m.updateConfirmBackup(msg)
		}
	}
	return m, nil
}

// ---- key handling ----

var listKeys = struct{ Up, Down, New, Edit, Delete, Test, Backup, Refresh key.Binding }{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	New:     key.NewBinding(key.WithKeys("n")),
	Edit:    key.NewBinding(key.WithKeys("e")),
	Delete:  key.NewBinding(key.WithKeys("d", "delete")),
	Test:    key.NewBinding(key.WithKeys("t")),
	Backup:  key.NewBinding(key.WithKeys("b")),
	Refresh: key.NewBinding(key.WithKeys("r", "f5")),
}

func (m *Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, listKeys.Up):
		if m.selectedIdx > 0 {
			m.selectedIdx--
		}
	case key.Matches(msg, listKeys.Down):
		if m.selectedIdx < len(m.instances)-1 {
			m.selectedIdx++
		}
	case key.Matches(msg, listKeys.New):
		m.startForm(nil)
	case key.Matches(msg, listKeys.Edit):
		if len(m.instances) > 0 {
			m.startForm(m.instances[m.selectedIdx])
		}
	case key.Matches(msg, listKeys.Delete):
		if len(m.instances) > 0 {
			m.mode = modeConfirmDelete
			m.err = ""
			m.message = ""
		}
	case key.Matches(msg, listKeys.Test):
		if len(m.instances) > 0 {
			inst := m.instances[m.selectedIdx]
			m.mode = modeTesting
			m.testResult = ""
			m.testErr = ""
			return m, tea.Batch(m.spinner.Tick, m.doTest(inst))
		}
	case key.Matches(msg, listKeys.Backup):
		if len(m.instances) > 0 {
			m.mode = modeConfirmBackup
			m.err = ""
			m.message = ""
			m.confirmInput.SetValue("")
			m.confirmInput.Focus()
		}
	case key.Matches(msg, listKeys.Refresh):
		return m, m.Refresh()
	}
	return m, nil
}

func (m *Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "tab", "down":
		m.blurFocus()
		m.formFocused = (m.formFocused + 1) % fCount
		m.focusCurrent()
		return m, nil
	case "shift+tab", "up":
		m.blurFocus()
		m.formFocused = (m.formFocused - 1 + fCount) % fCount
		m.focusCurrent()
		return m, nil
	case "enter":
		if m.formFocused == fEngine {
			if eng, err := entities.ParseEngine(m.formFields[fEngine].Value()); err == nil {
				if m.formFields[fPort].Value() == "" {
					m.formFields[fPort].SetValue(fmt.Sprintf("%d", entities.DefaultPort(eng)))
				}
			}
		}
		if m.formFocused == fCount-1 {
			return m.submitForm()
		}
		m.blurFocus()
		m.formFocused = (m.formFocused + 1) % fCount
		m.focusCurrent()
		return m, nil
	default:
		var cmd tea.Cmd
		m.formFields[m.formFocused], cmd = m.formFields[m.formFocused].Update(msg)
		return m, cmd
	}
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "s", "S":
		if len(m.instances) == 0 {
			m.mode = modeList
			return m, nil
		}
		id := m.instances[m.selectedIdx].ID
		m.mode = modeList
		return m, func() tea.Msg {
			_ = m.instanceUC.Delete(m.ctx, id)
			list, err := m.instanceUC.FindAll(m.ctx)
			return loadedMsg{instances: list, err: err}
		}
	default:
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) updateConfirmBackup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "enter":
		if !strings.EqualFold(strings.TrimSpace(m.confirmInput.Value()), usecases.ConfirmPhrase) {
			m.err = fmt.Sprintf("Debes escribir %q para confirmar", usecases.ConfirmPhrase)
			return m, nil
		}
		id := m.instances[m.selectedIdx].ID
		m.mode = modeList
		m.err = ""
		m.message = ""
		return m, m.doBackup(id)
	}
	var cmd tea.Cmd
	m.confirmInput, cmd = m.confirmInput.Update(msg)
	return m, cmd
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	inst, err := m.parseForm()
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.mode = modeList
	m.err = ""
	editID := m.editingID
	return m, func() tea.Msg {
		if editID == "" {
			err = m.instanceUC.Create(m.ctx, inst)
		} else {
			inst.ID = editID
			err = m.instanceUC.Update(m.ctx, inst)
		}
		if err != nil {
			return loadedMsg{err: err}
		}
		list, e := m.instanceUC.FindAll(m.ctx)
		return loadedMsg{instances: list, err: e}
	}
}

// ---- helpers ----

func (m *Model) startForm(inst *entities.Instance) {
	m.formFields = buildFormFields()
	m.formFocused = 0
	m.formFields[0].Focus()
	m.err = ""
	m.message = ""
	if inst == nil {
		m.mode = modeCreate
		m.editingID = ""
	} else {
		m.mode = modeEdit
		m.editingID = inst.ID
		m.formFields[fName].SetValue(inst.Name)
		m.formFields[fEngine].SetValue(string(inst.Engine))
		m.formFields[fHost].SetValue(inst.Host)
		m.formFields[fPort].SetValue(fmt.Sprintf("%d", inst.Port))
		m.formFields[fUsername].SetValue(inst.Username)
		m.formFields[fPassword].SetValue(inst.Password)
		m.formFields[fDatabase].SetValue(inst.Database)
		m.formFields[fExclude].SetValue(strings.Join(inst.ExcludedDatabases, ", "))
	}
}

func (m *Model) blurFocus() {
	if m.formFocused < len(m.formFields) {
		m.formFields[m.formFocused].Blur()
	}
}

func (m *Model) focusCurrent() {
	if m.formFocused < len(m.formFields) {
		m.formFields[m.formFocused].Focus()
	}
}

func (m *Model) parseForm() (*entities.Instance, error) {
	engine, err := entities.ParseEngine(m.formFields[fEngine].Value())
	if err != nil {
		return nil, err
	}
	var port int
	fmt.Sscanf(m.formFields[fPort].Value(), "%d", &port)
	if port == 0 {
		port = entities.DefaultPort(engine)
	}

	var excluded []string
	for _, part := range strings.Split(m.formFields[fExclude].Value(), ",") {
		if s := strings.TrimSpace(part); s != "" {
			excluded = append(excluded, s)
		}
	}

	return &entities.Instance{
		Name:              m.formFields[fName].Value(),
		Engine:            engine,
		Host:              m.formFields[fHost].Value(),
		Port:              port,
		Username:          m.formFields[fUsername].Value(),
		Password:          m.formFields[fPassword].Value(),
		Database:          m.formFields[fDatabase].Value(),
		ExcludedDatabases: excluded,
		Enabled:           true,
	}, nil
}

func (m *Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		list, err := m.instanceUC.FindAll(m.ctx)
		return loadedMsg{instances: list, err: err}
	}
}

func (m *Model) doTest(inst *entities.Instance) tea.Cmd {
	instCopy := *inst
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		lat, err := m.instanceUC.TestConnection(ctx, &instCopy)
		return testResultMsg{latency: lat, err: err}
	}
}

func (m *Model) doBackup(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.backupUC.RunForInstance(m.ctx, id)
		return backupFinishedMsg{err: err}
	}
}

func buildFormFields() []textinput.Model {
	fields := make([]textinput.Model, fCount)
	for i := range fields {
		ti := textinput.New()
		ti.Placeholder = formLabels[i]
		if i == fPassword {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		fields[i] = ti
	}
	return fields
}

// ---- view ----

func (m *Model) View() string {
	switch m.mode {
	case modeCreate:
		return m.viewForm("Nueva Instancia")
	case modeEdit:
		return m.viewForm("Editar Instancia")
	case modeConfirmDelete:
		return m.viewConfirm()
	case modeConfirmBackup:
		return m.viewConfirmBackup()
	case modeTesting:
		return m.viewTesting()
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Instancias de Base de Datos") + "\n\n")

	// Inline status bar (loading / errors / messages)
	switch {
	case m.isLoading && len(m.instances) == 0:
		sb.WriteString(m.spinner.View() + "  " + styles.StyleMuted.Render("Recuperando instancias...") + "\n\n")
	case m.isLoading:
		sb.WriteString(m.spinner.View() + "  " + styles.StyleMuted.Render("Actualizando...") + "\n\n")
	case m.err != "":
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	case m.message != "":
		sb.WriteString(styles.StyleSuccess.Render("✓ "+m.message) + "\n\n")
	}

	// Connection test result (persists across reloads)
	if m.testResult != "" {
		sb.WriteString(styles.StyleSuccess.Render("✓ Verificación de conexión: "+m.testResult) + "\n\n")
	}
	if m.testErr != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ Verificación de conexión fallida: "+m.testErr) + "\n\n")
	}

	if len(m.instances) == 0 && !m.isLoading {
		sb.WriteString(styles.StyleMuted.Render("No hay instancias registradas.") + "\n")
		sb.WriteString(styles.StyleMuted.Render("Utilice [n] para crear una nueva instancia.") + "\n")
	} else if len(m.instances) > 0 {
		header := fmt.Sprintf("  %-3s  %-20s  %-12s  %-22s  %-6s  %-6s  %-15s",
			"#", "Nombre", "Motor", "Host", "Puerto", "Act.", "BD Default")
		sb.WriteString(styles.StyleTableHeader.Render(header) + "\n")

		for i, inst := range m.instances {
			statusStr := styles.StyleSuccess.Render("●")
			if !inst.Enabled {
				statusStr = styles.StyleMuted.Render("○")
			}
			row := fmt.Sprintf("  %-3d  %-20s  %-12s  %-22s  %-6d  %-6s  %-15s",
				i+1,
				trunc(inst.Name, 20),
				trunc(inst.Engine.String(), 12),
				trunc(inst.Host, 22),
				inst.Port,
				statusStr,
				trunc(inst.Database, 15),
			)
			if i == m.selectedIdx {
				sb.WriteString(styles.StyleTableRowSelected.Render(row) + "\n")
			} else {
				sb.WriteString(styles.StyleTableRow.Render(row) + "\n")
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(styles.StyleHelp.Render(
		"n nuevo  e editar  d eliminar  t probar conexión  b respaldar ahora  r refrescar  ↑/↓ navegar\n" +
			"  en editar: campo 'Excluir BDs' = BDs separadas por coma que se omiten del respaldo"))
	return sb.String()
}

func (m *Model) viewForm(title string) string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render(title) + "\n\n")

	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	}

	for i := 0; i < fCount; i++ {
		focused := i == m.formFocused
		lblStyle := styles.StyleLabel
		if focused {
			lblStyle = styles.StyleLabelFocused
		}
		line := lblStyle.Render(formLabels[i]) + "  " + m.formFields[i].View()
		if formHints[i] != "" {
			line += "  " + styles.StyleMuted.Render("// "+formHints[i])
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Center,
		styles.StyleButton.Render("[ Guardar (enter) ]"),
		"   ",
		styles.StyleButtonSecondary.Render("[ Cancelar (esc) ]"),
	))
	sb.WriteString("\n\n")
	sb.WriteString(styles.StyleHelp.Render(
		"tab/↓ siguiente   shift+tab/↑ anterior   enter en último campo = guardar"))
	return sb.String()
}

func (m *Model) viewConfirm() string {
	if len(m.instances) == 0 {
		return ""
	}
	inst := m.instances[m.selectedIdx]
	content := strings.Join([]string{
		styles.StyleDanger.Render("Confirmar eliminación de instancia"),
		"",
		fmt.Sprintf("  %s  —  %s @ %s:%d",
			styles.StyleValue.Render(inst.Name),
			styles.StyleMuted.Render(inst.Engine.String()),
			inst.Host, inst.Port),
		"",
		styles.StyleWarning.Render("  Esta operación es irreversible."),
		"",
		"  [S] Confirmar     [N / esc] Cancelar",
	}, "\n")
	return styles.StyleModalOverlay.Render(content)
}

func (m *Model) viewConfirmBackup() string {
	if len(m.instances) == 0 {
		return ""
	}
	inst := m.instances[m.selectedIdx]
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Confirmar Respaldo Manual") + "\n\n")
	sb.WriteString(fmt.Sprintf("  Se iniciará un respaldo para la instancia:\n  %s (%s)\n\n",
		styles.StyleValue.Render(inst.Name), styles.StyleMuted.Render(inst.Engine.String())))
	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("  ✗ "+m.err) + "\n\n")
	}
	sb.WriteString(fmt.Sprintf("  Escribe %q para confirmar:\n  ", usecases.ConfirmPhrase))
	sb.WriteString(m.confirmInput.View() + "\n\n")
	sb.WriteString("  [enter] Confirmar     [esc] Cancelar")
	return styles.StyleModalOverlay.Render(sb.String())
}

func (m *Model) viewTesting() string {
	return styles.StyleCard.Render(
		m.spinner.View() + "  Verificando conexión...\n\n" +
			styles.StyleMuted.Render("Estableciendo comunicación con el servidor (tiempo límite: 10 s)."),
	)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
