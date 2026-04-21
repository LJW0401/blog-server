package content

import "strings"

const (
	introOpenTag  = "<!-- portfolio:intro -->"
	introCloseTag = "<!-- /portfolio:intro -->"
	// introMaxBytes caps the size of the intro block body. Larger bodies
	// are treated as malformed and fall back to ("", body). Keeps the home
	// card lightweight and prevents accidental "dumped whole article".
	introMaxBytes = 4 * 1024
)

// extractIntro returns (intro, rest) from a Markdown body:
//
//   - `intro` is the raw Markdown source between the first
//     `<!-- portfolio:intro -->` open tag and the next `<!-- /portfolio:intro -->`
//     close tag (exclusive of the tags themselves, with leading/trailing
//     newlines trimmed).
//   - `rest` is the body with the entire tag pair (and one adjacent newline
//     on each side, if present) removed.
//
// If any of the following hold, it falls back to ("", body) unchanged:
//   - open tag not found
//   - close tag not found after the open tag
//   - the intro region contains a duplicate open or close tag (nested /
//     malformed)
//   - the intro region exceeds introMaxBytes
//
// The function is pure and safe for concurrent use.
func extractIntro(body string) (intro, rest string) {
	openIdx := strings.Index(body, introOpenTag)
	if openIdx < 0 {
		return "", body
	}
	afterOpen := openIdx + len(introOpenTag)
	closeRel := strings.Index(body[afterOpen:], introCloseTag)
	if closeRel < 0 {
		return "", body
	}
	closeIdx := afterOpen + closeRel
	inner := body[afterOpen:closeIdx]

	// Reject nested / duplicate markers inside the intro region.
	if strings.Contains(inner, introOpenTag) || strings.Contains(inner, introCloseTag) {
		return "", body
	}
	if len(inner) > introMaxBytes {
		return "", body
	}

	// Trim at most one adjacent newline on each side so the stripped body
	// doesn't leave a jarring double blank line.
	startCut := openIdx
	if startCut > 0 && body[startCut-1] == '\n' {
		startCut--
	}
	endCut := closeIdx + len(introCloseTag)
	if endCut < len(body) && body[endCut] == '\n' {
		endCut++
	}
	rest = body[:startCut] + body[endCut:]
	intro = strings.Trim(inner, "\n")
	return intro, rest
}
