package scheduler

import (
	"errors"
	"math/rand"
	"time"

	"github.com/MaxHalford/eaopt"
)

type AttendeeId string

type Attendee struct {
	Id AttendeeId

	Calendar Calendar

	// The time the attendee prefers to take a pause between meetings.
	ExpectedPause time.Duration
}

type TimeInterval struct {
	// Inclusive.
	Start time.Time
	// Exclusive.
	End time.Time
}

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

type ScheduledEvent struct {
	TimeInterval
	Attendees []Attendee
	Room      Room
	Request   ScheduleRequest
}

type ScheduleRequest struct {
	Length        time.Duration
	Attendees     []Attendee
	PossibleRooms []Room
}

type CalendarEvent struct {
	TimeInterval
}

type Calendar interface {
	// Return a single overlapping event.
	Overlap(TimeInterval) (*CalendarEvent, bool, error)
}

type RoomId string

type Room struct {
	Id       RoomId
	Calendar Calendar
}

var DefaultNGenerations uint = 500

type Config func(*Scheduler)

func NGenerations(ngenerations uint) Config {
	return func(c *Scheduler) {
		c.ngenerations = ngenerations
	}
}

func New(earliest time.Time, reqs []ScheduleRequest, options ...Config) (*Scheduler, error) {
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

type Scheduler struct {
	ngenerations uint
	earliest     time.Time
	reqs         []ScheduleRequest
}

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

	// Find the minimum
	err = ga.Minimize(s.ScheduleFactory)
	if err != nil {
		return nil, err
	}

	// Assuming the first individual is the best -
	// https://godoc.org/github.com/MaxHalford/eaopt#GA isn't too well
	// documented.
	schedule, err := ga.HallOfFame[0].Genome.(*solution).Schedule()
	if err != nil {
		return nil, err
	}
	return schedule.Events, nil
}

// ScheduleFactory generates a viable schedule.
func (c *Scheduler) ScheduleFactory(rng *rand.Rand) eaopt.Genome {
	order := make([]int, len(c.reqs))
	for i := 0; i < len(c.reqs); i++ {
		order[i] = i
	}
	rng.Shuffle(len(order), func(i, j int) {
		order[i], order[j] = order[j], order[i]
	})
	return &solution{
		c.earliest,
		c.reqs,
		order,
	}
}

type solution struct {
	earliest time.Time
	reqs     []ScheduleRequest

	// This is the order we are optimizing for. We could in theory really
	// reorder reqs, but since eaopt requires that slices's interface{} content
	// is hashable we reorder ints which really are the indexes of reqs.
	order []int
}

func (s *solution) Clone() eaopt.Genome {
	return &solution{
		s.earliest,
		s.reqs,
		append([]int(nil), s.order...),
	}
}

func (s *solution) Crossover(genome eaopt.Genome, rng *rand.Rand) {
	// https://www.hindawi.com/journals/cin/2017/7430125/
	eaopt.CrossCXInt(s.order, genome.(*solution).order)
}

func (s *solution) Mutate(rng *rand.Rand) {
	// TODO: Test to see if more mutations than 1 should be done.
	eaopt.MutPermuteInt(s.order, 1, rng)
}

func (s *solution) Evaluate() (float64, error) {
	r, err := s.Schedule()
	return r.Evaluate(), err
}

type attendeeEvents struct {
	Attendee Attendee
	// Non-overlapping scheduled events. At least one element, always.
	Scheduled []ScheduledEvent
}

type constructedSchedule struct {
	Events           []ScheduledEvent
	earliest         time.Time
	eventsByAttendee map[AttendeeId]*attendeeEvents
}

const MaxIterations = 1000

