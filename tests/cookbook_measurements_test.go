package tests

func cookbookMeasurementReads(s *S) {
	c := s.fresh()
	weight := c.metric("weight", "kg")
	for _, metric := range []int64{1, weight} {
		c.must("INSERT INTO measurements(metric_id,day,value,source,created_at) VALUES (?,'2026-10-01',1,'ui','2026-10-01T12:00:00.000Z')", metric)
	}
	series, mood, day := s.d.Block("metric-series"), s.d.Block("mood-over-time"), s.d.Block("day-view")
	params := P{"day": "2026-10-01", "metric": "weight", "include_deleted": 0}
	labeled := sqlBlocks(s.d.Page("cookbook/recorded-sessions.md"))[1]
	s.K("active measurement recipes expose live metrics", len(c.rows(series, params)) == 1 && len(c.rows(mood)) == 1 && len(c.rows(day, params)) == 2 && len(c.rows(labeled, params)) == 1)
	c.must("UPDATE entities SET deleted_at='2026-10-02T00:00:00.000Z' WHERE id IN (1,?)", weight)
	s.K("active measurement recipes hide tombstoned metrics", len(c.rows(series, params)) == 0 && len(c.rows(mood)) == 0 && len(c.rows(day, params)) == 0 && len(c.rows(labeled, params)) == 0)
	params["include_deleted"] = 1
	s.K("historical labeled readings retain tombstoned metric facts", len(c.rows(labeled, params)) == 1 && c.n("SELECT count(*) FROM measurement_values") == 2)
	c.must("UPDATE entities SET deleted_at=NULL WHERE id IN (1,?)", weight)
	s.K("active measurement recipes restore readings on metric revival", len(c.rows(series, params)) == 1 && len(c.rows(mood)) == 1 && len(c.rows(day, params)) == 2)

	blocks := sqlBlocks(s.d.Page("cookbook/metric-series.md"))
	if !s.K("metric series includes a recorded-time cutoff recipe", len(blocks) == 2) {
		return
	}
	asOf := blocks[1]
	c = s.fresh()
	weight = c.metric("weight", "kg")
	put := func(value any, at string, prior any) int64 {
		return c.n("INSERT INTO measurements(metric_id,day,value,source,created_at,supersedes_id) VALUES (?,'2026-10-01',?,'ui',?,?) RETURNING id", weight, value, at, prior)
	}
	read := func(at string) string { return c.tab(asOf, P{"metric": "weight", "as_of": at}) }
	a := put(71.25, "2026-10-01T10:00:00.000Z", nil)
	b := put(72.25, "2026-10-01T12:00:00.000Z", a)
	put(73.25, "2026-10-01T11:00:00.000Z", b)   // the clock regressed across a correction
	put(80.25, "2026-10-01T10:00:00.000Z", nil) // an independent same-day reading
	s.K("recorded-time cutoff excludes later recorded facts", read("2026-10-01T09:59:59.999Z") == "")
	s.K("recorded-time cutoff retains original before a later correction", read("2026-10-01T10:30:00.000Z") == "1|2026-10-01|71.25|None|None|None; 4|2026-10-01|80.25|None|None|None", read("2026-10-01T10:30:00.000Z"))
	s.K("recorded-time cutoff follows eligible descendants across a clock regression", read("2026-10-01T11:30:00.000Z") == "3|2026-10-01|73.25|None|None|None; 4|2026-10-01|80.25|None|None|None", read("2026-10-01T11:30:00.000Z"))
	r := put(nil, "2026-10-01T13:00:00.000Z", int64(3))
	s.K("recorded-time cutoff applies retractions at the cutoff", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|None|None|None", read("2026-10-01T13:00:00.000Z"))
	put(74.25, "2026-10-01T13:00:00.000Z", r)
	s.K("recorded-time cutoff resolves equal timestamps by correction ancestry", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|None|None|None; 6|2026-10-01|74.25|None|None|None", read("2026-10-01T13:00:00.000Z"))
	c.must("UPDATE entities SET deleted_at='2026-10-02T00:00:00.000Z' WHERE id=?", weight)
	s.K("recorded-time history labels present metric lifecycle rather than hiding facts", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|None|2026-10-02T00:00:00.000Z|None; 6|2026-10-01|74.25|None|2026-10-02T00:00:00.000Z|None", read("2026-10-01T13:00:00.000Z"))
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", weight)
	session := sessionFixture(c, c.page("Workout"), M{})
	c.must("INSERT INTO measurements(metric_id,day,value,source,session_id,created_at) VALUES (?,'2026-10-01',99.25,'ui',?,'2026-10-01T13:00:00.000Z')", weight, session)
	c.must("UPDATE sessions SET deleted_at='2026-10-02T00:00:00.000Z' WHERE id=?", session)
	s.K("recorded-time history retains explicit scope and present session lifecycle", read("2026-10-01T13:00:00.000Z") == "4|2026-10-01|80.25|None|None|None; 6|2026-10-01|74.25|None|None|None; 7|2026-10-01|99.25|1|None|2026-10-02T00:00:00.000Z", read("2026-10-01T13:00:00.000Z"))
}
