// updater.go launches the operator-configured update command in a detached
// process so an in-app "update now" action survives the service restart the
// command itself triggers. The command is taken verbatim from config and never
// receives request-derived data — the only injection surface is the operator's
// own config file.
//
// IMPORTANT (deployment): under systemd's default KillMode=control-group, a
// `systemctl restart` of this unit kills every process in its cgroup, including
// a setsid child. The configured command MUST therefore detach from the unit's
// cgroup — e.g. wrap it with `systemd-run --scope` so the actual update runs in
// a transient unit. See config.yaml.example.
package update

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// Errors returned by Trigger, classified for handlers via errors.Is.
var (
	// ErrDisabled means no update_command is configured.
	ErrDisabled = errors.New("update: one-click update disabled (no update_command)")
	// ErrInProgress means an update was already launched and not yet finished.
	ErrInProgress = errors.New("update: an update is already in progress")
)

// Updater spawns the configured update command at most once at a time. The zero
// value is not usable — use NewUpdater.
type Updater struct {
	command string // operator-configured shell command; empty disables
	logPath string // detached process stdout/stderr sink
	logger  *slog.Logger

	mu      sync.Mutex
	running bool
}

// NewUpdater returns an Updater. An empty command disables one-click updates;
// Enabled() then reports false and Trigger() returns ErrDisabled.
func NewUpdater(command, logPath string, logger *slog.Logger) *Updater {
	if logger == nil {
		logger = slog.Default()
	}
	return &Updater{command: command, logPath: logPath, logger: logger}
}

// Enabled reports whether a command is configured.
func (u *Updater) Enabled() bool { return u.command != "" }

// Running reports whether an update launched by Trigger is still in flight.
func (u *Updater) Running() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.running
}

// Trigger launches the update command detached in its own session, redirecting
// its output to logPath. It returns once the child is started (not when the
// update finishes — the process may well be restarted out from under us).
// Single-flight: a second concurrent call returns ErrInProgress.
func (u *Updater) Trigger() error {
	if !u.Enabled() {
		return ErrDisabled
	}
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return ErrInProgress
	}
	u.running = true
	u.mu.Unlock()

	if err := u.spawn(); err != nil {
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
		return err
	}
	return nil
}

// spawn starts the detached process and a goroutine to reap it (best-effort —
// if the restart kills us first, the transient unit carries the update through).
func (u *Updater) spawn() error {
	// #nosec G204 -- command is operator config, never request input.
	cmd := exec.Command("sh", "-c", u.command)
	// New session so the child is not in our process group; combined with a
	// systemd-run wrapper in the command it fully detaches from our cgroup.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	logf, err := os.OpenFile(u.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd.Stdout = logf
	cmd.Stderr = logf

	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return err
	}
	u.logger.Info("update.triggered", slog.Int("pid", cmd.Process.Pid), slog.String("log", u.logPath))

	go func() {
		waitErr := cmd.Wait()
		_ = logf.Close()
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
		if waitErr != nil {
			u.logger.Warn("update.exit", slog.String("err", waitErr.Error()))
		} else {
			u.logger.Info("update.exit", slog.String("status", "ok"))
		}
	}()
	return nil
}
