package services

import (
	"context"
	"errors"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

type TaskListService struct {
	Lists repository.TaskListRepository
}

type CreateTaskListInput struct {
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

type UpdateTaskListInput struct {
	Name     Optional[string] `json:"name"`
	Color    Optional[string] `json:"color"`
	Position Optional[int]    `json:"position"`
}

// defaultLists are created for every new account (module 2: personal, work,
// family and shopping lists).
var defaultLists = []struct{ name, color string }{
	{"Personal", "#3b82f6"},
	{"Work", "#8b5cf6"},
	{"Family", "#10b981"},
	{"Shopping", "#f59e0b"},
}

func (s TaskListService) CreateDefaults(ctx context.Context, user models.User) error {
	for _, d := range defaultLists {
		color := d.color
		if _, err := s.Lists.Create(ctx, user.ID, d.name, &color); err != nil && !errors.Is(err, repository.ErrDuplicate) {
			return err
		}
	}
	return nil
}

func (s TaskListService) List(ctx context.Context, userID uint64) ([]models.TaskList, error) {
	return s.Lists.List(ctx, userID)
}

func (s TaskListService) Create(ctx context.Context, userID uint64, in CreateTaskListInput) (models.TaskList, error) {
	var c checker
	name := c.requiredText("name", in.Name, 100)
	color := c.color("color", in.Color)
	if err := c.err(); err != nil {
		return models.TaskList{}, err
	}
	l, err := s.Lists.Create(ctx, userID, name, color)
	return l, duplicateName(err)
}

func (s TaskListService) Update(ctx context.Context, userID, id uint64, in UpdateTaskListInput) (models.TaskList, error) {
	l, err := s.Lists.Get(ctx, userID, id)
	if err != nil {
		return l, err
	}
	var c checker
	if in.Name.Set {
		l.Name = c.requiredText("name", in.Name.Value, 100)
	}
	if in.Color.Set {
		l.Color = c.color("color", in.Color.Ptr())
	}
	if in.Position.Set {
		if in.Position.Null || in.Position.Value < 0 {
			c.fail("position", "must be zero or greater")
		}
		l.Position = in.Position.Value
	}
	if err := c.err(); err != nil {
		return models.TaskList{}, err
	}
	l, err = s.Lists.Update(ctx, l)
	return l, duplicateName(err)
}

func (s TaskListService) Delete(ctx context.Context, userID, id uint64) error {
	return s.Lists.Delete(ctx, userID, id)
}

func duplicateName(err error) error {
	if errors.Is(err, repository.ErrDuplicate) {
		return &ValidationError{Fields: map[string]string{"name": "a list with this name already exists"}}
	}
	return err
}
