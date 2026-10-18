package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"lifeos-ai/backend/internal/models"
)

// AutomationTools is the allowlist for automation actions: the routine,
// easy-to-undo tools, plus notify. Nothing that deletes, sends or spends.
func AutomationTools(d Deps) *Registry {
	return NewRegistry(
		notifyTool(d), createTaskTool(d), completeTaskTool(d), rescheduleTaskTool(d), createNoteTool(d), createReminderTool(d),
	)
}

type notifyInput struct {
	Title string  `json:"title"`
	Body  *string `json:"body"`
	Link  *string `json:"link"`
}

// notifyTool sends the user an in-app notification. It changes none of the
// user's data, so like a lookup it never needs approval.
func notifyTool(d Deps) Tool {
	return define("notify",
		"Send the user an in-app notification. link is an app path such as /tasks.",
		RiskRead, `{"type":"object","properties":{
			"title":{"type":"string","minLength":1,"maxLength":255},
			"body":{"type":"string","maxLength":1000},
			"link":{"type":"string","maxLength":255,"pattern":"^/"}
		},"required":["title"],"additionalProperties":false}`,
		func(_ context.Context, _ uint64, in notifyInput) string {
			return "Notify: " + quote(in.Title)
		},
		func(ctx context.Context, userID uint64, in notifyInput) (any, error) {
			in.Title = strings.TrimSpace(in.Title)
			switch {
			case in.Title == "" || len([]rune(in.Title)) > 255:
				return nil, &InputError{Message: "title must be 1–255 characters"}
			case in.Body != nil && len([]rune(*in.Body)) > 1000:
				return nil, &InputError{Message: "body must be at most 1000 characters"}
			case in.Link != nil && !safeAppPath(*in.Link):
				return nil, &InputError{Message: "link must be a path in the app, such as /tasks"}
			}
			key := make([]byte, 12)
			_, _ = rand.Read(key)
			n := models.Notification{
				UserID: userID, Kind: "automation", Title: in.Title, Body: in.Body, Link: in.Link, Priority: "normal",
				IdempotencyKey: "automation:" + hex.EncodeToString(key),
			}
			if _, err := d.Notifications.Insert(ctx, n); err != nil {
				return nil, err
			}
			return map[string]any{"title": in.Title}, nil
		})
}

// safeAppPath allows "/tasks" or "/notes?id=3", but not "//evil.com" or
// "/\evil.com", which browsers treat as other sites.
func safeAppPath(p string) bool {
	return len(p) <= 255 && strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.ContainsAny(p, "\\ \t\r\n")
}
