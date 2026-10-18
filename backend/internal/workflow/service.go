package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
	"lifeos-ai/backend/internal/services"
)

const (
	maxAutomations = 50
	maxActions     = 5
)

// Service manages a user's automations.
type Service struct {
	DB       *sql.DB
	Settings repository.SettingsRepository
	// Tools is ai.AutomationTools; actions are checked against it on save.
	Tools *ai.Registry
	Now   func() time.Time // overridable for tests
}

// Input is a create or a patch; nil fields are left as they are.
type Input struct {
	Name       *string             `json:"name"`
	Enabled    *bool               `json:"enabled"`
	Trigger    *models.Trigger     `json:"trigger"`
	Conditions *[]models.Condition `json:"conditions"`
	Actions    *[]models.Action    `json:"actions"`
}

func (s Service) List(ctx context.Context, userID uint64) ([]models.Automation, error) {
	return repository.AutomationRepository{DB: s.DB}.List(ctx, userID)
}

func (s Service) Get(ctx context.Context, userID, id uint64) (models.Automation, error) {
	return repository.AutomationRepository{DB: s.DB}.Get(ctx, userID, id)
}

func (s Service) Create(ctx context.Context, userID uint64, in Input) (models.Automation, error) {
	return s.create(ctx, userID, in, nil)
}

// create makes an automation, from a built-in workflow if templateKey is set.
func (s Service) create(ctx context.Context, userID uint64, in Input, templateKey *string) (models.Automation, error) {
	missing := map[string]string{}
	if in.Name == nil {
		missing["name"] = "is required"
	}
	if in.Trigger == nil {
		missing["trigger"] = "is required"
	}
	if in.Actions == nil {
		missing["actions"] = "add at least one action"
	}
	if len(missing) > 0 {
		return models.Automation{}, &services.ValidationError{Fields: missing}
	}
	n, err := repository.AutomationRepository{DB: s.DB}.Count(ctx, userID)
	if err != nil {
		return models.Automation{}, err
	}
	if n >= maxAutomations {
		return models.Automation{}, &services.ValidationError{Fields: map[string]string{"name": fmt.Sprintf("you can have at most %d automations", maxAutomations)}}
	}
	a := models.Automation{UserID: userID, TemplateKey: templateKey, Enabled: true, Conditions: []models.Condition{}}
	apply(&a, in)
	if err := s.validate(ctx, &a); err != nil {
		return models.Automation{}, err
	}
	var id uint64
	err = repository.InTx(ctx, s.DB, func(db repository.DBTX) error {
		var err error
		if id, err = (repository.AutomationRepository{DB: db}).Create(ctx, a); err != nil {
			return err
		}
		a.ID = id
		return scheduleNext(ctx, db, a)
	})
	if err != nil {
		return models.Automation{}, err
	}
	return s.Get(ctx, userID, id)
}

func (s Service) Update(ctx context.Context, userID, id uint64, in Input) (models.Automation, error) {
	err := repository.InTx(ctx, s.DB, func(db repository.DBTX) error {
		automations := repository.AutomationRepository{DB: db}
		a, err := automations.GetForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		apply(&a, in)
		if err := s.validate(ctx, &a); err != nil {
			return err
		}
		if err := automations.Update(ctx, a); err != nil {
			return err
		}
		return scheduleNext(ctx, db, a)
	})
	if err != nil {
		return models.Automation{}, err
	}
	return s.Get(ctx, userID, id)
}

func (s Service) Delete(ctx context.Context, userID, id uint64) error {
	// A queued run finds the automation gone and does nothing.
	return repository.AutomationRepository{DB: s.DB}.Delete(ctx, userID, id)
}

func apply(a *models.Automation, in Input) {
	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if in.Trigger != nil {
		a.Trigger = *in.Trigger
		if in.Conditions == nil && a.Trigger.Type != in.Trigger.Type {
			a.Conditions = []models.Condition{} // they were for another trigger
		}
	}
	if in.Conditions != nil {
		a.Conditions = *in.Conditions
	}
	if in.Actions != nil {
		a.Actions = *in.Actions
	}
}

