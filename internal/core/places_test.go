package core

import (
	"strings"
	"testing"
)

// photo is a file to keep as a photo taken on day at a position, with a small picture.
func photo(t *testing.T, c byte, title, day string, lat, lon float64) FileIn {
	return FileIn{Title: title, SHA256: sha(c), MIME: "image/jpeg", Taken: day, Lat: lat, Lon: lon, HasGPS: true, Preview: smallJPEG(t)}
}

func placeID(t *testing.T, s *Store, title string, p Point) int64 {
	t.Helper()
	id, err := s.CreatePlace(ctx, "cli", title)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Locate(ctx, "cli", id, p); err != nil {
		t.Fatal(err)
	}
	return id
}

func dayOf(t *testing.T, s *Store, day string) *Page {
	t.Helper()
	id, _ := s.PageID(ctx, day)
	if id == 0 {
		return nil
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAPhotoLinksItsDayToItsPlace(t *testing.T) {
	s := fresh(t)
	placeID(t, s, "Lisbon", Point{38.7223, -9.1393, 10000, true})
	placeID(t, s, "Café Lume", Point{38.7110, -9.1420, 100, true})
	k, err := s.AddFile(ctx, "cli", photo(t, 'a', "2019-06-03 IMG_1.jpg", "2019-06-03", 38.7111, -9.1421))
	if err != nil {
		t.Fatal(err)
	}
	if k.Place != "Café Lume" || !k.Linked || !k.Embedded || k.Day != "2019-06-03" || k.Unmatched != nil {
		t.Errorf("kept: %+v", k)
	}
	d := dayOf(t, s, "2019-06-03")
	if d == nil || titles(d.Out, "at") != "Café Lume" || d.Body != "![[2019-06-03 IMG_1.jpg]]" || titles(d.Out, "wikilink") != "2019-06-03 IMG_1.jpg" {
		t.Fatalf("the day: %+v", d)
	}
	f, _ := s.PageByID(ctx, k.ID)
	if f.Day != "2019-06-03" {
		t.Errorf("the photo's page is on %s, not the day it was taken", f.Day)
	}
	// a second photo that day in Lisbon, and the first one again
	if _, err := s.AddFile(ctx, "cli", photo(t, 'b', "2019-06-03 IMG_2.jpg", "2019-06-03", 38.7200, -9.1300)); err != nil {
		t.Fatal(err)
	}
	again, err := s.AddFile(ctx, "cli", photo(t, 'a', "whatever.jpg", "2019-06-03", 38.7111, -9.1421))
	if err != nil || !again.Existing || again.Embedded {
		t.Errorf("the same photo again: %+v %v", again, err)
	}
	d = dayOf(t, s, "2019-06-03")
	if titles(d.Out, "at") != "Café Lume,Lisbon" || d.Body != "![[2019-06-03 IMG_1.jpg]]\n\n![[2019-06-03 IMG_2.jpg]]" {
		t.Errorf("the day after two photos and a re-send: at %s, body %q", titles(d.Out, "at"), d.Body)
	}
}

func TestAPhotoNearNoPlaceIsAskedAbout(t *testing.T) {
	s := fresh(t)
	home := placeID(t, s, "Home", Point{38.73, -9.15, 150, false})
	k, err := s.AddFile(ctx, "cli", photo(t, 'a', "IMG_1.jpg", "2019-06-03", 41.1496, -8.6110))
	if err != nil {
		t.Fatal(err)
	}
	if k.Unmatched == nil || k.Linked || k.Place != "" || !k.Embedded || !strings.Contains(k.Unmatched.Map, "mlat=41.14960") {
		t.Errorf("near no place: %+v", k)
	}
	if d := dayOf(t, s, "2019-06-03"); titles(d.Out, "at") != "" {
		t.Errorf("a day linked to nowhere: %v", d.Out)
	}
	// the owner names it: the place takes the position as its point, and the day is linked
	k, err = s.AddFile(ctx, "cli", FileIn{SHA256: sha('a'), MIME: "image/jpeg", Taken: "2019-06-03", Lat: 41.1496, Lon: -8.6110, HasGPS: true, At: "Porto", Radius: 5000})
	if err != nil || !k.Existing || !k.PointSet || !k.Linked || k.Place != "Porto" {
		t.Fatalf("named: %+v %v", k, err)
	}
	porto, _ := s.PageID(ctx, "Porto")
	if pt, _ := s.PlaceOf(ctx, porto); pt == nil || pt.RadiusM != 5000 || !pt.LinkDays {
		t.Errorf("Porto's point: %+v", pt)
	}
	// later photos there match by themselves
	if k, _ := s.AddFile(ctx, "cli", photo(t, 'b', "IMG_2.jpg", "2019-06-04", 41.1400, -8.6200)); k.Place != "Porto" || !k.Linked {
		t.Errorf("a later photo in Porto: %+v", k)
	}
	// home is recognised, never linked
	k, _ = s.AddFile(ctx, "cli", photo(t, 'c', "IMG_3.jpg", "2019-06-05", 38.7301, -9.1501))
	if k.Place != "Home" || k.Linked || k.Unmatched != nil {
		t.Errorf("at home: %+v", k)
	}
	if d := dayOf(t, s, "2019-06-05"); titles(d.Out, "at") != "" || d.Body != "![[IMG_3.jpg]]" {
		t.Errorf("a day at home: %v %q", d.Out, d.Body)
	}
	// a photo never moves a point
	if _, err := s.AddFile(ctx, "cli", FileIn{SHA256: sha('c'), MIME: "image/jpeg", Taken: "2019-06-05", Lat: 38.8, Lon: -9.2, HasGPS: true, At: "Home"}); err != nil {
		t.Fatal(err)
	}
	if pt, _ := s.PlaceOf(ctx, home); pt.Lat != 38.73 || pt.RadiusM != 150 {
		t.Errorf("a photo moved home: %+v", pt)
	}
}

func TestAPhotoWithNoDayLinksNoDay(t *testing.T) {
	s := fresh(t)
	placeID(t, s, "Lakeside", Point{46.1, 7.2, 300, true})
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "scan.jpg", SHA256: sha('a'), MIME: "image/jpeg", Lat: 46.1, Lon: 7.2, HasGPS: true, Preview: smallJPEG(t)})
	if err != nil || k.Place != "Lakeside" || k.Linked || k.Embedded || k.Day != "" {
		t.Errorf("no day of its own: %+v %v", k, err)
	}
	if d := dayOf(t, s, Today()); d != nil {
		t.Errorf("today's page was made: %+v", d)
	}
}

