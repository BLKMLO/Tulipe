package translate

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNoLimiterWaitsForNothing(t *testing.T) {
	// Zero is the older behaviour and the default: send as fast as the service
	// answers. A nil limiter has to be usable without the caller checking.
	var l *limiter
	if newLimiter(0) != nil || newLimiter(-5) != nil {
		t.Fatal("a limit of zero or less produced a limiter")
	}
	start := time.Now()
	if err := l.wait(context.Background(), nil); err != nil {
		t.Fatalf("wait on a nil limiter: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("a nil limiter waited")
	}
}

func TestSlotsAreSpacedEvenly(t *testing.T) {
	// The clock is passed in, so the spacing is checked exactly rather than by
	// sleeping through it and hoping the machine was not busy.
	l := newLimiter(60) // one per second
	if l.interval != time.Second {
		t.Fatalf("interval = %v, want a second", l.interval)
	}
	now := time.Now()
	for i, want := range []time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second} {
		if got := l.reserve(now); got != want {
			t.Errorf("request %d waits %v, want %v", i+1, got, want)
		}
	}
}

func TestAnIdleRunDoesNotBankSlots(t *testing.T) {
	// A book whose chapters take a minute each must not be allowed to fire
	// sixty requests at once afterwards: the quota is per minute, not a purse
	// that fills while nothing happens.
	l := newLimiter(60)
	now := time.Now()
	l.reserve(now)

	later := now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if got := l.reserve(later); got != time.Duration(i)*time.Second {
			t.Errorf("after an idle hour, request %d waits %v", i+1, got)
		}
	}
}

func TestConcurrentCallersNeverShareASlot(t *testing.T) {
	// Nothing in Tulipe is concurrent yet. The limiter is written for the day
	// it is, and this is the property that would otherwise be discovered the
	// hard way: two goroutines must not be handed the same moment.
	l := newLimiter(600)
	now := time.Now()

	const callers = 50
	var wg sync.WaitGroup
	seen := make([]time.Duration, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seen[i] = l.reserve(now)
		}(i)
	}
	wg.Wait()

	taken := map[time.Duration]bool{}
	for _, d := range seen {
		if taken[d] {
			t.Fatalf("two callers were given the same slot at %v", d)
		}
		taken[d] = true
	}
	if len(taken) != callers {
		t.Errorf("%d distinct slots for %d callers", len(taken), callers)
	}
}

func TestWaitingStopsWhenTheRunIsCancelled(t *testing.T) {
	l := newLimiter(1) // one a minute: the second request would wait a long time
	ctx, cancel := context.WithCancel(context.Background())
	if err := l.wait(ctx, nil); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- l.wait(ctx, nil) }()
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled wait came back as a success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the run did not stop the wait")
	}
}

func TestALongWaitSaysSoAndAShortOneKeepsQuiet(t *testing.T) {
	// A screen that sits still for half a minute without a word looks broken;
	// one that flickers a message every 200ms is noise.
	l := newLimiter(1)
	var told []time.Duration
	notify := func(d time.Duration) { told = append(told, d) }

	if err := l.wait(context.Background(), notify); err != nil {
		t.Fatal(err)
	}
	if len(told) != 0 {
		t.Errorf("the first request, which waits for nothing, announced %v", told)
	}

	// The second would wait a minute. Reserve it by hand rather than sit
	// through it.
	if d := l.reserve(time.Now()); d < noticeableWait {
		t.Fatalf("the second slot is %v away, too soon for this test", d)
	}

	quick := newLimiter(6000) // ten a second: below the threshold
	told = nil
	for i := 0; i < 3; i++ {
		if err := quick.wait(context.Background(), notify); err != nil {
			t.Fatal(err)
		}
	}
	if len(told) != 0 {
		t.Errorf("a wait of a few milliseconds was announced: %v", told)
	}
}

func TestTheLimiterIsSharedByTheWholeBook(t *testing.T) {
	// A quota belongs to the service, not to the chapter. Defaults is applied
	// once by Book and again by Document, and the second must find the first
	// one's limiter rather than start a fresh allowance per chapter.
	book := Options{RequestsPerMinute: 30}.Defaults()
	if book.limiter == nil {
		t.Fatal("no limiter although a limit was asked for")
	}
	if doc := book.Defaults(); doc.limiter != book.limiter {
		t.Error("the second Defaults replaced the limiter; every chapter would get its own quota")
	}
	if none := (Options{}).Defaults(); none.limiter != nil {
		t.Error("a run with no limit was given a limiter")
	}
}

func TestTheSecondAttemptSpendsFromTheSameQuota(t *testing.T) {
	// salvageOptions resets the failure counter on purpose. Resetting the
	// limiter too would let the salvage pass ignore the quota.
	base := Options{RequestsPerMinute: 30}.Defaults()
	sub := salvageOptions(base)
	if sub.limiter != base.limiter {
		t.Error("the second attempt got a limiter of its own")
	}
	if sub.failures == base.failures {
		t.Error("the second attempt kept the first pass's failure count")
	}
}
