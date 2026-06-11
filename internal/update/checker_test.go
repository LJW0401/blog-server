// checker_test.go exercises the polling checker with a fake fetcher: the happy
// path, fail-soft on fetch errors, ETag not-modified handling, and the disabled
// (nil fetch) path.
package update

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckerHappyPath(t *testing.T) {
	fetch := func(_ context.Context, _ string) (string, string, bool, error) {
		return "v2.0.0", "etag-1", false, nil
	}
	c := NewChecker("v1.0.0", fetch, time.Hour, nil)
	c.CheckNow(context.Background())

	st := c.State()
	if !st.Available || st.Latest != "v2.0.0" {
		t.Fatalf("want available v2.0.0, got %+v", st)
	}
	if st.CheckedAt.IsZero() {
		t.Error("CheckedAt should be set after a successful check")
	}
}

func TestCheckerFailSoft(t *testing.T) {
	// 失败依赖：fetch 报错时保留上一次的状态，不清空、不 panic
	calls := 0
	fetch := func(_ context.Context, _ string) (string, string, bool, error) {
		calls++
		if calls == 1 {
			return "v2.0.0", "etag-1", false, nil
		}
		return "", "", false, errors.New("api down")
	}
	c := NewChecker("v1.0.0", fetch, time.Hour, nil)
	c.CheckNow(context.Background()) // success
	c.CheckNow(context.Background()) // error — must keep prior

	if st := c.State(); !st.Available || st.Latest != "v2.0.0" {
		t.Fatalf("error check should preserve prior state, got %+v", st)
	}
}

func TestCheckerNotModified(t *testing.T) {
	// 304 路径：notModified=true 时保留上一次 tag，不被空 tag 覆盖
	calls := 0
	fetch := func(_ context.Context, priorETag string) (string, string, bool, error) {
		calls++
		if calls == 1 {
			return "v2.0.0", "etag-1", false, nil
		}
		if priorETag != "etag-1" {
			t.Errorf("expected prior ETag to be sent, got %q", priorETag)
		}
		return "", "etag-1", true, nil
	}
	c := NewChecker("v1.0.0", fetch, time.Hour, nil)
	c.CheckNow(context.Background())
	c.CheckNow(context.Background())

	if st := c.State(); st.Latest != "v2.0.0" {
		t.Fatalf("not-modified should keep prior tag, got %q", st.Latest)
	}
}

func TestCheckerDisabled(t *testing.T) {
	// nil fetch（update_repo 未配置）：检查为空操作，永不提示更新
	c := NewChecker("v1.0.0", nil, time.Hour, nil)
	c.CheckNow(context.Background())
	if st := c.State(); st.Available || st.Latest != "" {
		t.Fatalf("disabled checker must report nothing, got %+v", st)
	}
}

func TestCheckerNotNewer(t *testing.T) {
	// 上游版本不高于当前：有 Latest 但 Available=false
	fetch := func(_ context.Context, _ string) (string, string, bool, error) {
		return "v1.0.0", "e", false, nil
	}
	c := NewChecker("v1.0.0", fetch, time.Hour, nil)
	c.CheckNow(context.Background())
	if st := c.State(); st.Available {
		t.Fatalf("equal version must not be available, got %+v", st)
	}
}
