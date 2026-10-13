package core

import (
	"context"
	"time"
	_ "time/tzdata" // The reminder contract also works where the OS has no zone database.
)

// taskCalendar implements docs/contract/planning.md. emit bounds allocation at its caller.
func taskCalendar(ctx context.Context, in TaskSpec, from, through string, emit func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkWindow(from, through); err != nil {
		return err
	}
	if err := validateTaskSchedule(in); err != nil {
		return err
	}
	if in.RepeatUnit == "" {
		return nil
	}
	if in.RepeatUntilDay != "" && in.RepeatUntilDay < through {
		through = in.RepeatUntilDay
	}
	if through < from || through < in.AnchorDay {
		return nil
	}
	anchor, _ := time.Parse(time.DateOnly, in.AnchorDay)
	lo, _ := time.Parse(time.DateOnly, max(from, in.AnchorDay))
	hi, _ := time.Parse(time.DateOnly, through)
	var distance, ceiling int64
	switch in.RepeatUnit {
	case "day", "week":
		distance, ceiling = (lo.Unix()-anchor.Unix())/86400, (hi.Unix()-anchor.Unix())/86400
		if in.RepeatUnit == "week" {
			distance /= 7
			ceiling /= 7
		}
	case "month":
		distance = int64((lo.Year()-anchor.Year())*12 + int(lo.Month()-anchor.Month()))
		ceiling = int64((hi.Year()-anchor.Year())*12 + int(hi.Month()-anchor.Month()))
	case "year":
		distance, ceiling = int64(lo.Year()-anchor.Year()), int64(hi.Year()-anchor.Year())
	}
	// offset is bounded by the calendar before it is multiplied or added.
	for offset := distance / in.RepeatEvery * in.RepeatEvery; offset <= ceiling; {
		if err := ctx.Err(); err != nil {
			return err
		}
		var candidate time.Time
		switch in.RepeatUnit {
		case "day":
			candidate = anchor.AddDate(0, 0, int(offset))
		case "week":
			candidate = anchor.AddDate(0, 0, int(offset*7))
		case "month":
			candidate = taskMonth(anchor, int(offset))
		case "year":
			candidate = taskMonth(anchor, int(offset)*12)
		}
		key := candidate.Format(time.DateOnly)
		if key >= from && key <= through {
			if err := emit(key); err != nil {
				return err
			}
		}
		if in.RepeatEvery > ceiling-offset {
			break
		}
		offset += in.RepeatEvery
	}
	return nil
}

func taskMonth(anchor time.Time, offset int) time.Time {
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(offset), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(anchor.Day(), last)-1)
}

// validateTaskSchedule guards the expansion of taskCalendar, which a test may call with a spec no row holds; a
// written spec is held to tasks_recurrence, tasks_repeat_unit and tasks_repeat_every.
func validateTaskSchedule(in TaskSpec) error {
	if in.RepeatUnit == "" {
		if in.RepeatEvery != 0 || in.AnchorDay != "" || in.RepeatUntilDay != "" {
			return Invalid("a one-off task cannot have recurrence fields")
		}
		return nil
	}
	if in.RepeatUnit != "day" && in.RepeatUnit != "week" && in.RepeatUnit != "month" && in.RepeatUnit != "year" {
		return Invalid("unknown task recurrence unit")
	}
	if in.RepeatEvery < 1 || !IsDay(in.AnchorDay) || in.RepeatUntilDay != "" && !IsDay(in.RepeatUntilDay) {
		return Invalid("task recurrence needs a positive interval and exact days")
	}
	return nil
}

// TaskReminder distinguishes absent intent from a clock that cannot be resolved.
type TaskReminder struct {
	State string `json:"state"` // none, resolved or unresolved
	At    string `json:"at,omitempty"`
}

// ResolveTaskReminder resolves intent only; it never schedules or sends a notification. The task and the occurrence
// are rows, so their clock, zone and mode are as the DDL admits them.
func ResolveTaskReminder(task Task, occurrence TaskOccurrence) (TaskReminder, error) {
	if task.DeletedAt != "" || occurrence.DeletedAt != "" || occurrence.State != StateOpen || occurrence.ReminderMode == ReminderOff {
		return TaskReminder{State: "none"}, nil
	}
	if occurrence.ReminderMode == ReminderAt {
		return TaskReminder{State: "resolved", At: occurrence.ReminderOverrideAt}, nil
	}
	if occurrence.DueDay == "" || task.ReminderLocalTime == "" && task.ReminderZone == "" {
		return TaskReminder{State: "none"}, nil
	}
	at, ok := resolveTaskClock(occurrence.DueDay, task.ReminderLocalTime, task.ReminderZone)
	if !ok {
		return TaskReminder{State: "unresolved"}, nil
	}
	return TaskReminder{State: "resolved", At: at}, nil
}

func resolveTaskClock(day, clock, zone string) (string, bool) {
	if zone == "Local" {
		return "", false
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return "", false
	}
	civil, err := time.Parse("2006-01-02T15:04", day+"T"+clock)
	if err != nil {
		return "", false
	}
	// Examine zone intervals, not time.Date's unspecified fold/gap selection.
	// IANA offsets fit within a day; the wider window also includes a date skip.
	start, end := civil.Add(-48*time.Hour), civil.Add(48*time.Hour)
	var earliest, shifted time.Time
	var found, haveShift bool
	for cursor, steps := start, 0; cursor.Before(end) && steps < 128; steps++ {
		local := cursor.In(location)
		_, offset := local.Zone()
		if offset <= -86400 || offset >= 86400 {
			return "", false
		}
		_, next := local.ZoneBounds()
		if !next.IsZero() && !next.After(cursor) {
			// Go's TZ-footer calculation uses a 365-day year for this bound:
			// https://github.com/golang/go/issues/81583. Correct only that
			// synthetic leap-year endpoint; other broken bounds stay unresolved.
			u := cursor.UTC()
			lastDay := time.Date(u.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
			if u.YearDay() != 366 || !next.Equal(lastDay) {
				return "", false
			}
			next = lastDay.AddDate(0, 0, 1)
		}
		stop := end
		if !next.IsZero() && next.Before(stop) {
			stop = next
		}
		candidate := time.Unix(civil.Unix()-int64(offset), 0).UTC()
		if !candidate.Before(cursor) && candidate.Before(stop) && candidate.In(location).Format("2006-01-02T15:04:05") == day+"T"+clock+":00" {
			if !found || candidate.Before(earliest) {
				earliest = candidate
				found = true
			}
		}
		if next.IsZero() || !next.Before(end) {
			break
		}
		_, after := next.In(location).Zone()
		if after > offset {
			gapStart, gapEnd := next.Unix()+int64(offset), next.Unix()+int64(after)
			if civil.Unix() >= gapStart && civil.Unix() < gapEnd {
				midnight, _ := time.Parse(time.DateOnly, day)
				if gapStart <= midnight.Unix() && gapEnd >= midnight.Unix()+86400 {
					return "", false
				}
				shifted = time.Unix(civil.Unix()-int64(offset), 0).UTC()
				haveShift = true
				want := civil.Add(time.Duration(after-offset) * time.Second)
				if shifted.In(location).Format("2006-01-02T15:04:05") != want.Format("2006-01-02T15:04:05") {
					return "", false
				}
			}
		}
		cursor = next
	}
	if !found {
		earliest = shifted
		found = haveShift
	}
	if !found || earliest.Year() < 0 || earliest.Year() > 9999 {
		return "", false
	}
	return earliest.Format(instantLayout), true
}
