package workflow

import (
	"fmt"
	"slices"
	"strings"

	"lifeos-ai/backend/internal/models"
)

const maxConditions = 10

// validateConditions checks each condition against the trigger's fields
// and normalizes its value. Errors are keyed conditions[i].
func validateConditions(spec TriggerSpec, conds []models.Condition) (map[string]string, []condition) {
	errs := map[string]string{}
	if len(conds) > maxConditions {
		errs["conditions"] = fmt.Sprintf("at most %d", maxConditions)
		return errs, nil
	}
	out := make([]condition, 0, len(conds))
	for i, raw := range conds {
		key := fmt.Sprintf("conditions[%d]", i)
		c, err := parseCondition(spec, raw)
		if err != "" {
			errs[key] = err
			continue
		}
		out = append(out, c)
	}
	return errs, out
}

// condition is a checked condition.
type condition struct {
	Field string
	Op    string
	Value any // string, []string, or nil
}

func parseCondition(spec TriggerSpec, raw models.Condition) (condition, string) {
	name, op := raw.Field, raw.Op
	var field *Field
	for i := range spec.Fields {
		if spec.Fields[i].Name == name {
			field = &spec.Fields[i]
		}
	}
	if field == nil {
		if len(spec.Fields) == 0 {
			return condition{}, "this trigger has no conditions"
		}
		return condition{}, fmt.Sprintf("unknown field %q", name)
	}
	if !slices.Contains(opsByType[field.Type], op) {
		return condition{}, fmt.Sprintf("%s can't use %q; use one of %s", field.Label, op, strings.Join(opsByType[field.Type], ", "))
	}
	c := condition{Field: name, Op: op}
	switch op {
	case "is_set", "is_not_set":
		return c, ""
	case "in", "not_in":
		list, ok := raw.Value.([]any)
		if !ok || len(list) == 0 {
			return c, "value must be a non-empty list"
		}
		var vals []string
		for _, e := range list {
			s, ok := e.(string)
			if !ok || (field.Choices != nil && !slices.Contains(field.Choices, s)) {
				return c, fmt.Sprintf("values must be among %s", strings.Join(field.Choices, ", "))
			}
			vals = append(vals, s)
		}
		c.Value = vals
		return c, ""
	default:
		s, ok := raw.Value.(string)
		s = strings.TrimSpace(s)
		if !ok || s == "" || len([]rune(s)) > 200 {
			return c, "value must be 1–200 characters of text"
		}
		if field.Choices != nil && !slices.Contains(field.Choices, s) {
			return c, fmt.Sprintf("value must be one of %s", strings.Join(field.Choices, ", "))
		}
		c.Value = s
		return c, ""
	}
}

// matches reports whether every condition holds for the event's values.
func matches(conds []condition, v values) bool {
	for _, c := range conds {
		if !holds(c, v[c.Field]) {
			return false
		}
	}
	return true
}

func holds(c condition, got any) bool {
	switch c.Op {
	case "is_set":
		return isSet(got)
	case "is_not_set":
		return !isSet(got)
	}
	if list, ok := asStrings(got); ok { // tags
		has := slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, c.Value.(string)) })
		if c.Op == "contains" {
			return has
		}
		return !has // not_contains
	}
	s := strings.ToLower(text(got))
	switch c.Op {
	case "equals":
		return s == strings.ToLower(c.Value.(string))
	case "not_equals":
		return s != strings.ToLower(c.Value.(string))
	case "contains":
		return strings.Contains(s, strings.ToLower(c.Value.(string)))
	case "not_contains":
		return !strings.Contains(s, strings.ToLower(c.Value.(string)))
	case "in":
		return slices.Contains(c.Value.([]string), s)
	case "not_in":
		return !slices.Contains(c.Value.([]string), s)
	}
	return false
}

func isSet(v any) bool {
	if list, ok := asStrings(v); ok {
		return len(list) > 0
	}
	return strings.TrimSpace(text(v)) != ""
}

func asStrings(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, text(e))
		}
		return out, true
	}
	return nil, false
}