func (c *constructedSchedule) Add(req ScheduleRequest) error {
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
		overlap, attendee, overlaps, err := c.findAttendeeOverlap(candidate)
		if err != nil {
			return err
		}
		if overlaps {
			candidate.Start = overlap.End.Add(attendee.ExpectedPause)
			candidate.End = candidate.Start.Add(req.Length)
			continue
		}

		// TODO: Investigate if we can do better room allocation. For example,
		// cost for switching room or cost for using a large room with few
		// people.
		busyRooms, nextTimeToTry := c.findAlreadyScheduledRooms(candidate.TimeInterval)
		room, found, err := c.findAvailableRoom(candidate, req, busyRooms)
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
			return errors.New("Too many iterations.")
		}
	}

	// We have found a time that works.

	c.Events = append(c.Events, candidate)
	for _, a := range req.Attendees {
		e, exists := c.eventsByAttendee[a.Id]
		if !exists {
			e = &attendeeEvents{
				Attendee: a,
			}
			c.eventsByAttendee[a.Id] = e
		}
		e.Scheduled = append(e.Scheduled, candidate)
	}

	return nil
}

func (c *constructedSchedule) findAlreadyScheduledRooms(ti TimeInterval) ([]Room, *time.Time) {
	m := make(map[RoomId]Room)
	var earliestStart *time.Time
	for _, event := range c.Events {
		// TODO: This loop can be optimized. We could iterate from the end and
		// once we are seeing events that end before ti we can stop iterating.

		if event.TimeInterval.Overlaps(ti) {
			if earliestStart == nil || event.Start.Before(*earliestStart) {
				earliestStart = &event.End
			}
			m[event.Room.Id] = event.Room
		}
	}

	rooms := make([]Room, 0, len(m))
	for _, v := range m {
		rooms = append(rooms, v)
	}
	return rooms, earliestStart
}

func (c *constructedSchedule) findAvailableRoom(se ScheduledEvent, req ScheduleRequest, excluded []Room) (*Room, bool, error) {
	lookup := make(map[RoomId]struct{})
	for _, r := range excluded {
		lookup[r.Id] = struct{}{}
	}

	for _, room := range req.PossibleRooms {
		if _, ignored := lookup[room.Id]; ignored {
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

func (c *constructedSchedule) findAttendeeOverlap(se ScheduledEvent) (*CalendarEvent, *Attendee, bool, error) {

	// Now we check if the user already has a meeting.

	for _, a := range se.Attendees {
		ev, overlaps, err := a.Calendar.Overlap(se.TimeInterval)
		if err != nil {
			return nil, nil, false, err
		}
		if overlaps {
			return ev, &a, true, nil
		}

		if _, exist := c.eventsByAttendee[a.Id]; exist {
			for _, scheduled := range c.eventsByAttendee[a.Id].Scheduled {
				// TODO: This loop can be optimized. We could iterate from the
				// end and once we are seeing events where
				// scheduled.End.Before(se.Start) we can stop iterating.

				if scheduled.TimeInterval.Overlaps(se.TimeInterval) {
					return &CalendarEvent{scheduled.TimeInterval}, &a, true, nil
				}
			}
		}
	}
	return nil, nil, false, nil
}

func latest(a time.Time, others ...time.Time) time.Time {
	for _, b := range others {
		if a.Before(b) {
			a = b
		}
	}
	return a
}

func (c constructedSchedule) Evaluate() float64 {
	var score time.Duration
	for _, attendee := range c.eventsByAttendee {
		// First event as early as possible.
		score += attendee.Scheduled[0].Start.Sub(c.earliest)

		// All events packed as tight as possible.
		for i, nextEvent := range attendee.Scheduled[1:] {
			curEvent := attendee.Scheduled[i]
			score += nextEvent.Start.Sub(curEvent.End) - attendee.Attendee.ExpectedPause
		}
	}

	// TODO: Convert to seconds to not work with giant numbers?
	return float64(score)
}

func (s *solution) Schedule() (constructedSchedule, error) {
	sch := constructedSchedule{
		earliest:         s.earliest,
		eventsByAttendee: make(map[AttendeeId]*attendeeEvents),
	}
	for _, event := range s.order {
		if err := sch.Add(s.reqs[event]); err != nil {
			return sch, err
		}
	}
	return sch, nil
}
