package storage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrObjectNotFound = errors.New("object not found")
	ErrInvalidKey     = errors.New("object key is not allowed")
)

var allowedKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9/_.-]{0,254}$`)

type Object struct {
	Body        []byte
	ContentType string
}

type Store interface {
	Put(ctx context.Context, key string, contentType string, body []byte) error
	Get(ctx context.Context, key string) (*Object, error)
	Delete(ctx context.Context, key string) error
}

func ValidateKey(key string) error {
	matchesPattern := allowedKeyPattern.MatchString(key)
	hasParentSegment := strings.Contains(key, "..")
	hasEmptySegment := strings.Contains(key, "//") || strings.HasSuffix(key, "/")
	if !matchesPattern || hasParentSegment || hasEmptySegment {
		return fmt.Errorf("%q: %w", key, ErrInvalidKey)
	}
	return nil
}
