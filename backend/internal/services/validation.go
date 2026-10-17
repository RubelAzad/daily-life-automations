package services

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ValidationError carries per-field messages; handlers render it as 422.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "validation failed" }

// checker collects the first error per field.
type checker struct {
	fields map[string]string
}

func (c *checker) fail(field, msg string) {
	if c.fields == nil {
		c.fields = map[string]string{}
	}
	if _, exists := c.fields[field]; !exists {
		c.fields[field] = msg
	}
}

func (c *checker) err() error {
	if len(c.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: c.fields}
}

// requiredText trims value and checks it is non-empty and at most n characters.
func (c *checker) requiredText(field, value string, n int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		c.fail(field, "is required")
	}
	c.maxLen(field, value, n)
	return value
}

func (c *checker) maxLen(field, value string, n int) {
	if len([]rune(value)) > n {
		c.fail(field, "must be at most "+strconv.Itoa(n)+" characters")
	}
}

func (c *checker) oneOf(field, value string, allowed []string) {
	if !slices.Contains(allowed, value) {
		c.fail(field, "must be one of: "+strings.Join(allowed, ", "))
	}
}

// parseTime accepts RFC 3339 ("2026-10-17T09:00:00+06:00") and returns UTC.
func (c *checker) parseTime(field, value string) *time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		c.fail(field, "must be an RFC 3339 date-time, e.g. 2026-10-17T09:00:00Z")
		return nil
	}
	t = t.UTC()
	return &t
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (c *checker) color(field string, value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	if !hexColor.MatchString(*value) {
		c.fail(field, "must be a hex color like #3b82f6")
	}
	lower := strings.ToLower(*value)
	return &lower
}

// optionalText trims value and maps empty to nil (stored as NULL).
func optionalText(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// Optional distinguishes an absent JSON field (Set=false) from an explicit
// null (Set=true, Null=true), which PATCH needs to clear nullable columns.
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

// Ptr returns nil for null, otherwise a pointer to the value.
func (o Optional[T]) Ptr() *T {
	if o.Null {
		return nil
	}
	v := o.Value
	return &v
}
