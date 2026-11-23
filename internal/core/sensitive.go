package core

import (
	"errors"
	"strings"
)

// Sensitive PRs (V0.sensitive-pr): a PR whose merge needs the owner's own,
// deliberate press, such as a promotion to production. Two marks make a PR
// sensitive, and either is enough: the agent's reason on its merge ask
// (change_request {ask: merge, args: {sensitive: "<why>"}}), which the
// change keeps, and the GitHub label "sensitive" on the PR. A mark only makes
// a merge stricter: Fyr never approves a sensitive merge by the repo's
// auto-merge setting, the board leaves it out of Merge all green, and the
// card shows the reason in red, with a confirm on Merge.

// SensitiveLabel is the GitHub label that marks a PR sensitive.
const SensitiveLabel = "sensitive"

// LabelSensitive is the reason a PR labelled SensitiveLabel shows.
const LabelSensitive = `it has the GitHub label "sensitive"`

// SensitiveWhy is why c's merge is sensitive, or "".
func SensitiveWhy(c *Change) string {
	if c == nil {
		return ""
	}
	if c.Sensitive != "" {
		return c.Sensitive
	}
	if c.PR != nil && c.PR.Sensitive {
		return LabelSensitive
	}
	return ""
}

// askSensitive is why the merge ask with args a is sensitive, or "": the
// ask's own reason, else its change's.
func askSensitive(t *Thread, a map[string]any) string {
	if why, _ := a["sensitive"].(string); why != "" {
		return why
	}
	url, _ := a["pr"].(string)
	return SensitiveWhy(changeByPR(t, url))
}

// checkSensitive validates a merge ask's sensitive arg: one line of text.
func checkSensitive(a map[string]any) error {
	v, ok := a["sensitive"]
	if !ok {
		return nil
	}
	s, _ := v.(string)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return errors.New(`args.sensitive, when given, must say in a few words why the merge is sensitive, for example "promotes to production"`)
	}
	if len(s) > 200 {
		s = s[:200]
	}
	a["sensitive"] = s
	return nil
}

// HasSensitiveLabel reports whether names hold SensitiveLabel (any case).
func HasSensitiveLabel(names []string) bool {
	for _, n := range names {
		if strings.EqualFold(strings.TrimSpace(n), SensitiveLabel) {
			return true
		}
	}
	return false
}
