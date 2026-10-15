// SPDX-License-Identifier: BSD-3-Clause

package throttle_test

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/org-runink/runi/throttle"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// A watchdog that sees a service fail every 2 s. Each restart must pass two
// caps: the service may restart at most once every 10 s, and the watchdog
// as a whole at most 3 times a minute, on the minute.
func Example() {
	each, err := throttle.NewCooldown[string](10*time.Second, 1000)
	if err != nil {
		panic(err)
	}
	all, err := throttle.NewQuota(3, time.Minute, start)
	if err != nil {
		panic(err)
	}
	held := 0
	for s := 0; s < 80; s += 2 {
		at := start.Add(time.Duration(s) * time.Second)
		// Ask the per-minute quota first without spending it, so a refusal
		// there does not use up the service's cooldown.
		if all.Remaining(at) == 0 {
			held++
			continue
		}
		if each.Allow("db", at) && all.Allow(at) {
			fmt.Printf("t=%2ds restart db\n", s)
		}
	}
	fmt.Println("refused by cooldown:", each.Denied(), "held by quota:", held)
	// Output:
	// t= 0s restart db
	// t=10s restart db
	// t=20s restart db
	// t=60s restart db
	// t=70s restart db
	// refused by cooldown: 16 held by quota: 19
}

// One action every 2 s on average, up to 3 at once. Five requests at once
// get three; the next token is due 2 s later.
func ExampleBucket() {
	b, err := throttle.NewBucket(0.5, 3)
	if err != nil {
		panic(err)
	}
	for i := range 5 {
		fmt.Printf("request %d: %v\n", i, b.Allow(start))
	}
	fmt.Println("next token at", b.Next(start).Sub(start))
	fmt.Println("tokens after 5 s:", b.Tokens(start.Add(5*time.Second)))
	// Output:
	// request 0: true
	// request 1: true
	// request 2: true
	// request 3: false
	// request 4: false
	// next token at 2s
	// tokens after 5 s: 2
}

// At most 2 scale-ups per hour, on the hour.
func ExampleQuota() {
	b, err := throttle.NewQuota(2, time.Hour, start)
	if err != nil {
		panic(err)
	}
	for _, m := range []int{5, 20, 40, 59, 61} {
		at := start.Add(time.Duration(m) * time.Minute)
		ok := b.Allow(at)
		fmt.Printf("%02d:%02d %-5v remaining %d until %s\n", m/60, m%60, ok, b.Remaining(at), b.WindowEnd(at).Format("15:04"))
	}
	// Output:
	// 00:05 true  remaining 1 until 01:00
	// 00:20 true  remaining 0 until 01:00
	// 00:40 false remaining 0 until 01:00
	// 00:59 false remaining 0 until 01:00
	// 01:01 true  remaining 1 until 02:00
}

// Each node may be drained at most once every 5 minutes.
func ExampleCooldown() {
	cd, err := throttle.NewCooldown[string](5*time.Minute, 0)
	if err != nil {
		panic(err)
	}
	at := func(m int) time.Time { return start.Add(time.Duration(m) * time.Minute) }
	fmt.Println(cd.Allow("node-a", at(0)), cd.Allow("node-b", at(1)), cd.Allow("node-a", at(3)))
	fmt.Println("node-a next at", cd.Next("node-a", at(3)).Sub(start))
	fmt.Println(cd.Allow("node-a", at(5)), "tracked:", cd.Len())
	fmt.Println("forgotten at 10m:", cd.Sweep(at(10)), "tracked:", cd.Len())
	// Output:
	// true true false
	// node-a next at 5m0s
	// true tracked: 2
	// forgotten at 10m: 2 tracked: 0
}

// Delays between retries, from a seeded source so they are the same on every
// run. Full jitter's ceiling doubles from 100 ms up to the 2 s cap; each
// delay is anywhere below it.
func ExampleBackoff() {
	b, err := throttle.NewBackoff(throttle.FullJitter, 100*time.Millisecond, 2*time.Second, rand.NewPCG(1, 2))
	if err != nil {
		panic(err)
	}
	for range 6 {
		ceil := b.Ceiling()
		fmt.Printf("attempt %d: wait %v (at most %v)\n", b.Attempt(), b.Next().Round(time.Millisecond), ceil)
	}
	// Output:
	// attempt 0: wait 77ms (at most 100ms)
	// attempt 1: wait 123ms (at most 200ms)
	// attempt 2: wait 314ms (at most 400ms)
	// attempt 3: wait 637ms (at most 800ms)
	// attempt 4: wait 375ms (at most 1.6s)
	// attempt 5: wait 82ms (at most 2s)
}

// Decorrelated jitter: each delay between base and three times the last,
// capped.
func ExampleBackoff_decorrelated() {
	b, err := throttle.NewBackoff(throttle.DecorrelatedJitter, 100*time.Millisecond, 2*time.Second, rand.NewPCG(1, 2))
	if err != nil {
		panic(err)
	}
	for range 6 {
		fmt.Println(b.Next().Round(time.Millisecond))
	}
	// Output:
	// 254ms
	// 508ms
	// 1.217s
	// 2s
	// 1.482s
	// 279ms
}

// A breaker that opens after 3 failures in a row, waits 30 s, then lets one
// probe through.
func ExampleBreaker() {
	b, err := throttle.NewBreaker(3, 30*time.Second, 1)
	if err != nil {
		panic(err)
	}
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	for s := range 3 {
		if b.Allow(at(s)) {
			b.Record(false, at(s))
		}
	}
	fmt.Println(b.State(at(3)), "until", b.OpenUntil().Sub(start), "allow:", b.Allow(at(10)))
	fmt.Println(b.State(at(32)), "probe:", b.Allow(at(32)), "second:", b.Allow(at(32)))
	b.Record(true, at(33))
	fmt.Println(b.State(at(33)), "after", b.Transitions(), "transitions")
	// Output:
	// open until 32s allow: false
	// half-open probe: true second: false
	// closed after 3 transitions
}
