// updater_test.go covers the updater's disabled path, a real detached spawn
// (smoke), and single-flight rejection of a concurrent trigger.
package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdaterDisabled(t *testing.T) {
	// 权限/配置边界：空命令禁用在线更新
	u := NewUpdater("", filepath.Join(t.TempDir(), "update.log"), nil)
	if u.Enabled() {
		t.Fatal("empty command must report disabled")
	}
	if err := u.Trigger(); !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
}

func TestUpdaterSpawns(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "ran.txt")
	// Command writes a sentinel so we can confirm it actually executed.
	u := NewUpdater("touch "+sentinel, filepath.Join(dir, "update.log"), nil)
	if !u.Enabled() {
		t.Fatal("non-empty command should be enabled")
	}
	if err := u.Trigger(); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	// Detached but fast; poll for the sentinel and for running to clear.
	if !waitFor(2*time.Second, func() bool {
		_, err := os.Stat(sentinel)
		return err == nil && !u.Running()
	}) {
		t.Fatal("command did not run / running flag stuck")
	}
}

func TestUpdaterSingleFlight(t *testing.T) {
	// 并发竞态：更新进行中时第二次触发必须被拒
	dir := t.TempDir()
	u := NewUpdater("sleep 1", filepath.Join(dir, "update.log"), nil)
	if err := u.Trigger(); err != nil {
		t.Fatalf("first Trigger: %v", err)
	}
	if err := u.Trigger(); !errors.Is(err, ErrInProgress) {
		t.Fatalf("second Trigger want ErrInProgress, got %v", err)
	}
	// Cleanup: wait for the sleep to finish so the goroutine clears running.
	if !waitFor(3*time.Second, func() bool { return !u.Running() }) {
		t.Fatal("updater stuck running after command should have exited")
	}
}

// waitFor polls cond until true or timeout; returns whether it became true.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}
