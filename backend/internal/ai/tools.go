package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"lifeos-ai/backend/internal/models"
)

// Risk decides whether a tool call needs the user's approval.
type Risk string

const (
	RiskRead Risk = "read" // looks things up; never changes anything
	RiskLow  Risk = "low"  // routine, easy-to-undo change (create a task, set a reminder)
	// RiskHigh is for sending messages, spending money, deleting data and
	// bookings. These always need approval, whatever the user's settings.
	RiskHigh Risk = "high"
)

// NeedsApproval applies the user's ai_approval_mode to a tool's risk.
// Unknown modes are treated as the strictest one.
func NeedsApproval(risk Risk, mode string) bool {
	switch risk {
	case RiskRead:
		return false
	case RiskLow:
		return mode != models.ApprovalHighImpactOnly
	default:
		return true
	}
}

// Tool is one action the assistant may take. Its input is decoded strictly
// into a typed struct (unknown fields are rejected) and the user is bound
// from the session, never taken from the model.
type Tool struct {
	Name        string
	Description string
	Risk        Risk
	Schema      json.RawMessage
	prepare     func(ctx context.Context, userID uint64, raw json.RawMessage) (prepared, error)
}

// prepared is a decoded tool call, ready to run.
type prepared struct {
	summary string
	run     func(ctx context.Context) (any, error)
}

// InputError is bad tool input from the model (as opposed to a service
// validation error on otherwise well-formed input).
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// define builds a Tool whose input is decoded into T.
func define[T any](name, description string, risk Risk, schema string,
	describe func(ctx context.Context, userID uint64, in T) string,
	run func(ctx context.Context, userID uint64, in T) (any, error),
) Tool {
	if !json.Valid([]byte(schema)) {
		panic("ai: invalid schema for tool " + name)
	}
	return Tool{
		Name: name, Description: description, Risk: risk, Schema: json.RawMessage(schema),
		prepare: func(ctx context.Context, userID uint64, raw json.RawMessage) (prepared, error) {
			var in T
			if err := decodeStrict(raw, &in); err != nil {
				return prepared{}, err
			}
			return prepared{
				summary: describe(ctx, userID, in),
				run:     func(ctx context.Context) (any, error) { return run(ctx, userID, in) },
			}, nil
		},
	}
}

// decodeStrict accepts exactly one JSON object with only known fields.
func decodeStrict(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return &InputError{Message: fmt.Sprintf("invalid input: %s must be %s", typeErr.Field, typeErr.Type)}
		}
		return &InputError{Message: "invalid input: " + err.Error()}
	}
	if dec.More() {
		return &InputError{Message: "invalid input: expected a single JSON object"}
	}
	return nil
}

// Registry is the allowlist of tools. Anything not registered can't run.
type Registry struct {
	tools []Tool
	index map[string]int
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{index: map[string]int{}}
	for _, t := range tools {
		if _, dup := r.index[t.Name]; dup {
			panic("ai: duplicate tool " + t.Name)
		}
		r.index[t.Name] = len(r.tools)
		r.tools = append(r.tools, t)
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	i, ok := r.index[name]
	if !ok {
		return Tool{}, false
	}
	return r.tools[i], true
}

func (r *Registry) All() []Tool { return append([]Tool(nil), r.tools...) }

// Check decodes input for the named tool as a call would (strictly) and
// returns the action's summary, without running it. Errors are safe to show.
func (r *Registry) Check(ctx context.Context, userID uint64, name string, input json.RawMessage) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", &InputError{Message: "unknown action " + name}
	}
	if err := checkSchema(t.Schema, input); err != nil {
		return "", err
	}
	prep, err := t.prepare(ctx, userID, input)
	if err != nil {
		return "", err
	}
	return prep.summary, nil
}

// checkSchema applies the simple JSON Schema rules the tools use: required
// properties, enums, string lengths and patterns, integer bounds.
func checkSchema(schema, input json.RawMessage) error {
	var sc struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type      string   `json:"type"`
			Enum      []string `json:"enum"`
			MinLength *int     `json:"minLength"`
			MaxLength *int     `json:"maxLength"`
			Pattern   string   `json:"pattern"`
			Minimum   *float64 `json:"minimum"`
			Maximum   *float64 `json:"maximum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &sc); err != nil {
		return err
	}
	var in map[string]any
	if err := json.Unmarshal(input, &in); err != nil {
		return &InputError{Message: "invalid input: expected a JSON object"}
	}
	for _, k := range sc.Required {
		if _, ok := in[k]; !ok {
			return &InputError{Message: k + " is required"}
		}
	}
	for k, v := range in {
		p, ok := sc.Properties[k]
		if !ok {
			continue // decodeStrict rejects unknown fields
		}
		switch x := v.(type) {
		case string:
			n := len([]rune(x))
			switch {
			case p.MinLength != nil && n < *p.MinLength:
				return &InputError{Message: fmt.Sprintf("%s must be at least %d characters", k, *p.MinLength)}
			case p.MaxLength != nil && n > *p.MaxLength:
				return &InputError{Message: fmt.Sprintf("%s must be at most %d characters", k, *p.MaxLength)}
			case len(p.Enum) > 0 && !slices.Contains(p.Enum, x):
				return &InputError{Message: fmt.Sprintf("%s must be one of %s", k, strings.Join(p.Enum, ", "))}
			case p.Pattern != "" && !regexp.MustCompile(p.Pattern).MatchString(x):
				return &InputError{Message: k + " has the wrong format"}
			}
		case float64:
			switch {
			case p.Type == "integer" && x != float64(int64(x)):
				return &InputError{Message: k + " must be a whole number"}
			case p.Minimum != nil && x < *p.Minimum:
				return &InputError{Message: fmt.Sprintf("%s must be at least %g", k, *p.Minimum)}
			case p.Maximum != nil && x > *p.Maximum:
				return &InputError{Message: fmt.Sprintf("%s must be at most %g", k, *p.Maximum)}
			}
		}
	}
	return nil
}

// Specs describes the tools to the model.
func (r *Registry) Specs() []ToolSpec {
	specs := make([]ToolSpec, len(r.tools))
	for i, t := range r.tools {
		specs[i] = ToolSpec{Name: t.Name, Description: t.Description, InputSchema: t.Schema}
	}
	return specs
}
