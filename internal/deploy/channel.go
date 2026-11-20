package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/meros/fyr/internal/release"
)

// V0.versioned-releases: the update channel a host follows (config
// updateChannel). "main" follows `deployable` and its build-<sha12> releases
// (the behaviour before channels, and the default of a config without the
// key); "stable" follows the newest version release vX.Y.Z that is not a
// prerelease; a version (v1.2.3, v1.3.0-rc.1) pins the host to that release.

// Channel names.
const (
	ChannelMain   = "main"
	ChannelStable = "stable"
)

// ParseChannel checks a config's updateChannel: "" is main, "main",
// "stable", or a version tag to pin to.
func ParseChannel(s string) (string, error) {
	switch s = strings.TrimSpace(s); {
	case s == "" || s == ChannelMain:
		return ChannelMain, nil
	case s == ChannelStable:
		return ChannelStable, nil
	case release.IsVersion(s):
		return s, nil
	}
	return "", fmt.Errorf("updateChannel %q is not main, stable or a version (v1.2.3)", s)
}

// channel is the channel this source follows; a bad one is main (fyrd says
// so at start).
func (r *Releases) channel() string {
	c, err := ParseChannel(r.Channel)
	if err != nil {
		return ChannelMain
	}
	return c
}

// ChannelName is the channel this source follows, for Settings and the log.
func (r *Releases) ChannelName() string { return r.channel() }

// Wanted is the commit this source's channel points at, and the tag of the
// release that holds it: `deployable` and build-<sha12> on main; the newest
// stable version, or the pinned one, and that version's tag otherwise.
func (r *Releases) wanted(ctx context.Context) (sha, tag string, err error) {
	switch c := r.channel(); c {
	case ChannelMain:
		sha, err = r.mainHead(ctx)
		return sha, release.Tag(sha), err
	case ChannelStable:
		tag, err = r.newestStable(ctx)
		if err != nil {
			return "", "", err
		}
	default:
		tag = c
	}
	sha, err = r.tagCommit(ctx, tag)
	if err != nil {
		return "", "", err
	}
	r.mu.Lock()
	if r.tags == nil {
		r.tags = map[string]string{}
	}
	r.tags[sha] = tag
	r.mu.Unlock()
	return sha, tag, nil
}

// releaseTag is the tag of the release that holds sha for this channel: the
// version the channel last pointed at with this commit, else build-<sha12>.
func (r *Releases) releaseTag(sha string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tags[sha]; ok {
		return t
	}
	return release.Tag(sha)
}

// VersionOf is the version release the channel last found at sha, or "".
func (r *Releases) VersionOf(sha string) string {
	if t := r.releaseTag(sha); release.IsVersion(t) {
		return t
	}
	return ""
}

// newestStable is the newest published version release that is not a
// prerelease: GitHub's flag and the tag's -suffix both count, drafts and
// build-<sha12> releases are passed over. One API call, asked again with
// its ETag (a 304 costs nothing).
func (r *Releases) newestStable(ctx context.Context) (string, error) {
	b, err := r.api(ctx, "repos/"+r.repo()+"/releases?per_page=50")
	if err != nil {
		return "", fmt.Errorf("listing the releases of %s: %w", r.repo(), err)
	}
	var rels []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(b, &rels); err != nil {
		return "", fmt.Errorf("listing the releases of %s: %w", r.repo(), err)
	}
	best, bestV := "", release.Semver{}
	for _, x := range rels {
		v, ok := release.ParseVersion(x.Tag)
		if !ok || x.Draft || x.Prerelease || v.Prerelease() {
			continue
		}
		if best == "" || bestV.Less(v) {
			best, bestV = x.Tag, v
		}
	}
	if best == "" {
		return "", fmt.Errorf("%s has no stable release (vX.Y.Z) yet", r.repo())
	}
	return best, nil
}

// tagCommit is the commit a tag points at: by git ls-remote, else the API.
func (r *Releases) tagCommit(ctx context.Context, tag string) (string, error) {
	if sha, err := r.lsRemote(ctx, tag); err == nil {
		return sha, nil
	}
	b, err := r.api(ctx, "repos/"+r.repo()+"/git/ref/tags/"+url.PathEscape(tag))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", tag, err)
	}
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		return "", fmt.Errorf("reading %s: %w", tag, err)
	}
	sha := ref.Object.SHA
	if ref.Object.Type == "tag" { // an annotated tag: its commit
		tb, err := r.api(ctx, "repos/"+r.repo()+"/git/tags/"+sha)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", tag, err)
		}
		var t struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		json.Unmarshal(tb, &t)
		sha = t.Object.SHA
	}
	if !isSHA(sha) {
		return "", fmt.Errorf("%s points at %q", tag, sha)
	}
	return sha, nil
}
