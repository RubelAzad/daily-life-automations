package ai

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// maxStoredJSON caps what the audit log keeps of a tool's input or output.
const maxStoredJSON = 8 << 10

var (
	// sensitiveKeys are field names whose values are never stored. Keys are
	// compared lower-case with "_" and "-" removed.
	sensitiveKeys = map[string]bool{
		"password": true, "passcode": true, "secret": true, "token": true, "apikey": true,
		"accesstoken": true, "refreshtoken": true, "authorization": true, "cookie": true,
		"cvv": true, "cvc": true, "pin": true, "cardnumber": true, "iban": true, "ssn": true,
	}
	apiKeyPattern = regexp.MustCompile(`\b(sk-ant-[A-Za-z0-9_\-]{8,}|sk-[A-Za-z0-9]{20,})`)
	// 13–19 digits, optionally grouped by spaces or dashes; checked with Luhn.
	cardPattern = regexp.MustCompile(`\b\d(?:[ \-]?\d){12,18}\b`)
)

// RedactJSON returns a copy of raw with secrets masked: values of sensitive
// fields, API keys and payment card numbers. Invalid JSON is stored as a
// redacted string. The result is capped at maxStoredJSON.
func RedactJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		v = RedactText(string(raw))
	}
	out, err := json.Marshal(redactValue(v))
	if err != nil {
		return nil
	}
	return capJSON(out)
}

// RedactText masks API keys and card numbers in free text.
func RedactText(s string) string {
	s = apiKeyPattern.ReplaceAllString(s, "[key removed]")
	return cardPattern.ReplaceAllStringFunc(s, func(m string) string {
		if luhn(m) {
			return "[card number removed]"
		}
		return m
	})
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			norm := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(k))
			if sensitiveKeys[norm] || strings.HasSuffix(norm, "password") || strings.HasSuffix(norm, "secret") || strings.HasSuffix(norm, "token") {
				t[k] = "[redacted]"
			} else {
				t[k] = redactValue(val)
			}
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactValue(t[i])
		}
		return t
	case string:
		return RedactText(t)
	default:
		return v
	}
}

func capJSON(b []byte) json.RawMessage {
	if len(b) <= maxStoredJSON {
		return b
	}
	preview := []rune(string(b))
	if len(preview) > 2000 {
		preview = preview[:2000]
	}
	out, _ := json.Marshal(map[string]any{"truncated": true, "bytes": len(b), "preview": string(preview)})
	return out
}

func luhn(s string) bool {
	sum, n := 0, 0
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if n%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		n++
	}
	return n >= 13 && sum%10 == 0
}
