package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	placeholderRE = regexp.MustCompile(`\{\{\s*([a-z_]+(?:\.[a-z_]+)?)\s*\}\}`)
	relativeRE    = regexp.MustCompile(`^(?:(today|tomorrow)|\+(\d{1,4})([mhd]))(?:\s+(\d{1,2}:\d{2}))?$`)
	// Action fields that hold a time and may be relative.
	timeFields = map[string]bool{"due_at": true, "remind_at": true}
)

// values are what placeholders resolve to.
type values map[string]any

// placeholderValues builds the values for a run: the event's snapshot plus
// the automation name and the current local date and time.
func placeholderValues(name string, data map[string]any, now time.Time, loc *time.Location) values {
	v := values{
		"automation.name": name,
		"today":           now.In(loc).Format("2006-01-02"),
		"now":             now.In(loc).Format("Mon 2 Jan 15:04"),
	}
	for subject, raw := range data {
		fields, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for k, val := range fields {
			v[subject+"."+k] = val
		}
		// A readable local due time for text, e.g. "Mon 19 Oct 17:00".
		if subject == "task" {
			v["task.due"] = ""
			if s, ok := fields["due_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					v["task.due"] = t.In(loc).Format("Mon 2 Jan 15:04")
				}
			}
		}
	}
	return v
}

// sampleValues stand in for a real event when checking an action at save
// time.
func sampleValues(allowed []string) values {
	v := values{}
	for _, name := range allowed {
		switch {
		case strings.HasSuffix(name, ".id"):
			v[name] = float64(1)
		case name == "note.tags":
			v[name] = []any{"sample"}
		default:
			v[name] = "sample"
		}
	}
	return v
}

// resolveInput fills in placeholders and relative times in an action's
// input and returns the JSON for the tool. A string that is only a
// placeholder for a number (e.g. "{{task.id}}") becomes that number.
func resolveInput(raw json.RawMessage, v values, now time.Time, loc *time.Location) (json.RawMessage, error) {
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil || input == nil {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	for k, val := range input {
		out, err := resolveValue(val, v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		if s, ok := out.(string); ok && timeFields[k] {
			if out, err = resolveTime(s, now, loc); err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
		}
		input[k] = out
	}
	return json.Marshal(input)
}

func resolveValue(val any, v values) (any, error) {
	switch x := val.(type) {
	case string:
		if m := placeholderRE.FindStringSubmatch(x); m != nil && m[0] == strings.TrimSpace(x) {
			got, ok := v[m[1]]
			if !ok {
				return nil, fmt.Errorf("{{%s}} isn't available for this trigger", m[1])
			}
			if n, ok := got.(float64); ok {
				return n, nil
			}
			if n, ok := got.(uint64); ok {
				return float64(n), nil
			}
		}
		var missing string
		out := placeholderRE.ReplaceAllStringFunc(x, func(p string) string {
			name := placeholderRE.FindStringSubmatch(p)[1]
			got, ok := v[name]
			if !ok {
				missing = name
				return p
			}
			return text(got)
		})
		if missing != "" {
			return nil, fmt.Errorf("{{%s}} isn't available for this trigger", missing)
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, err := resolveValue(e, v)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return val, nil
	}
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case uint64:
		return strconv.FormatUint(x, 10)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = text(e)
		}
		return strings.Join(parts, ", ")
	case []string:
		return strings.Join(x, ", ")
	default:
		return fmt.Sprint(x)
	}
}

// resolveTime turns "+30m", "+2h", "+1d", "+1d 09:00", "today 17:00" or
// "tomorrow 09:00" (local time) into RFC 3339. RFC 3339 times pass through.
func resolveTime(s string, now time.Time, loc *time.Location) (string, error) {
	s = strings.TrimSpace(s)
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s, nil
	}
	m := relativeRE.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return "", fmt.Errorf(`must be a time like "+1d", "+2h", "tomorrow 09:00" or RFC 3339`)
	}
	local := now.In(loc)
	var t time.Time
	switch {
	case m[1] != "":
		if m[4] == "" {
			return "", fmt.Errorf("%q needs a time, e.g. %q", m[1], m[1]+" 09:00")
		}
		t = local
		if m[1] == "tomorrow" {
			t = local.AddDate(0, 0, 1)
		}
	default:
		n, _ := strconv.Atoi(m[2])
		switch m[3] {
		case "m":
			t = local.Add(time.Duration(n) * time.Minute)
		case "h":
			t = local.Add(time.Duration(n) * time.Hour)
		case "d":
			t = local.AddDate(0, 0, n)
		}
		if n == 0 || (m[3] == "d" && n > 366) || (m[3] != "d" && n > 10000) {
			return "", fmt.Errorf("must be between +1 and a year ahead")
		}
		if m[4] != "" && m[3] != "d" {
			return "", fmt.Errorf("a time of day only goes with days, e.g. \"+1d 09:00\"")
		}
	}
	if m[4] != "" {
		hh, mm, ok := parseClock(fmt.Sprintf("%05s", m[4]))
		if !ok {
			return "", fmt.Errorf("%q isn't a time of day", m[4])
		}
		t = time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, loc)
	}
	return t.Format(time.RFC3339), nil
}
