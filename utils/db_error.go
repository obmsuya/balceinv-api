package utils

import "strings"

// FriendlyDBError turns a raw GORM/SQLite driver error into a message safe
// to show a user. Services return DB errors unwrapped, so anything that
// isn't a recognized constraint failure is passed through unchanged rather
// than hidden — better a plain error than a silently wrong one.
// ponytail: only covers the constraint kinds actually seen leaking to the
// UI so far; extend the switch if another raw SQLite string turns up.
func FriendlyDBError(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "FOREIGN KEY constraint failed"):
		return "That record references something that no longer exists. Please refresh and try again."
	case strings.Contains(message, "UNIQUE constraint failed"):
		return "A record with that value already exists."
	default:
		return message
	}
}