// validate checks and normalizes the automation and sets its next run.
func (s Service) validate(ctx context.Context, a *models.Automation) error {
	settings, err := s.Settings.Get(ctx, a.UserID)
	if err != nil {
		return err
	}
	loc, now := settings.Location(), s.now()
	errs := map[string]string{}

	if n := len([]rune(a.Name)); n == 0 || n > 100 {
		errs["name"] = "must be 1–100 characters"
	}
	spec, ok := triggerSpec(a.Trigger.Type)
	if !ok {
		errs["trigger.type"] = "unknown trigger"
	}
	if a.Trigger.Type == models.TriggerSchedule {
		for k, v := range validateSchedule(a.Trigger.Schedule, now, loc) {
			errs[k] = v
		}
		if a.Trigger.Schedule != nil {
			normalizeSchedule(a.Trigger.Schedule)
		}
	} else {
		a.Trigger.Schedule = nil
	}

	if ok {
		condErrs, conds := validateConditions(spec, a.Conditions)
		for k, v := range condErrs {
			errs[k] = v
		}
		if len(condErrs) == 0 {
			a.Conditions = make([]models.Condition, len(conds))
			for i, c := range conds {
				a.Conditions[i] = models.Condition{Field: c.Field, Op: c.Op, Value: c.Value}
			}
		}
		s.validateActions(ctx, a, spec, now, loc, errs)
	}
	if len(errs) > 0 {
		return &services.ValidationError{Fields: errs}
	}

	a.NextRunAt = nil
	if a.Enabled && a.Trigger.Type == models.TriggerSchedule {
		a.NextRunAt = NextRun(*a.Trigger.Schedule, loc, now)
	}
	return nil
}

// validateActions checks each action as it would run, with sample values in
// place of the event's data.
func (s Service) validateActions(ctx context.Context, a *models.Automation, spec TriggerSpec, now time.Time, loc *time.Location, errs map[string]string) {
	if len(a.Actions) == 0 || len(a.Actions) > maxActions {
		errs["actions"] = fmt.Sprintf("add 1–%d actions", maxActions)
		return
	}
	sample := sampleValues(spec.Placeholders)
	for i := range a.Actions {
		key := fmt.Sprintf("actions[%d]", i)
		act := &a.Actions[i]
		if !actionAllowed(act.Type) {
			errs[key] = fmt.Sprintf("unknown action %q", act.Type)
			continue
		}
		if len(act.Input) == 0 || string(act.Input) == "null" {
			act.Input = json.RawMessage(`{}`)
		}
		if len(act.Input) > 20000 {
			errs[key] = "input is too large"
			continue
		}
		resolved, err := resolveInput(act.Input, sample, now, loc)
		if err != nil {
			errs[key] = err.Error()
			continue
		}
		if _, err := s.Tools.Check(ctx, a.UserID, act.Type, resolved); err != nil {
			errs[key] = err.Error()
		}
	}
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// runPayload is the automation.run job's payload.
type runPayload struct {
	AutomationID uint64        `json:"automation_id"`
	FireAt       *time.Time    `json:"fire_at,omitempty"` // schedule runs
	Event        *events.Event `json:"event,omitempty"`   // event runs
}

// scheduleNext queues the run at a.NextRunAt, if any. The key includes the
// time, so saving again without changing it queues nothing new, and a run
// queued for an older time finds next_run_at moved on and does nothing.
func scheduleNext(ctx context.Context, db repository.DBTX, a models.Automation) error {
	if a.NextRunAt == nil {
		return nil
	}
	_, _, err := scheduler.Enqueue(ctx, db, scheduler.NewJob{
		UserID: &a.UserID, Kind: JobRun, RunAt: *a.NextRunAt,
		Key:     fmt.Sprintf("automation:%d:at:%d", a.ID, a.NextRunAt.UnixMilli()),
		Payload: runPayload{AutomationID: a.ID, FireAt: a.NextRunAt},
	})
	return err
}
