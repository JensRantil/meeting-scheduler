// Package scheduler is a meeting scheduler that tries to schedule meetings as
// early and as packed as possible in the workweek.
//
// Imagine you want to have a meeting with someone, but it's not urgent and can
// wait until next week. You open up a web interface where you enter "I want to
// have a meeting with Eric for 30 minutes". At Sunday night a meeting
// scheduler will take all queued meeting requests and schedule the events 1)
// as early as possible in the week to allow the people to be productive for
// the rest of the week and 2) as close as possible to other meetings to avoid
// the attendees to have fragmented days where time by an actual computer is
// only for 30 minutes of which they are getting nothing done.
//
// `meeting-scheduler` is a library that will do the scheduling of the above.
// The scheduling is an NP-complete problem, so this library uses a heuristical
// approach to finding an optimal schedule - a genetic algorithm backed by the
// excellent `eaopt` (https://www.github.com/MaxHalford/eaopt) library.
//
// This library was initially developed during a Tink (https://www.tink.se)
// hackathon.
package scheduler

import (
	"errors"
	"math/rand"
	"time"

	"github.com/MaxHalford/eaopt"
)

// AttendeeID is a unique identifier for a meeting attendee.
type AttendeeID string

// Attendee is an attendee that attends a meeting.
type Attendee struct {
	// Id is the unique identifier for an attendee.
	ID AttendeeID
	// Calendar is an instance of the attendee's calendar containing previously
	// scheduled meetings.
	Calendar Calendar
}

// TimeInterval holds an interval of time.
type TimeInterval struct {
	// Inclusive. Must be strictly before End.
	Start time.Time
	// Exclusive. Must be strictly after Start.
	End time.Time
}

// Overlaps checks if this interval overlaps with another interval.
func (ti TimeInterval) Overlaps(ti2 TimeInterval) bool {
	if ti.End.Before(ti2.Start) {
		return false
	}
	if ti2.End.Before(ti.Start) {
		return false
	}
	if ti.End == ti2.Start {
		return false
	}
	if ti2.End == ti.Start {
		return false
	}
	return true
}

// ScheduledEvent is an event which has been scheduled with a fixed time and
// rooms. It is the scheduled equivalent of a ScheduleRequest.
type ScheduledEvent struct {
	// TimeInterval is the time over which this event has been scheduled.
	TimeInterval
	// Attendees is a list of the attendees for this meeting.
	Attendees []Attendee
	// Room is the room in which the event will take place.
	Room Room
	// Request is the equivalent ScheduleRequest that generated this
	// ScheduledEvent.
	Request *ScheduleRequest
}

// ScheduleRequest is the input the scheduling. It's a request to schedule a
// meeting.
type ScheduleRequest struct {
	// Length is the requested length of the meeting.
	Length time.Duration
	// Attendees is a list of the attendees of the meeting.
	Attendees []Attendee
	// PossibleRooms is a list of the possible rooms in which the meetings can
	// take place. If you have multiple offices you might want to limit which
	// rooms a meeting can take place in.
	PossibleRooms []Room
}

// CalendarEvent is an event stored in a calendar.
type CalendarEvent struct {
	TimeInterval
}

// Calendar is an external calendar source.
type Calendar interface {
	// Overlap checks if a TimeInterval overlaps with a preexisting event in a
	// Calendar.
	Overlap(TimeInterval) (*CalendarEvent, bool, error)
}

// RoomID is a unique id for a room.
type RoomID string

// Room is a meeting room that can be booked.
type Room struct {
	// Is is a unique id for a room. It is mostly needed to be able to work
	// around the fact that Room isn't hashable and can't be stored in a map.
	ID RoomID
	// Calendar is the calendar of the room.
	Calendar Calendar
}

// DefaultNGenerations is the number of generations that the genetic algorithm
// should execute before it's done. Increase this if your scheduling problem
// hasn't converged.
var DefaultNGenerations uint = 500

// Config is an optional configuration to a Scheduler.
type Config func(*Scheduler)

// NGenerations is an optional configuration option which changes the number of
// iterations that the genetic algorithm does.
func NGenerations(ngenerations uint) Config {
	return func(c *Scheduler) {
		c.ngenerations = ngenerations
	}
}

// New instantiates a new meeting scheduler that tries to schedule meeting
// requests, reqs, as close as possible to earliest which also minimizing
// attendee calendar fragmentation (that is, an attendee has a break of 45
// minutes between meetings).
//
// If you'd like your attendees to have pauses between their meetings, simulate
// that in Calendar.Overlap.
func New(earliest time.Time, reqs []*ScheduleRequest, options ...Config) (*Scheduler, error) {
	s := Scheduler{
		DefaultNGenerations,
		earliest,
		reqs,
	}
	for _, o := range options {
		o(&s)
	}
	return &s, nil
}

// Scheduler is a meeting scheduler that schedules meetings.
type Scheduler struct {
	ngenerations uint
	earliest     time.Time
	reqs         []*ScheduleRequest
}

