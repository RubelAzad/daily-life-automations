package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"lifeos-ai/backend/internal/models"
)

type AIConversationRepository struct {
	DB DBTX
}

func (r AIConversationRepository) Create(ctx context.Context, userID uint64, title string) (models.AIConversation, error) {
	res, err := r.DB.ExecContext(ctx, `INSERT INTO ai_conversations (user_id, title) VALUES (?, ?)`, userID, title)
	if err != nil {
		return models.AIConversation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.AIConversation{}, err
	}
	return r.Get(ctx, userID, uint64(id))
}

func (r AIConversationRepository) Get(ctx context.Context, userID, id uint64) (models.AIConversation, error) {
	var c models.AIConversation
	err := r.DB.QueryRowContext(ctx,
		`SELECT id, user_id, title, created_at, updated_at FROM ai_conversations WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// List returns the most recently active conversations first.
func (r AIConversationRepository) List(ctx context.Context, userID uint64, limit, offset int) ([]models.AIConversation, int, error) {
	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_conversations WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT id, user_id, title, created_at, updated_at FROM ai_conversations
		  WHERE user_id = ? ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.AIConversation{}
	for rows.Next() {
		var c models.AIConversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (r AIConversationRepository) SetTitle(ctx context.Context, userID, id uint64, title string) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE ai_conversations SET title = ? WHERE id = ? AND user_id = ?`, title, id, userID)
	return err
}

func (r AIConversationRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM ai_conversations WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Messages returns a conversation's messages in order. Callers must have
// checked that the conversation belongs to the user.
func (r AIConversationRepository) Messages(ctx context.Context, conversationID uint64) ([]models.AIMessage, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, created_at FROM ai_messages WHERE conversation_id = ? ORDER BY id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AIMessage{}
	for rows.Next() {
		var (
			m       models.AIMessage
			content []byte
		)
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &content, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Content = rawJSON(content)
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMessage appends a message and marks the conversation as active.
func (r AIConversationRepository) AddMessage(ctx context.Context, conversationID uint64, role string, content json.RawMessage) (models.AIMessage, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO ai_messages (conversation_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		// The app's clock, like ai_actions.decided_at, so the two compare reliably.
		conversationID, role, string(content), time.Now().UTC())
	if err != nil {
		return models.AIMessage{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.AIMessage{}, err
	}
	if _, err := r.DB.ExecContext(ctx, `UPDATE ai_conversations SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, conversationID); err != nil {
		return models.AIMessage{}, err
	}
	var m models.AIMessage
	var raw []byte
	err = r.DB.QueryRowContext(ctx, `SELECT id, conversation_id, role, content, created_at FROM ai_messages WHERE id = ?`, id).
		Scan(&m.ID, &m.ConversationID, &m.Role, &raw, &m.CreatedAt)
	m.Content = rawJSON(raw)
	return m, err
}

// DeleteMessage removes one message (used to undo a turn that failed
// before anything happened).
func (r AIConversationRepository) DeleteMessage(ctx context.Context, conversationID, id uint64) error {
	_, err := r.DB.ExecContext(ctx, `DELETE FROM ai_messages WHERE id = ? AND conversation_id = ?`, id, conversationID)
	return err
}
