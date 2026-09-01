package s3config

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
	"BackiieTUI/tui/styles"
)

const (
	fBucket    = 0
	fRegion    = 1
	fEndpoint  = 2
	fAccessKey = 3
	fSecretKey = 4
	fPrefix    = 5
	fCount     = 6
)

var fLabels = [fCount]string{
	"Bucket         ",
	"Región         ",
	"Endpoint       ",
	"Access Key ID  ",
	"Secret Key     ",
	"Prefijo S3     ",
}

var fHints = [fCount]string{
	"nombre del bucket S3",
	"us-east-1 | eu-west-1 | ...",
	"vacío=AWS; set para MinIO/Ceph: http://minio:9000",
	"",
	"",
	"backups/ (opcional)",
}

type testState int

const (
	testIdle testState = iota
	testRunning
	testOK
	testFail
)

// Model manages the S3 configuration tab.
type Model struct {
	ctx        context.Context
	s3uc       *usecases.S3ConfigUseCase
	fields     []textinput.Model
	focused    int
	forceStyle bool
	err        string
	message    string
	testState  testState
	testMsg    string
	spinner    spinner.Model
}

func NewModel(ctx context.Context, s3uc *usecases.S3ConfigUseCase) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	fields := make([]textinput.Model, fCount)
	for i := range fields {
		ti := textinput.New()
		ti.Placeholder = fLabels[i]
		if i == fSecretKey {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		fields[i] = ti
	}
	return &Model{ctx: ctx, s3uc: s3uc, fields: fields, spinner: sp}
}

func (m *Model) Init() tea.Cmd {
	return m.Refresh()
}

// Refresh reloads config from persistence. Called by the root model on tab activation.
func (m *Model) Refresh() tea.Cmd {
	return m.loadCmd()
}

type loadedMsg struct {
	cfg *entities.S3Config
	err error
}

// SavedMsg is emitted when save finishes.
type SavedMsg struct{ Err error }

type testResultMsg struct {
	err error
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.err == nil && msg.cfg != nil {
			m.fields[fBucket].SetValue(msg.cfg.Bucket)
			m.fields[fRegion].SetValue(msg.cfg.Region)
			m.fields[fEndpoint].SetValue(msg.cfg.Endpoint)
			m.fields[fAccessKey].SetValue(msg.cfg.AccessKeyID)
			m.fields[fSecretKey].SetValue(msg.cfg.SecretAccessKey)
			m.fields[fPrefix].SetValue(msg.cfg.PathPrefix)
		}
		return m, nil

	case SavedMsg:
		if msg.Err != nil {
			m.err = msg.Err.Error()
			m.message = ""
		} else {
			m.message = "Configuración S3 guardada"
			m.err = ""
		}
		return m, nil

	case testResultMsg:
		m.testState = testFail
		if msg.err == nil {
			m.testState = testOK
			m.testMsg = "Conexión S3 exitosa"
		} else {
			m.testMsg = msg.err.Error()
		}
		return m, nil

	case spinner.TickMsg:
		if m.testState == testRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			m.blurFocus()
			m.focused = (m.focused + 1) % (fCount + 2) // +2 for Save and Test buttons
			m.focusCurrent()
			return m, nil
		case "shift+tab", "up":
			m.blurFocus()
			m.focused = (m.focused - 1 + fCount + 2) % (fCount + 2)
			m.focusCurrent()
			return m, nil
		case "enter":
			if m.focused == fCount {
				return m, m.saveCmd()
			}
			if m.focused == fCount+1 {
				return m, m.testCmd()
			}
			m.blurFocus()
			m.focused = (m.focused + 1) % (fCount + 2)
			m.focusCurrent()
			return m, nil
		case "left", "right":
			if m.focused >= fCount {
				m.blurFocus()
				if m.focused == fCount {
					m.focused = fCount + 1
				} else {
					m.focused = fCount
				}
				m.focusCurrent()
				return m, nil
			}
			fallthrough
		default:
			if m.focused < fCount {
				var cmd tea.Cmd
				m.fields[m.focused], cmd = m.fields[m.focused].Update(msg)
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m *Model) blurFocus() {
	if m.focused < fCount {
		m.fields[m.focused].Blur()
	}
}

func (m *Model) focusCurrent() {
	if m.focused < fCount {
		m.fields[m.focused].Focus()
	}
}

func (m *Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := m.s3uc.Get(m.ctx)
		return loadedMsg{cfg: cfg, err: err}
	}
}

func (m *Model) saveCmd() tea.Cmd {
	cfg := m.buildConfig()
	return func() tea.Msg {
		err := m.s3uc.Save(m.ctx, cfg)
		return SavedMsg{Err: err}
	}
}

func (m *Model) testCmd() tea.Cmd {
	m.testState = testRunning
	m.testMsg = ""
	cfg := m.buildConfig()
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
			defer cancel()
			err := m.s3uc.TestConnection(ctx, cfg)
			return testResultMsg{err: err}
		},
	)
}

func (m *Model) buildConfig() *entities.S3Config {
	return &entities.S3Config{
		Bucket:          m.fields[fBucket].Value(),
		Region:          m.fields[fRegion].Value(),
		Endpoint:        m.fields[fEndpoint].Value(),
		AccessKeyID:     m.fields[fAccessKey].Value(),
		SecretAccessKey: m.fields[fSecretKey].Value(),
		PathPrefix:      m.fields[fPrefix].Value(),
		ForcePathStyle:  m.fields[fEndpoint].Value() != "",
	}
}

func (m *Model) View() string {
	var sb strings.Builder
	sb.WriteString(styles.StyleTitle.Render("Configuración de Almacenamiento S3") + "\n")
	sb.WriteString(styles.StyleMuted.Render("Compatible con AWS S3, MinIO y Ceph Object Storage") + "\n\n")

	if m.err != "" {
		sb.WriteString(styles.StyleDanger.Render("✗ "+m.err) + "\n\n")
	}
	if m.message != "" {
		sb.WriteString(styles.StyleSuccess.Render("✓ "+m.message) + "\n\n")
	}

	for i := 0; i < fCount; i++ {
		focused := i == m.focused
		lbl := styles.StyleLabel.Render(fLabels[i])
		if focused {
			lbl = styles.StyleLabelFocused.Render(fLabels[i])
		}
		line := lbl + "  " + m.fields[i].View()
		if fHints[i] != "" {
			line += "  " + styles.StyleMuted.Render("// "+fHints[i])
		}
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n")

	saveStyle := styles.StyleButton
	testStyle := styles.StyleButtonSecondary
	if m.focused == fCount {
		saveStyle = styles.StyleButton.Copy().Background(styles.ColorAccent)
	}
	if m.focused == fCount+1 {
		testStyle = styles.StyleButton.Copy().Background(styles.ColorSuccess)
	}

	saveBtn := saveStyle.Render("[ Guardar ]")
	testBtn := testStyle.Render("[ Probar Conexión ]")

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, saveBtn, "   ", testBtn))
	sb.WriteString("\n\n")

	// Test connection status
	switch m.testState {
	case testRunning:
		sb.WriteString(m.spinner.View() + "  Verificando acceso al bucket S3...\n")
	case testOK:
		sb.WriteString(styles.StyleSuccess.Render("✓ "+m.testMsg) + "\n")
	case testFail:
		sb.WriteString(styles.StyleDanger.Render("✗ La verificación falló: "+m.testMsg) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styles.StyleHelp.Render(
		"tab/↓ siguiente   shift+tab/↑ anterior   enter en botón = ejecutar   ←/→ cambiar de botón"))

	return sb.String()
}
