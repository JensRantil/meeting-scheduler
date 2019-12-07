package scheduler

import (
	"testing"
	"time"

	"github.com/k0kubun/pp"
)

func TestTimeIntervalOverlap(t *testing.T) {
	now := time.Now()
	t1 := TimeInterval{now.Add(0 * time.Minute), now.Add(60 * time.Minute)}
	t2 := TimeInterval{now.Add(30 * time.Minute), now.Add(90 * time.Minute)}
	t3 := TimeInterval{now.Add(60 * time.Minute), now.Add(90 * time.Minute)}

	if !t1.Overlaps(t2) {
		t.Error("expected t1 and t2 to overlap")
	}
	if t1.Overlaps(t3) {
		t.Error("expected t1 and t3 to not overlap")
	}
	if !t2.Overlaps(t3) {
		t.Error("expected t2 and t3 to not overlap")
	}
}

func TestOptimalSolutionEvaluation(t *testing.T) {
	emptyCalendar := FakeCalendar{}
	rooms := []Room{
		{"room-1", emptyCalendar},
	}
	attendees := []Attendee{
		{"christian", emptyCalendar},
		{"jens", emptyCalendar},
	}
	reqs := []ScheduleRequest{
		{60 * time.Minute, attendees, rooms},
	}

	// Monday morning at 9.
	now, _ := time.Parse("02-01-2006 15:04", "02-12-2019 09:00")
	scheduler, err := New(now, reqs)
	if err != nil {
		t.Fatal(err)
	}

	events, err := scheduler.Run()
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 1 {
		t.Error("Expected a single event scheduled:", events)
	}
	if s := events[0].Start; s != now {
		t.Error("Wrong start time. Expected:", now, "Was:", s)
	}
	if diff := events[0].End.Sub(events[0].Start); diff != reqs[0].Length {
		t.Error("Wrong event length. Expected:", reqs[0].Length, "Was:", diff)
	}
}

func TestPuttingEventsEarlierInTheWeekIsBetter(t *testing.T) {
	emptyCalendar := FakeCalendar{}
	rooms := []Room{
		{"room-1", emptyCalendar},
	}
	attendee1 := Attendee{"christian", emptyCalendar}
	attendee2 := Attendee{"jens", emptyCalendar}
	attendee3 := Attendee{"henrik", emptyCalendar}
	reqs := []ScheduleRequest{
		{60 * time.Minute, []Attendee{attendee1, attendee2}, rooms},
		{30 * time.Minute, []Attendee{attendee1, attendee2, attendee3}, rooms},
	}

	// Monday morning at 9.
	now, _ := time.Parse("02-01-2006 15:04", "02-12-2019 09:00")
	scheduler, err := New(now, reqs)
	if err != nil {
		t.Fatal(err)
	}

	events, err := scheduler.Run()
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Error("Expected a single event scheduled:", events)
	}
	if s := events[0].Start; s != now {
		t.Error("Wrong start time. Expected:", now, "Was:", s)
	}
	if events[0].Request.Length != reqs[1].Length {
		t.Error("Expected second request to be prioritized before the first. Order:", pp.Sprint(events))
	}
	if events[1].Request.Length != reqs[0].Length {
		t.Error("Expected second request to be prioritized before the first. Order:", pp.Sprint(events))
	}
	for _, event := range events {
		checkEvent(t, event)
	}
}

func TestSchedulingOfSolution(t *testing.T) {
	emptyCalendar := FakeCalendar{}
	rooms := []Room{
		{"room-1", emptyCalendar},
	}
	attendee1 := Attendee{"a", emptyCalendar}
	attendee2 := Attendee{"b", emptyCalendar}
	attendee3 := Attendee{"c", emptyCalendar}
	attendee4 := Attendee{"d", emptyCalendar}
	attendee5 := Attendee{"e", emptyCalendar}
	reqs := []ScheduleRequest{
		{15 * time.Minute, []Attendee{attendee1, attendee2}, rooms},
		{60 * time.Minute, []Attendee{attendee5, attendee1}, rooms},
		{30 * time.Minute, []Attendee{attendee3, attendee4}, rooms},
	}

	// Monday morning at 9.
	now, _ := time.Parse("02-01-2006 15:04", "02-12-2019 09:00")

	sol := solution{
		now,
		reqs,
		[]int{0, 1, 2},
	}

	schedule, err := sol.Schedule()
	if err != nil {
		t.Fatal(err)
	}
	expected := []TimeInterval{
		{
			now,
			now.Add(reqs[0].Length),
		},
		{
			now.Add(reqs[0].Length),
			now.Add(reqs[0].Length).Add(reqs[1].Length),
		},
		{
			// Not adding any additional pause time here because attendee 3 & 4
			// didn't have a meeting previously.
			now.Add(reqs[0].Length).Add(reqs[1].Length),
			now.Add(reqs[0].Length).Add(reqs[1].Length).Add(reqs[2].Length),
		},
	}
	for i, e := range expected {
		if schedule.Events[i].TimeInterval != e {
			t.Errorf("Unexpected event on index %d.\nExpected:\n%s\nWas:\n%s", i, pp.Sprint(e), pp.Sprint(schedule.Events[i].TimeInterval))
		}
	}
}

func checkEvent(t *testing.T, event ScheduledEvent) {
	if diff := event.End.Sub(event.Start); diff != event.Request.Length {
		t.Error("Wrong event length. Expected:", event.Request.Length, "Was:", diff)
	}

}

type FakeCalendar []TimeInterval

func (f FakeCalendar) Overlap(interval TimeInterval) (*CalendarEvent, bool, error) {
	for _, ti := range f {
		if ti.End.Before(ti.Start) {
			panic("incorrect time interval")
		}
		if !(ti.End.Before(interval.Start) || interval.Start.Before(ti.End)) {
			return &CalendarEvent{ti}, true, nil
		}

	}
	return nil, false, nil
}
