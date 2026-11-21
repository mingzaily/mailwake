package mail

import "strings"

// DisplayName returns the sender's display name without the quotes that some
// servers and senders keep around it, for example `"Cindy"` becomes `Cindy`.
// Quotes are display-name syntax rather than part of the name.
func DisplayName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return name
	}
	quote := name[0]
	if (quote != '"' && quote != '\'') || name[len(name)-1] != quote {
		return name
	}
	inner := name[1 : len(name)-1]
	if quote == '"' {
		inner = strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(inner)
	}
	return strings.TrimSpace(inner)
}
