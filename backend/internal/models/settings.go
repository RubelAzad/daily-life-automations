package models

import "time"

// AI approval modes. High-impact actions (sending messages, spending money,
// deleting data, bookings) need approval in every mode.
const (
	ApprovalAlways         = "always"           // ask before every change
	ApprovalHighImpactOnly = "high_impact_only" // routine changes without asking
)

var ApprovalModes = []string{ApprovalAlways, ApprovalHighImpactOnly}

type UserSettings struct {
	UserID uint64 `json:"-"`
	// Timezone is an IANA name such as "Asia/Dhaka".
	Timezone          string `json:"timezone"`
	WeekStartsOn      int    `json:"week_starts_on"` // 0 Sunday, 1 Monday, 6 Saturday
	QuietHoursEnabled bool   `json:"quiet_hours_enabled"`
	QuietHoursStart   string `json:"quiet_hours_start"` // "HH:MM" local time
	QuietHoursEnd     string `json:"quiet_hours_end"`
	AIApprovalMode    string `json:"ai_approval_mode"`
	// Automatic AI briefings, at a local "HH:MM" time. Off by default.
	MorningBriefingEnabled bool      `json:"morning_briefing_enabled"`
	MorningBriefingTime    string    `json:"morning_briefing_time"`
	EveningSummaryEnabled  bool      `json:"evening_summary_enabled"`
	EveningSummaryTime     string    `json:"evening_summary_time"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// DefaultSettings are what an account without a settings row uses.
func DefaultSettings(userID uint64) UserSettings {
	return UserSettings{
		UserID:              userID,
		Timezone:            "UTC",
		WeekStartsOn:        1,
		QuietHoursStart:     "22:00",
		QuietHoursEnd:       "07:00",
		AIApprovalMode:      ApprovalAlways,
		MorningBriefingTime: "07:30",
		EveningSummaryTime:  "21:00",
	}
}

// Location is the user's time zone, or UTC if the stored name can't be
// loaded (it is validated on save, so this only guards against bad data).
func (s UserSettings) Location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil && s.Timezone != "Local" {
		return loc
	}
	return time.UTC
}
