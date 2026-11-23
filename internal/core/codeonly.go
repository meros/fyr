package core

import (
	"fmt"
	"strings"
)

// codeOnlyNotice is the agent's one line about an ask Fyr closed: the ask,
// the change, and what to use instead.
func codeOnlyNotice(action string, c *Change) string {
	ask := strings.TrimPrefix(action, "change.")
	instead := goneAsks[ChangeAsk(ask)]
	if instead == "" {
		instead = goneKinds[c.Kind]
	}
	if instead == "" {
		instead = CodeOnlyReason + "."
	}
	return fmt.Sprintf("Fyr closed your %s ask on %q and ended the change as abandoned. %s", ask, c.Title, instead)
}
