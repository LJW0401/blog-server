// release.go fetches the latest published release tag for a repository. Used
// by the release checker to compare the running version against upstream.
// Reuses the Client's ETag-conditional do() so steady-state polling stays well
// under the unauthenticated rate limit.
package github

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetLatestReleaseResult carries the parsed tag plus caching metadata. A 304
// response yields NotModified=true and an empty TagName — callers keep their
// prior value.
type GetLatestReleaseResult struct {
	TagName     string
	ETag        string
	NotModified bool
}

// GetLatestRelease returns the tag_name of the repository's latest non-draft,
// non-prerelease release (GitHub's /releases/latest semantics). priorETag, when
// non-empty, is sent as If-None-Match.
func (c *Client) GetLatestRelease(ctx context.Context, owner, name, priorETag string) (*GetLatestReleaseResult, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/latest", owner, name)
	body, hdr, err := c.do(ctx, path, priorETag)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return &GetLatestReleaseResult{NotModified: true, ETag: hdr.Get("ETag")}, nil
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode release: %v", ErrUpstream, err)
	}
	return &GetLatestReleaseResult{TagName: payload.TagName, ETag: hdr.Get("ETag")}, nil
}
