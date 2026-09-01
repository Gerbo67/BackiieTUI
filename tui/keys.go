package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all global keybindings.
type KeyMap struct {
	TabNext key.Binding
	TabPrev key.Binding
	New     key.Binding
	Edit    key.Binding
	Delete  key.Binding
	Confirm key.Binding
	Cancel  key.Binding
	Refresh key.Binding
	Backup  key.Binding
	Up      key.Binding
	Down    key.Binding
	Quit    key.Binding
}

// GlobalKeys is the application-wide keybinding set.
var GlobalKeys = KeyMap{
	TabNext: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "siguiente pestaña")),
	TabPrev: key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "pestaña anterior")),
	New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "nuevo")),
	Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "editar")),
	Delete:  key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "eliminar")),
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirmar")),
	Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancelar")),
	Refresh: key.NewBinding(key.WithKeys("r", "f5"), key.WithHelp("r", "refrescar")),
	Backup:  key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "respaldar ahora")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "subir")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "bajar")),
	Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "salir")),
}
