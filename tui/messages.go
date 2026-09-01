package tui

import "time"

// NavigateMsg switches the active tab.
type NavigateMsg struct{ Tab int }

// BackupStartedMsg notifies the TUI a backup has begun.
type BackupStartedMsg struct {
	InstanceName string
	DBName       string
}

// BackupProgressMsg reports bytes written during an upload.
type BackupProgressMsg struct {
	InstanceName string
	DBName       string
	BytesWritten int64
}

// BackupCompletedMsg notifies the TUI a backup has finished successfully.
type BackupCompletedMsg struct {
	InstanceName string
	DBName       string
	BackupID     string
	At           time.Time
}

// BackupFailedMsg notifies the TUI a backup has failed.
type BackupFailedMsg struct {
	InstanceName string
	DBName       string
	Err          error
}

// RefreshMsg triggers data reload in the active view.
type RefreshMsg struct{}

// NotificationMsg is a transient status message shown in the top bar.
type NotificationMsg struct {
	Text    string
	IsError bool
}

// InstancesLoadedMsg carries a fresh list of instances to the view.
type InstancesLoadedMsg struct {
	Err error
}

// BackupsLoadedMsg carries a fresh list of backup records to the view.
type BackupsLoadedMsg struct {
	Err error
}

// ConnectionTestResultMsg carries the result of a connection test.
type ConnectionTestResultMsg struct {
	LatencyMs int64
	Err       error
}

// SchedulerReloadedMsg signals the scheduler was reloaded.
type SchedulerReloadedMsg struct{}