func TestPlaceRefusals(t *testing.T) {
	s := fresh(t)
	if _, err := s.CreatePerson(ctx, "cli", "Bob Sample", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "a.jpg", SHA256: sha('a'), MIME: "image/jpeg", Taken: "2019-06-03", At: "Bob Sample"}); err == nil {
		t.Error("a person named as a photo's place")
	}
	id, _ := s.CreatePlace(ctx, "cli", "Somewhere")
	for name, p := range map[string]Point{"0°, 0°": {0, 0, 100, true}, "a latitude of 91": {91, 0, 100, true}, "a radius of 5 m": {1, 1, 5, true}} {
		if err := s.Locate(ctx, "cli", id, p); status(err) != 422 {
			t.Errorf("%s: %v", name, err)
		}
	}
	page, _, _ := s.CreatePage(ctx, "cli", "Plain", "")
	if err := s.Locate(ctx, "cli", page, Point{1, 1, 100, true}); status(err) != 422 {
		t.Errorf("a plain page located: %v", err)
	}
	if _, err := s.AddFile(ctx, "cli", FileIn{Title: "b.jpg", SHA256: sha('b'), MIME: "image/jpeg", Lat: 0, Lon: 0, HasGPS: true}); status(err) != 422 {
		t.Errorf("a position at 0°, 0°: %v", err)
	}
	// the owner moves a point
	if err := s.Locate(ctx, "cli", id, Point{10, 10, 200, false}); err != nil {
		t.Fatal(err)
	}
	if err := s.Locate(ctx, "cli", id, Point{11, 11, 300, true}); err != nil {
		t.Fatal(err)
	}
	if pt, _ := s.PlaceOf(ctx, id); pt.Lat != 11 || pt.RadiusM != 300 || !pt.LinkDays {
		t.Errorf("moved: %+v", pt)
	}
	if p, _ := s.PageByID(ctx, id); p.Point == nil || p.Point.Lat != 11 {
		t.Errorf("a place page without its point: %+v", p)
	}
	if r, err := s.Integrity(ctx); err != nil || !r.OK {
		t.Errorf("integrity: %+v %v", r, err)
	}
}

func TestAPhotoNearNoPlaceStillHasItsDay(t *testing.T) {
	s := fresh(t)
	k, err := s.AddFile(ctx, "cli", FileIn{Title: "IMG_1.HEIC", SHA256: sha('a'), MIME: "image/heic", Taken: "2019-06-05", Lat: 41.15, Lon: -8.61, HasGPS: true})
	if err != nil || k.Day != "2019-06-05" || k.Unmatched == nil || k.Linked || k.Embedded {
		t.Errorf("a HEIC near no place: %+v %v", k, err)
	}
	if f, _ := s.PageByID(ctx, k.ID); f.Day != "2019-06-05" {
		t.Errorf("its page is on %s", f.Day)
	}
	if d := dayOf(t, s, "2019-06-05"); d != nil {
		t.Errorf("a day page made for nothing: %+v", d)
	}
}
