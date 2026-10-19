package tests

import "time"

// streaks: cookbook/streaks run literally on hand-checked days. The answer is the run of day pages with words that
// ends today, or yesterday when today has no words yet; an earlier run, a gap, an empty page, a tombstone and a day
// after today do not count; and the look back stops at 3660 days.
func streaks(s *S) {
	Q := s.d.Block("streaks")
	const today = "2026-10-19"
	// back is the day n days before today.
	back := func(n int) string {
		t, _ := time.Parse(time.DateOnly, today)
		return t.AddDate(0, 0, -n).Format(time.DateOnly)
	}
	// run is the days from first through last, counted back from today: run(0, 2) is today and the two days before.
	run := func(c *C, first, last int) {
		for n := first; n <= last; n++ {
			c.dayPage(back(n), "Synthetic words.")
		}
	}
	answer := func(c *C) string { return c.tab(Q, P{"today": today}) }

	c := s.fresh()
	run(c, 0, 2)  // today and the two days before it: a run of 3
	run(c, 4, 15) // after a gap of three days, a run of 12
	s.K("a run of 3 ending today, a gap, and a run of 12 before it: the streak is 3, and today is written", answer(c) == "3|1", answer(c))

	c = s.fresh()
	run(c, 1, 2)
	run(c, 4, 15)
	s.K("nothing written today, a run of 2 ending yesterday: the streak is 2, and today is not written", answer(c) == "2|0", answer(c))

	c = s.fresh()
	run(c, 4, 15)
	s.K("neither today nor yesterday is written: the streak is 0, though an earlier run of 12 exists", answer(c) == "0|0", answer(c))

	c = s.fresh()
	run(c, 0, 11)
	s.K("a run of 12 ending today is 12", answer(c) == "12|1", answer(c))

	c = s.fresh()
	run(c, 1, 12)
	s.K("a run of 12 ending yesterday is 12", answer(c) == "12|0", answer(c))

	c = s.fresh()
	c.dayPage(today, "Synthetic words.")
	c.dayPage(back(-1), "Synthetic words.")
	c.dayPage(back(-2), "Synthetic words.")
	s.K("a day after today does not count", answer(c) == "1|1", answer(c))

	c = s.fresh()
	c.dayPage(today, "")
	run(c, 1, 2)
	s.K("a day page with an empty body is not written: the streak ends yesterday", answer(c) == "2|0", answer(c))

	c = s.fresh()
	run(c, 0, 5)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE is_journal AND day=?", back(2))
	s.K("a tombstoned day page breaks the run: today, yesterday and the day before count 2", answer(c) == "2|1", answer(c))

	c = s.fresh()
	run(c, 0, 3660) // 3661 days in a row: the look back stops at 3660 days
	s.K("a run longer than the look back reads as 3660", answer(c) == "3660|1", answer(c))
}
