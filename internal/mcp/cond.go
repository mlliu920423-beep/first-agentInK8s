package mcp

import (
	"fmt"
	"os"
	"strings"
)

// condition is the parsed form of a Config.EnabledIf expression.
//
// Grammar (MVP — deliberately narrow; see ADR-005 Alternative 3 for why we
// didn't pull in a full expression engine):
//
//	""              → always true
//	"always"        → always true
//	"env:VAR"       → os.Getenv("VAR") != ""
//	"env:VAR=value" → os.Getenv("VAR") == "value"
//
// Anything else is a parse-time error. That means a typo (e.g. `enev:X=1`)
// crashes boot rather than silently disabling the server. Debugging "why
// isn't my MCP running" is much easier from a startup panic than from a
// missing tool at runtime.
type condition struct {
	raw     string // for logs: "env:ENABLE_FS_MCP=1" etc.
	always  bool
	envVar  string
	envWant string // "" means "just check non-empty"
	envEq   bool   // true when the expr had "=value"
}

func parseCondition(expr string) (condition, error) {
	c := condition{raw: expr}
	if expr == "" || expr == "always" {
		c.always = true
		if expr == "" {
			c.raw = "always"
		}
		return c, nil
	}
	if strings.HasPrefix(expr, "env:") {
		body := strings.TrimPrefix(expr, "env:")
		if body == "" {
			return c, fmt.Errorf("empty variable name in %q", expr)
		}
		// Split on first '='; anything after (including further '=') is
		// treated as literal. Explicit second '=' anywhere is a typo we
		// choose to reject to keep the grammar boring.
		if i := strings.Index(body, "="); i >= 0 {
			name := body[:i]
			val := body[i+1:]
			if name == "" {
				return c, fmt.Errorf("empty variable name before '=' in %q", expr)
			}
			if strings.Contains(val, "=") {
				return c, fmt.Errorf("multiple '=' in %q — MVP grammar only allows one", expr)
			}
			c.envVar, c.envWant, c.envEq = name, val, true
			return c, nil
		}
		c.envVar = body
		return c, nil
	}
	return c, fmt.Errorf("unknown condition %q (want `always`, `env:VAR`, or `env:VAR=value`)", expr)
}

// eval returns whether the MCP server should be started right now.
// Reads os.Getenv each call so tests can flip vars between calls.
func (c condition) eval() bool {
	if c.always {
		return true
	}
	got := os.Getenv(c.envVar)
	if c.envEq {
		return got == c.envWant
	}
	return got != ""
}

// expandVars substitutes ${env:VAR} and ${env:VAR:-default} in s.
//
// We roll our own tiny parser because os.ExpandEnv doesn't support the
// `:-default` idiom and we want to keep a discriminator prefix (`env:`)
// so future sources (`${secret:...}` etc.) can be added without ambiguity.
//
// Behaviour:
//
//	${env:VAR}            → os.Getenv("VAR") (empty string if unset)
//	${env:VAR:-fallback}  → os.Getenv("VAR"), or "fallback" if empty
//
// Anything malformed (`${env:}`, `${something-else:...}`, unterminated `${`)
// is left in place verbatim rather than raising — we prefer soft-fail here
// because expansion runs on many fields at parse time and a hard error would
// obscure the "real" underlying yaml problem in the caller's message.
func expandVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.Index(s[i+2:], "}")
			if end < 0 {
				// unterminated — copy the rest literal
				out.WriteString(s[i:])
				break
			}
			inner := s[i+2 : i+2+end]
			out.WriteString(expandOne(inner, s[i:i+3+end]))
			i += 3 + end
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// expandOne handles the body between `${` and `}`. `literal` is the whole
// `${...}` in case we need to preserve it on malformed input.
func expandOne(inner, literal string) string {
	if !strings.HasPrefix(inner, "env:") {
		return literal
	}
	body := strings.TrimPrefix(inner, "env:")
	name := body
	def := ""
	hasDefault := false
	if i := strings.Index(body, ":-"); i >= 0 {
		name = body[:i]
		def = body[i+2:]
		hasDefault = true
	}
	if name == "" {
		return literal
	}
	v := os.Getenv(name)
	if v == "" && hasDefault {
		return def
	}
	return v
}
