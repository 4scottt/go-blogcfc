package store

import "errors"

var (
	// ErrNotFound is returned when a row the caller asked for by id or
	// alias does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrDuplicate is returned when a uniqueness rule (a category name or
	// alias, a username) would be broken.
	ErrDuplicate = errors.New("store: duplicate")
)