// Run executes scheduling of meetings.
func (s *Scheduler) Run() ([]ScheduledEvent, error) {
	// Instantiate a GA with a GAConfig
	ga, err := eaopt.NewDefaultGAConfig().NewGA()
	if err != nil {
		return nil, err
	}

	// Set the number of generations to run for
	ga.NGenerations = s.ngenerations

	// Add a custom print function to track progress
	// TODO: Make this callback(ish) be definable as an Config option.
	/*ga.Callback = func(ga *eaopt.GA) {
		fmt.Printf("Best fitness at generation %d: %f\n", ga.Generations, ga.HallOfFame[0].Fitness)
	}*/

	// TODO: Stop early if no progress is being made.

	// Find the minimum
	err = ga.Minimize(s.ScheduleFactory)
	if err != nil {
		return nil, err
	}

	// Assuming the first individual is the best -
	// https://godoc.org/github.com/MaxHalford/eaopt#GA isn't too well
	// documented.
	schedule, err := ga.HallOfFame[0].Genome.(*candidate).Schedule()
	if err != nil {
		return nil, err
	}
	return schedule.Events, nil
}

// ScheduleFactory generates a viable schedule candidate.
func (c *Scheduler) ScheduleFactory(rng *rand.Rand) eaopt.Genome {
	order := make([]int, len(c.reqs))
	for i := 0; i < len(c.reqs); i++ {
		order[i] = i
	}
	rng.Shuffle(len(order), func(i, j int) {
		order[i], order[j] = order[j], order[i]
	})
	return &candidate{
		c.earliest,
		c.reqs,
		order,
	}
}

// candidate is the internal representation of a schedule. A "schedule" here
// depicts "a set of scheduled meetings". The candidate might not be the
// optimal schedule. candidate implements `eaopt.Genome`.
type candidate struct {
	earliest time.Time
	reqs     []*ScheduleRequest

	// This is the order we are optimizing for. We could in theory really
	// reorder reqs, but since eaopt requires that slices's interface{} content
	// is hashable we reorder ints which really are the indexes of reqs.
	order []int
}

// Clone makes a copy of a candidate.
func (s *candidate) Clone() eaopt.Genome {
	return &candidate{
		s.earliest,
		s.reqs,
		append([]int(nil), s.order...),
	}
}

// Crossover modified this candidate based on genome. You can see it as two
// solutions mating (and one parent, weirdly, being replaced by its child).
func (s *candidate) Crossover(genome eaopt.Genome, rng *rand.Rand) {
	// https://www.hindawi.com/journals/cin/2017/7430125/
	eaopt.CrossCXInt(s.order, genome.(*candidate).order)
}

// Mutate makes random changes to this candidate.
func (s *candidate) Mutate(rng *rand.Rand) {
	// TODO: Test to see if more mutations than 1 should be done.
	eaopt.MutPermuteInt(s.order, 1, rng)
}

// Evaluate evaluates how good a candidate performs. Lower is better.
func (s *candidate) Evaluate() (float64, error) {
	r, err := s.Schedule()
	return r.Evaluate(), err
}

type attendeeEvents struct {
	Attendee Attendee
	// Non-overlapping scheduled events. At least one element, always.
	Scheduled []ScheduledEvent
}

// constructedSchedule is a solution's `ScheduledEvent`s
type constructedSchedule struct {
	// ScheduledEvent is a list of all events with actual times.
	Events []ScheduledEvent
	// earliest time is that same as Scheduler.earliest.
	earliest time.Time
	// eventsByAttendee contains `ScheduledEvent`s grouped by attendee. It's
	// used as a lookup table to more quickly be able to evaluate how well the
	// solution performs.
	eventsByAttendee map[AttendeeID]*attendeeEvents
}

// MaxIterations is the number of iterations we allow before we consider we are
// stuck in a loop trying to schedule a ScheduleRequest. The code is complex.
// This avoids deadlock.
const MaxIterations = 1000

// Add schedules a single ScheduleRequest. It does so by starting on
// constructedSchedule.earliest and moving forward until it find an empty slot.
func (c *constructedSchedule) Add(req *ScheduleRequest) error {
	candidate := ScheduledEvent{
		TimeInterval: TimeInterval{
			c.earliest,
			c.earliest.Add(req.Length),
		},
		Attendees: req.Attendees,
		Request:   req,
	}

	iterations := 0
	for {
		// TODO: Attendee already has meeting better name?
		overlap, overlaps, err := c.findAttendeeOverlap(candidate)
		if err != nil {
			return err
		}
		if overlaps {
			candidate.Start = overlap.End
			candidate.End = candidate.Start.Add(req.Length)
			continue
		}

		// TODO: Investigate if we can do better room allocation. For example,
		// cost for switching room or cost for using a large room with few
		// people.
		busyRooms, nextTimeToTry := c.findAlreadyScheduledRooms(candidate.TimeInterval)
		room, found, err := c.findAvailableRoom(candidate, busyRooms)
		if err != nil {
			return err
		}
		if found {
			candidate.Room = *room
			break
		} else {
			candidate.Start = *nextTimeToTry
			candidate.End = candidate.Start.Add(req.Length)
		}

		iterations++
		if iterations > MaxIterations {
			return errors.New("too many iterations")
		}
	}

	// We have found a time that works.

	c.Events = append(c.Events, candidate)
	for _, a := range req.Attendees {
		e, exists := c.eventsByAttendee[a.ID]
		if !exists {
			e = &attendeeEvents{
				Attendee: a,
			}
			c.eventsByAttendee[a.ID] = e
		}
		e.Scheduled = append(e.Scheduled, candidate)
	}

	return nil
}

