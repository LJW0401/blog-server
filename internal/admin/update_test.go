// update_test.go covers the /manage/update handlers: the status page, the
// CSRF gate on the POST trigger, the disabled path, and a real (detached)
// trigger that runs a harmless command.
package admin_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/admin"
	"github.com/penguin/blog-server/internal/update"
)

// newUpdateHandlers wires an UpdateHandlers onto the crud harness with a given
// checker and updater.
func newUpdateHandlers(b *crudBundle, c *update.Checker, u *update.Updater) *admin.UpdateHandlers {
	return &admin.UpdateHandlers{Parent: b.Admin, Checker: c, Updater: u, Repo: "acme/blog"}
}

func availableChecker(t *testing.T) *update.Checker {
	t.Helper()
	c := update.NewChecker("v1.0.0", func(context.Context, string) (string, string, bool, error) {
		return "v2.0.0", "e", false, nil
	}, time.Hour, nil)
	c.CheckNow(context.Background())
	return c
}

func TestUpdatePage_Smoke_UpToDate(t *testing.T) {
	b := crudSetup(t)
	c := update.NewChecker("v1.0.0", nil, time.Hour, nil) // disabled fetch → no update
	uh := newUpdateHandlers(b, c, update.NewUpdater("", filepath.Join(b.DataDir, "u.log"), nil))

	w := b.authedGet(t, "/manage/update", uh.Page)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "系统更新") || !strings.Contains(body, "当前已是最新版本") {
		t.Errorf("up-to-date page missing expected content")
	}
}

func TestUpdatePage_Smoke_AvailableWithButton(t *testing.T) {
	b := crudSetup(t)
	c := availableChecker(t)
	uh := newUpdateHandlers(b, c, update.NewUpdater("true", filepath.Join(b.DataDir, "u.log"), nil))

	w := b.authedGet(t, "/manage/update", uh.Page)
	body := w.Body.String()
	if !strings.Contains(body, "v2.0.0") || !strings.Contains(body, "确认更新到 v2.0.0") {
		t.Errorf("available+enabled page should show confirm button, got:\n%s", body)
	}
}

func TestUpdatePage_AvailableNoCommandShowsInstructions(t *testing.T) {
	// 边界：有新版但未配置 update_command → 展示手动升级指引，不出现确认按钮
	b := crudSetup(t)
	c := availableChecker(t)
	uh := newUpdateHandlers(b, c, update.NewUpdater("", filepath.Join(b.DataDir, "u.log"), nil))

	w := b.authedGet(t, "/manage/update", uh.Page)
	body := w.Body.String()
	if !strings.Contains(body, "manage.sh update") {
		t.Errorf("should show manual upgrade instructions")
	}
	if strings.Contains(body, "确认更新到") {
		t.Errorf("must not show one-click button when command unset")
	}
}

func TestUpdateTrigger_Edge_CSRFMissing(t *testing.T) {
	// 权限/认证：POST 缺 CSRF 必须 403，且不得触发更新
	b := crudSetup(t)
	sentinel := filepath.Join(b.DataDir, "ran.txt")
	u := update.NewUpdater("touch "+sentinel, filepath.Join(b.DataDir, "u.log"), nil)
	uh := newUpdateHandlers(b, availableChecker(t), u)

	w := b.authedPost(t, "/manage/update", url.Values{}, uh.Trigger)
	if w.Code != 403 {
		t.Fatalf("missing CSRF want 403, got %d", w.Code)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("update must not run when CSRF is rejected")
	}
}

func TestUpdateTrigger_Edge_Disabled(t *testing.T) {
	// 配置边界：未配置命令时带 CSRF 触发返回 403 + 错误提示
	b := crudSetup(t)
	u := update.NewUpdater("", filepath.Join(b.DataDir, "u.log"), nil)
	uh := newUpdateHandlers(b, availableChecker(t), u)

	w := b.authedPost(t, "/manage/update", url.Values{"csrf": {b.CSRF}}, uh.Trigger)
	if w.Code != 403 {
		t.Fatalf("disabled trigger want 403, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "未配置") {
		t.Errorf("expected disabled error message")
	}
}

func TestUpdateTrigger_Smoke_Runs(t *testing.T) {
	b := crudSetup(t)
	sentinel := filepath.Join(b.DataDir, "ran.txt")
	u := update.NewUpdater("touch "+sentinel, filepath.Join(b.DataDir, "u.log"), nil)
	uh := newUpdateHandlers(b, availableChecker(t), u)

	w := b.authedPost(t, "/manage/update", url.Values{"csrf": {b.CSRF}}, uh.Trigger)
	if w.Code != 200 {
		t.Fatalf("trigger want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "更新已启动") {
		t.Errorf("expected 'updating' page")
	}
	// The command is detached; poll for its effect.
	if !waitForFile(2*time.Second, sentinel) {
		t.Error("update command did not run")
	}
}

func waitForFile(timeout time.Duration, path string) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := os.Stat(path)
	return err == nil
}
