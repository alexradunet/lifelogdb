package tests

import (
	"context"
	"math"
	"math/rand"
	"regexp"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// places: where a place is (schema.sql, D21) — an optional row of a place's page with its point, its radius and
// whether its days are linked; cookbook/place-of-a-photo run literally: a point given once, a position matched (the
// smallest circle that holds it, then the nearest) against a haversine oracle, the antimeridian limit, the day linked
// and the photo shown in it once.
func places(s *S) {
	err := func(r string) bool { return strings.HasPrefix(r, "ERR") }
	point := func(c *C, id any, lat, lon, radius, link any) string {
		return c.tryx("INSERT INTO places(id,lat,lon,radius_m,link_days) VALUES (?,?,?,?,?)", id, lat, lon, radius, link)
	}

	// ---- the keys and CHECKs
	c := s.fresh()
	pl := c.named("place", "Lakeside")
	s.K("a place's point is a row of its own, keyed by its page", point(c, pl, 46.1, 7.2, 300, 1) == "OK" &&
		c.tab("select e.entity_type, pl.lat, pl.lon, pl.radius_m, pl.link_days from places pl join entities e using(id)") == "place|46.1|7.2|300|1")
	s.K("a place needs no row: a place without a point is whole", c.n("select count(*) from places where id=?", c.named("place", "Nowhere")) == 0 && c.integrityOK())
	s.K("a places row needs a page of type place", err(point(c, c.page("Plain"), 1, 1, 100, 1)) && err(point(c, c.named("person", "Ann"), 1, 1, 100, 1)))
	s.K("...and cannot claim another type", err(c.tryx("INSERT INTO places(id,entity_type,lat,lon,radius_m) VALUES (?,'person',1,1,100)", c.named("person", "Bo"))))
	for _, v := range [][2]any{{91.0, 0.5}, {-90.5, 0.5}, {10.0, 180.5}, {10.0, -181.0}, {math.Inf(1), 0.5}, {10.0, math.Inf(-1)}, {math.NaN(), 0.5}, {"x", 0.5}, {0.0, 0.0}} {
		s.K("a point at ("+val(v[0])+", "+val(v[1])+") is refused", err(point(c, c.named("place", ""), v[0], v[1], 100, 1)))
	}
	for _, v := range [][2]any{{90.0, 180.0}, {-90.0, -180.0}, {0.0, 10.0}, {10.0, 0.0}} {
		s.K("a point at ("+val(v[0])+", "+val(v[1])+") is accepted (only 0°, 0° together is \"no location\")", point(c, c.named("place", ""), v[0], v[1], 100, 1) == "OK")
	}
	for _, r := range []any{9, 100001, nil, 1.5} {
		s.K("a radius of "+val(r)+" m is refused", err(point(c, c.named("place", ""), 10, 10, r, 1)))
	}
	s.K("a radius of 10 m and of 100 km are accepted", point(c, c.named("place", ""), 10, 10, 10, 1) == "OK" && point(c, c.named("place", ""), 10, 10, 100000, 0) == "OK")
	s.K("link_days is 0 or 1", err(point(c, c.named("place", ""), 10, 10, 100, 2)) && err(point(c, c.named("place", ""), 10, 10, 100, nil)))
	s.K("link_days defaults to 1", c.tryx("INSERT INTO places(id,lat,lon,radius_m) VALUES (?,1,1,100)", c.named("place", "Default")) == "OK" &&
		c.n("select pl.link_days from places pl join entity_names n on n.entity_id=pl.id where n.name_key='default'") == 1)
	c.must("UPDATE entities SET updated_at='2000-01-01T00:00:00.000Z' WHERE id=?", pl)
	s.K("a wrong point is fixed by UPDATE", c.tryx("UPDATE places SET lat=46.11, radius_m=400 WHERE id=?", pl) == "OK")
	s.K("...which bumps entities.updated_at (places_touch)", c.n("select updated_at > '2000-01-01T00:00:00.000Z' from entities where id=?", pl) == 1)
	s.K("a point is never deleted", err(c.tryx("DELETE FROM places WHERE id=?", pl)))
	s.K("a place with a point cannot become a person (the places row's FK)", err(c.tryx("UPDATE entities SET entity_type='person' WHERE id=?", pl)))
	s.K("integrity and foreign keys clean", c.integrityOK())

	// ---- cookbook/place-of-a-photo
	blocks := regexp.MustCompile("(?s)```sql\n(.*?)```").FindAllStringSubmatch(s.d.Page("cookbook/place-of-a-photo.md"), -1)
	if len(blocks) != 3 {
		stop("cookbook/place-of-a-photo has %d SQL blocks, not 3: give a point, match, link the day", len(blocks))
	}
	give, match, day := blocks[0][1], blocks[1][1], blocks[2][1]
	mpd := func(lat float64) float64 { return 111320 * math.Cos(lat*math.Pi/180) }
	at := func(c *C, lat, lon float64) [][]any {
		r, e := c.query(match, P{"lat": lat, "lon": lon, "m_per_deg_lon": mpd(lat)})
		if e != nil {
			stop("place-of-a-photo match: %v", e)
		}
		return r
	}
	c = s.fresh()
	lisbon, cafe, home := c.named("place", "Lisbon"), c.named("place", "Café Lume"), c.named("place", "Home")
	for _, x := range []struct {
		id                int64
		lat, lon          float64
		radius, link_days int
	}{{lisbon, 38.7223, -9.1393, 10000, 1}, {cafe, 38.7110, -9.1420, 100, 1}, {home, 38.7300, -9.1500, 150, 0}} {
		if _, e := c.runBlock(give, P{"place_id": x.id, "lat": x.lat, "lon": x.lon, "radius_m": x.radius, "link_days": x.link_days}, nil); e != nil {
			stop("place-of-a-photo give: %v", e)
		}
	}
	_, e := c.runBlock(give, P{"place_id": cafe, "lat": 40.0, "lon": -8.0, "radius_m": 500, "link_days": 0}, nil)
	s.K("cookbook/place-of-a-photo: a place's point is given once; a second answer never moves it",
		e == nil && c.tab("select lat, lon, radius_m, link_days from places where id=?", cafe) == "38.711|-9.142|100|1", e)
	title := func(r [][]any) string {
		if len(r) == 0 {
			return "none"
		}
		return val(r[0][1])
	}
	s.K("a position in the café, inside Lisbon too: the smallest circle wins", title(at(c, 38.7111, -9.1421)) == "Café Lume")
	s.K("a position in Lisbon, outside the café: Lisbon", title(at(c, 38.7200, -9.1300)) == "Lisbon")
	s.K("a position near no place: no row", title(at(c, 41.15, -8.61)) == "none")
	h := at(c, 38.7301, -9.1501)
	s.K("a position at home: matched, with link_days 0 (recognised, not linked)", title(h) == "Home" && val(h[0][2]) == "0", tab(h))
	c.must("UPDATE entities SET deleted_at = "+NOW+" WHERE id=?", cafe)
	s.K("a tombstoned place is never the answer", title(at(c, 38.7111, -9.1421)) == "Lisbon")

	tied := s.fresh()
	first, second := tied.named("place", "Zulu"), tied.named("place", "Alpha")
	tied.must("INSERT INTO places(id,lat,lon,radius_m,link_days) VALUES (?,46,7,100,0),(?,46,7,100,1)", first, second)
	// A replacement writer may add indexes. Stable attribution must survive a different visit order.
	tied.must("CREATE INDEX place_match_fixture_order ON places(radius_m,id DESC)")
	tied.must("ANALYZE")
	tied.must("PRAGMA reverse_unordered_selects=ON")
	winner := at(tied, 46, 7)
	s.K("equal place circles resolve by stable id, including link_days", len(winner) == 1 && val(winner[0][0]) == val(first) && val(winner[0][2]) == "0", tab(winner))
	var writerMatch *core.Match
	matchErr := tied.store().Do(context.Background(), "ui", func(t *core.Tx) error {
		var err error
		writerMatch, err = t.MatchPlace(46, 7)
		return err
	})
	s.K("the writer resolves equal place circles by stable id", matchErr == nil && writerMatch != nil && writerMatch.ID == first && !writerMatch.LinkDays, matchErr, writerMatch)

	// the match against a haversine oracle: the smallest circle that holds the position, then the nearest
	hav := func(a, b, c2, d float64) float64 {
		rad := math.Pi / 180
		p1, p2, dl := a*rad, c2*rad, (d-b)*rad
		x := math.Pow(math.Sin((p2-p1)/2), 2) + math.Cos(p1)*math.Cos(p2)*math.Pow(math.Sin(dl/2), 2)
		return 2 * 6371008.8 * math.Asin(math.Sqrt(x))
	}
	c = s.fresh()
	rnd := rand.New(rand.NewSource(2026))
	type spot struct {
		lat, lon float64
		r        int
		title    string
	}
	var live []spot
	for i := range 70 {
		sp := spot{44.38 + rnd.Float64()*0.1, 26.03 + rnd.Float64()*0.14, 50 + rnd.Intn(1500), "Spot " + val(int64(i))}
		id := c.named("place", sp.title)
		point(c, id, sp.lat, sp.lon, sp.r, 1)
		if i < 60 {
			live = append(live, sp)
		} else {
			c.must("UPDATE entities SET deleted_at = "+NOW+" WHERE id=?", id)
		}
	}
	agree, found, same, worst := 0, 0, 0, 0.0
	var wrong []string
	for range 400 {
		la, lo := 44.38+rnd.Float64()*0.1, 26.03+rnd.Float64()*0.14
		got := at(c, la, lo)
		writer := "none"
		c.store().Do(context.Background(), "ui", func(t *core.Tx) error {
			if m, _ := t.MatchPlace(la, lo); m != nil {
				writer = m.Title
			}
			return nil
		})
		if writer == title(got) {
			same++
		}
		want, edge := "none", false
		best := spot{r: math.MaxInt}
		bestD := 0.0
		for _, sp := range live {
			d := hav(la, lo, sp.lat, sp.lon)
			if math.Abs(d-float64(sp.r)) < 0.01*float64(sp.r) {
				edge = true // within 1 % of a radius: either side is right
			}
			if d <= float64(sp.r) && (sp.r < best.r || sp.r == best.r && d < bestD) {
				best, bestD, want = sp, d, sp.title
			}
		}
		if want != "none" {
			found++
		}
		if title(got) == want || edge {
			agree++
		} else {
			wrong = append(wrong, title(got)+" ≠ "+want)
		}
		if len(got) > 0 {
			for _, sp := range live {
				if sp.title == title(got) {
					worst = math.Max(worst, math.Abs(math.Sqrt(got[0][3].(float64))/hav(la, lo, sp.lat, sp.lon)-1))
				}
			}
		}
	}
	s.K("the match is the haversine answer for all 400 positions among 60 live places of random radii (within 1 % of a radius, either answer)",
		agree == 400 && found > 100, found, wrong)
	s.K("and its distance is within 0.5 % of the great-circle distance", worst > 0 && worst < 0.005, worst)
	s.K("the writer's own match is the recipe's for all 400 positions", same == 400, same)
	c = s.fresh()
	fj := c.named("place", "Taveuni")
	point(c, fj, -16.8, 179.999, 1000, 1)
	d := hav(-16.8, 179.999, -16.8, -179.999)
	s.K("as D21 states: a place 213 m away across ±180° is not matched", d < 1000 && title(at(c, -16.8, -179.999)) == "none", d)

	// the day linked and the photo shown, once
	c = s.fresh()
	lake := c.named("place", "Lakeside")
	p := P{"taken_day": "2019-06-03", "place_id": lake, "file_title": "2019-06-03 IMG_1.jpg", "source": "ui", "append_embed": 1}
	if _, e := c.runBlock(day, p, nil); e != nil {
		stop("place-of-a-photo day: %v", e)
	}
	dp := p["photo_day_id"]
	s.K("cookbook/place-of-a-photo: a day with no page gets one, its at link and the photo's embed",
		c.tab("select n.title, e.day, e.body from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key where e.id=?", dp) == "2019-06-03|2019-06-03|![[2019-06-03 IMG_1.jpg]]" &&
			c.n("select count(*) from links where from_id=? and to_id=? and kind='at'", dp, lake) == 1, c.tab("select n.title, e.day, e.body from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key where e.id=?", dp))
	sts := statements(day)
	again := []string{sts[0], sts[4], sts[5], sts[6]} // the day page found: the two INSERTs skipped, as a writer does
	p["append_embed"] = 0
	if _, e := c.runBlock(strings.Join(again, "\n"), p, nil); e != nil {
		stop("place-of-a-photo day again: %v", e)
	}
	s.K("...and a second photo of that day at that place, or the same photo again, adds no link and no second embed",
		c.n("select count(*) from links where from_id=? and kind='at'", dp) == 1 && c.str("select body from entities where id=?", dp) == "![[2019-06-03 IMG_1.jpg]]")
	c.must("UPDATE entities SET body = 'Swam at dawn.' WHERE id=?", dp)
	p["file_title"] = "2019-06-03 IMG_2.jpg"
	p["append_embed"] = 1
	c.runBlock(strings.Join(again, "\n"), p, nil)
	s.K("another photo is appended after a blank line", c.str("select body from entities where id=?", dp) == "Swam at dawn.\n\n![[2019-06-03 IMG_2.jpg]]")

	for _, body := range []string{"![[2019-06-03 IMG_2.jpg|lake]]", "`![[2019-06-03 IMG_2.jpg]]`", "[[2019-06-03 IMG_2.jpg]]"} {
		c.must("UPDATE entities SET body = ? WHERE id=?", body, dp)
		p["append_embed"] = 1
		want := body + "\n\n![[2019-06-03 IMG_2.jpg]]"
		if text.HasEmbed(body, p["file_title"].(string)) {
			p["append_embed"] = 0
			want = body
		}
		_, e := c.runBlock(strings.Join(again, "\n"), p, nil)
		s.K("cookbook parsed embed binding preserves aliases but appends after literal code/plain links", e == nil && c.str("select body from entities where id=?", dp) == want, e)
	}
	s.K("integrity and foreign keys clean", c.integrityOK())

	// the writer's own keep of a photo writes what the recipe writes
	cw := s.fresh()
	lk := cw.named("place", "Lakeside")
	if _, e := cw.runBlock(give, P{"place_id": lk, "lat": 46.1, "lon": 7.2, "radius_m": 300, "link_days": 1}, nil); e != nil {
		stop("photo writer point: %v", e)
	}
	k, ew := cw.store().AddFile(context.Background(), "ui", core.FileIn{Title: "2019-06-03 IMG_1.jpg", SHA256: strings.Repeat("ab", 32), MIME: "image/jpeg",
		Taken: "2019-06-03", Lat: 46.1001, Lon: 7.2001, HasGPS: true, Preview: jpegBytes})
	s.K("the writer's own keep of a photo links its day, shows it there once and puts its page on that day, as the recipe does",
		ew == nil && k.Linked && cw.tab("select n.title,e.day,e.body from entities e join entity_names n on n.entity_id=e.id and n.name_key=e.preferred_name_key where n.name_key='2019-06-03'") == "2019-06-03|2019-06-03|![[2019-06-03 IMG_1.jpg]]" &&
			cw.n("select count(*) from links l join entities d on d.id=l.from_id where d.preferred_name_key='2019-06-03' and l.kind = 'at' and l.to_id = ?", lk) == 1 &&
			cw.str("select e.day from entities e join entity_names n on n.entity_id=e.id where n.name_key='2019-06-03 img_1.jpg'") == "2019-06-03", ew, k)
	s.K("...and stores no position of the photo: the only coordinates are the place's", cw.n("select count(*) from places") == 1 &&
		cw.n("select count(*) from entities where body like '%46.1001%'") == 0)
}