// findAlreadyScheduledRooms returns a list of rooms that are already scheduled
// over time interval ti. It also returns the earliest end timestamp for a busy
// room which is used to know next time we should try to reschedule.
func (c *constructedSchedule) findAlreadyScheduledRooms(ti TimeInterval) ([]Room, *time.Time) {
	m := make(map[RoomID]Room)
	var earliestEnd *time.Time
	for _, event := range c.Events {
		// TODO: This loop can be optimized. We could iterate from the end and
		// once we are seeing events that end before ti we can stop iterating.

		if event.TimeInterval.Overlaps(ti) {
			if earliestEnd == nil || event.End.Before(*earliestEnd) {
				earliestEnd = &event.End
			}
			m[event.Room.ID] = event.Room
		}
	}

	rooms := make([]Room, 0, len(m))
	for _, v := range m {
		rooms = append(rooms, v)
	}
	return rooms, earliestEnd
}

// findAvailableRoom returns the first available room it finds which isn't being
// used over time interval ti, and isn't part of excluded rooms.
func (c *constructedSchedule) findAvailableRoom(se ScheduledEvent, excluded []Room) (*Room, bool, error) {
	lookup := make(map[RoomID]struct{})
	for _, r := range excluded {
		lookup[r.ID] = struct{}{}
	}

	for _, room := range se.Request.PossibleRooms {
		if _, ignored := lookup[room.ID]; ignored {
			continue
		}

		_, overlaps, err := room.Calendar.Overlap(se.TimeInterval)
		if err != nil {
			return nil, false, err
		}
		if !overlaps {
			return &room, true, nil
		}
	}

	return nil, false, nil
}

// findAttendeeOverlap finds the attendees which are busy during the proposed time interval.
func (c *constructedSchedule) findAttendeeOverlap(se ScheduledEvent) (*CalendarEvent, bool, error) {

	// Now we check if the user already has a meeting.

	for _, a := range se.Attendees {
		ev, overlaps, err := a.Calendar.Overlap(se.TimeInterval)
		if err != nil {
			return nil, false, err
		}
		if overlaps {
			return ev, true, nil
		}

		if _, exist := c.eventsByAttendee[a.ID]; exist {
			for _, scheduled := range c.eventsByAttendee[a.ID].Scheduled {
				// TODO: This loop can be optimized. We could iterate from the
				// end and once we are seeing events where
				// scheduled.End.Before(se.Start) we can stop iterating.

				if scheduled.TimeInterval.Overlaps(se.TimeInterval) {
					return &CalendarEvent{scheduled.TimeInterval}, true, nil
				}
			}
		}
	}
	return nil, false, nil
}

// latest returns the latest time among a set of times.
func latest(a time.Time, others ...time.Time) time.Time {
	for _, b := range others {
		if a.Before(b) {
			a = b
		}
	}
	return a
}

// Evaluate evaluates how good a constructedSchedule performs. Attendees that
// start their days late with meetings and/or attendees that have fragmented
// days incur higher costs. That is, lower is better.
func (c constructedSchedule) Evaluate() float64 {
	var score time.Duration
	for _, attendee := range c.eventsByAttendee {
		// First event as early as possible.
		score += attendee.Scheduled[0].Start.Sub(c.earliest)

		// All events packed as tight as possible.
		for i, nextEvent := range attendee.Scheduled[1:] {
			curEvent := attendee.Scheduled[i]
			score += nextEvent.Start.Sub(curEvent.End)
		}
	}

	// TODO: Convert to seconds to not work with giant numbers?
	return float64(score)
}

// Schedule constructs a constructedSchedule from a candidate. It does this by
// laying out each ScheduleRequest one by one on each attendees "virtual
// calendar".
func (s *candidate) Schedule() (constructedSchedule, error) {
	sch := constructedSchedule{
		earliest:         s.earliest,
		eventsByAttendee: make(map[AttendeeID]*attendeeEvents),
	}
	for _, event := range s.order {
		if err := sch.Add(s.reqs[event]); err != nil {
			return sch, err
		}
	}
	return sch, nil
}
