// SPDX-License-Identifier: BSD-3-Clause

// Package budget splits one deadline between the phases of a piece of work,
// and keeps an honest record of how each phase spent its share.
//
// A request that must answer in three minutes usually grows a timeout per
// step: two minutes for this call, ninety seconds for that scrape. Each number
// is defensible on its own, nothing adds them up, and the only real bound on
// the whole is whatever the client gives up at. This package replaces that
// with one deadline and a slice for every phase derived from what is left when
// the phase starts:
//
//	slice(phase) = min( cap(phase), remaining − margin − Σ floor(later stages) )
//
// A floor is time an earlier stage may not take: give the phase that writes the
// answer a floor, and no amount of slowness earlier on can leave it with
// nothing. Phases that share a stage run concurrently, so only strictly later
// stages hold time back. A phase whose slice is below the plan's MinSlice is
// not started at all and is recorded as skipped, rather than started into a
// guaranteed overrun.
//
//	b, err := budget.New(plan, nil)
//	ctx, cancel, ok := b.Begin(parent, "search")
//	if ok {
//		err := search(ctx)
//		cancel()
//		b.End(ctx, "search", err)
//	}
//	...
//	for _, r := range b.Finalize() {
//		// r did not finish: say so in the answer
//	}
//
// Every phase ends as done, failed, overrun or skipped. Finalize freezes that
// record when the answer is assembled: a phase still running then is overrun,
// and nothing that happens afterwards changes the record. The answer can then
// say which parts it is missing instead of silently leaving them out.
//
// # What it does not do
//
// It does not schedule. It never runs, orders, retries or parallelises your
// phases; it answers "how long may this phase take if it starts now?" and
// records what happened. Stage numbers only tell it which floors are still
// ahead.
//
// It does not stop work. A slice is a context deadline, and cancellation in Go
// is cooperative: a phase that ignores its context keeps running and keeps
// whatever it holds. Finalize reports it as overrun, which is true, but the
// goroutine is yours to stop.
//
// Its caps are a plan, not a measurement. Nothing here knows whether a phase
// can finish in its slice; measure that on the hardware that will run it.
//
// The ledger reads time through the clock you give New, so the arithmetic can
// be tested with a fake clock. Context deadlines are set from durations and
// expire on the real clock.
package budget
