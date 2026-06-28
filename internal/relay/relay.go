// Package relay holds the pure decision logic for `flotilla watch`: whether a
// gateway message is an operator command (Accept) and where it should be
// delivered (Route). Keeping these as pure functions — independent of discordgo
// and tmux — makes the security-critical filtering and routing fully unit
// testable.
package relay

import (
	"fmt"
	"strings"
	"unicode"
)

// Accept reports whether a gateway message should be acted on: it requires the
// configured operator as the author.
//
// The self-mirror feedback guard (dropping the channel's OWN webhook posts) USED to
// live here, keyed on a Discord webhook id. As of the Transport SPI extraction it
// moved INTO the transport adapter (internal/transport's selfMirrorGuardAdapter),
// which drops a self-post author-agnostically BEFORE this function is ever reached —
// so a self-post can no longer arrive at the relay, and Accept no longer needs (or
// carries) the Discord-shaped webhookID arm. This keeps the relay's decision logic
// fully medium-agnostic. The guard remains author-agnostic at the adapter, so it
// still holds even if this operator-author rule is later relaxed.
func Accept(authorID, operatorID string) bool {
	return operatorID != "" && authorID == operatorID
}

// Decision is the routing outcome for an accepted message.
type Decision struct {
	Agent   string // delivery target (the XO agent, or a named desk)
	Message string // the body to deliver
	Notice  string // optional one-line channel notice (e.g. unknown @agent)
}

// Route maps an accepted operator message to a delivery decision.
//
//   - "@@..."            → a literal "@..." delivered to the XO (escape hatch).
//   - "@<name> <body>"   → <body> to <name> when resolve(name) succeeds
//     (case-insensitive); the body is preserved verbatim, including newlines.
//   - "@<unknown> <body>" → the whole message to the XO, plus a Notice.
//   - anything else      → the whole message to the XO.
//
// resolve maps a (case-insensitive) token to a canonical agent name, ok.
func Route(body, xoAgent string, resolve func(string) (string, bool)) Decision {
	if strings.HasPrefix(body, "@@") {
		return Decision{Agent: xoAgent, Message: "@" + body[2:]}
	}
	if strings.HasPrefix(body, "@") {
		afterAt := body[1:]
		i := strings.IndexFunc(afterAt, unicode.IsSpace)
		if i <= 0 {
			return Decision{Agent: xoAgent, Message: body} // "@name" with no body
		}
		token := afterAt[:i]
		rest := strings.TrimLeft(afterAt[i:], " \t\r\n")
		if rest == "" {
			return Decision{Agent: xoAgent, Message: body}
		}
		if canon, ok := resolve(token); ok {
			return Decision{Agent: canon, Message: rest}
		}
		return Decision{Agent: xoAgent, Message: body, Notice: fmt.Sprintf("no agent %q; sent to XO", token)}
	}
	return Decision{Agent: xoAgent, Message: body}
}
