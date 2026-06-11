// export.go backs the admin "导出全部数据" action: it streams the full-site
// bundle produced by internal/transfer to the logged-in admin. Authentication
// is the AuthGate session check on /manage/*, not this handler.
package admin

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/penguin/blog-server/internal/transfer"
)

// ExportHandlers streams full-site export bundles. DataDir/DB point at the live
// instance; AppVersion is stamped into the bundle manifest for diagnostics.
type ExportHandlers struct {
	DataDir    string
	DB         *sql.DB
	Logger     *slog.Logger
	AppVersion string
}

// Download streams the export bundle as an attachment. The request is assumed
// already authorized by the /manage/* AuthGate.
func (h *ExportHandlers) Download(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	filename := fmt.Sprintf("blog-export-%s.tar.gz", now.Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Stream straight to the client. A mid-stream failure can't retroactively
	// set a 500 once bytes are flushed, so we log it for diagnosis.
	err := transfer.WriteBundle(r.Context(), w, h.DataDir, now.Format(time.RFC3339), h.AppVersion, h.DB)
	if err != nil {
		h.Logger.Error("admin.export",
			slog.String("err", err.Error()),
			slog.String("remote", r.RemoteAddr),
		)
		return
	}
	h.Logger.Info("admin.export.done",
		slog.String("file", filename),
		slog.String("remote", r.RemoteAddr),
	)
}
