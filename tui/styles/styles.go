package styles

import "github.com/charmbracelet/lipgloss"

var (
	ColorPrimary = lipgloss.Color("#7C3AED")
	ColorAccent  = lipgloss.Color("#06B6D4")
	ColorSuccess = lipgloss.Color("#10B981")
	ColorWarning = lipgloss.Color("#F59E0B")
	ColorDanger  = lipgloss.Color("#EF4444")
	ColorMuted   = lipgloss.Color("#6B7280")
	ColorBg      = lipgloss.Color("#1E1E2E")
	ColorSurface = lipgloss.Color("#2D2D3F")
	ColorText    = lipgloss.Color("#E2E8F0")
	ColorSubtext = lipgloss.Color("#94A3B8")
	ColorBorder  = lipgloss.Color("#4B5563")

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			PaddingBottom(1)

	StyleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBg).
			Background(ColorPrimary).
			Padding(0, 2)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColorSubtext).
				Background(ColorSurface).
				Padding(0, 2)

	StyleCard = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2).
			Background(ColorSurface)

	StyleCardFocused = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(1, 2).
				Background(ColorSurface)

	StyleLabel = lipgloss.NewStyle().
			Foreground(ColorSubtext).
			Width(20)

	StyleLabelFocused = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Width(20)

	StyleValue = lipgloss.NewStyle().
			Foreground(ColorText)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	StyleDanger = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleAccent = lipgloss.NewStyle().
			Foreground(ColorAccent)

	StyleHelp = lipgloss.NewStyle().
			Foreground(ColorMuted).
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(ColorBorder).
			PaddingTop(1)

	StyleButton = lipgloss.NewStyle().
			Foreground(ColorBg).
			Background(ColorPrimary).
			Padding(0, 2).
			Bold(true)

	StyleButtonSecondary = lipgloss.NewStyle().
				Foreground(ColorText).
				Background(ColorSurface).
				Padding(0, 2).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(ColorBorder)

	StyleModalOverlay = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Padding(1, 2).
				Background(ColorSurface)

	StyleTableHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorAccent).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(ColorBorder)

	StyleTableRow = lipgloss.NewStyle().
			Foreground(ColorText)

	StyleTableRowSelected = lipgloss.NewStyle().
				Foreground(ColorBg).
				Background(ColorPrimary)

	StyleStatusPending   = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
	StyleStatusRunning   = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleStatusCompleted = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	StyleStatusFailed    = lipgloss.NewStyle().Foreground(ColorDanger).Bold(true)
	StyleStatusExpired   = lipgloss.NewStyle().Foreground(ColorMuted).Bold(true)
)

// StatusStyle returns the appropriate lipgloss style for a backup status string.
func StatusStyle(status string) lipgloss.Style {
	switch status {
	case "Completado":
		return StyleStatusCompleted
	case "Fallido":
		return StyleStatusFailed
	case "Ejecutando":
		return StyleStatusRunning
	case "Pendiente":
		return StyleStatusPending
	default:
		return StyleStatusExpired
	}
}

// StatusIcon returns the icon character for a backup status string.
func StatusIcon(status string) string {
	switch status {
	case "Completado":
		return "✓"
	case "Fallido":
		return "✗"
	case "Ejecutando":
		return "⟳"
	case "Pendiente":
		return "○"
	default:
		return "—"
	}
}
