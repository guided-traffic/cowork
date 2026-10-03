package auth

import (
	"regexp"
	"strings"
)

// usernamePattern is the username rule of the users table: lower case, 1 to 63
// characters, starting with a letter or a digit (docs/adr/0033 D2).
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ValidUsername reports whether s can be the username of a person.
func ValidUsername(s string) bool { return usernamePattern.MatchString(s) }

// NormaliseUsername is what a login does with the name it was given before it
// looks it up: trimmed and lower-cased. A value that still cannot be a
// username comes back empty — it names no account, and the login treats it
// like any other name that names none.
func NormaliseUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !ValidUsername(s) {
		return ""
	}
	return s
}
