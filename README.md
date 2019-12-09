Meeting Scheduler
=================
[![GoDoc](https://godoc.org/github.com/JensRantil/meeting-scheduler?status.svg)](https://godoc.org/github.com/JensRantil/meeting-scheduler)

Imagine you want to have a meeting with someone, but it's not urgent and can
wait until next week. You open up a web interface where you enter "I want to
have a meeting with Eric for 30 minutes". At Sunday night a meeting scheduler
will take all queued meeting requests and schedule the events

 * as early as possible in the week to allow the people to be productive for
   the rest of the week.
 * as close as possible to other meetings to avoid the attendees to have
   fragmented days where time by an actual computer is only for 30 minutes of
   which they are getting nothing done.

`meeting-scheduler` is a library that will do the scheduling of the above. The
scheduling is an NP-complete problem, so this library uses a heuristical
approach to finding an optimal schedule - a genetic algorithm backed by the
excellent [`eaopt`](https://www.github.com/MaxHalford/eaopt) library.

This library was initially developed during a [Tink](https://www.tink.se) hackathon.
