package core

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
)

// Point is a place's row (D21, the places table): where it is, the radius a photo's position must fall in, and
// whether a photo there links its day.
type Point struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	RadiusM  int     `json:"radius_m"`
	LinkDays bool    `json:"link_days"`
}

// DefaultRadius is the radius a place named for a photo takes when none is given: a spot, not a city.
const DefaultRadius = 250

func (p Point) check() error {
	switch {
	case math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180:
		return Invalid("a point is a latitude -90..90 and a longitude -180..180")
	case p.Lat == 0 && p.Lon == 0:
		return Invalid("0°, 0° is how photo metadata says \"no location\": it is never a place's point")
	case p.RadiusM < 10 || p.RadiusM > 100000:
		return Invalid("a radius is 10 to 100000 metres")
	}
	return nil
}

func linkDays(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Locate gives a place its point or moves it: the owner's act (cookbook/place-of-a-photo.md says a photo never moves
// a point).
func (t *Tx) Locate(id int64, p Point) error {
	if err := p.check(); err != nil {
		return err
	}
	var typ string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT entity_type FROM entities WHERE id = ?`, id).Scan(&typ); errors.Is(err, sql.ErrNoRows) {
		return NotFound("no page %d", id)
	} else if err != nil {
		return err
	}
	if typ != "place" {
		return Invalid("only a place has a point, not a %s", typ)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO places(id, lat, lon, radius_m, link_days) VALUES (?, ?, ?, ?, ?)
	                     ON CONFLICT(id) DO UPDATE SET lat = excluded.lat, lon = excluded.lon, radius_m = excluded.radius_m,
	                                                   link_days = excluded.link_days`, id, p.Lat, p.Lon, p.RadiusM, linkDays(p.LinkDays))
	return err
}

// pointIfNone gives a place a point only if it has none (cookbook/place-of-a-photo.md); set says it did.
func (t *Tx) pointIfNone(id int64, p Point) (set bool, err error) {
	if err := p.check(); err != nil {
		return false, err
	}
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO places(id, lat, lon, radius_m, link_days) VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`,
		id, p.Lat, p.Lon, p.RadiusM, linkDays(p.LinkDays))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// Match is the place a position is in.
type Match struct {
	ID       int64
	Title    string
	LinkDays bool
}

// matchSQL implements cookbook/place-of-a-photo.md's deterministic position match.
const matchSQL = `
SELECT id, title, link_days, d2
  FROM (SELECT pl.id, pg_name.title, pl.link_days, pl.radius_m,
               ((pl.lat - :lat) * 111320.0) * ((pl.lat - :lat) * 111320.0)
             + ((pl.lon - :lon) * :m_per_deg_lon) * ((pl.lon - :lon) * :m_per_deg_lon) AS d2
          FROM places pl
          JOIN entities pg ON pg.id = pl.id JOIN entity_names pg_name ON pg_name.entity_id = pg.id AND pg_name.name_key = pg.preferred_name_key AND pg.deleted_at IS NULL)
 WHERE d2 <= radius_m * radius_m
 ORDER BY radius_m, d2, id
 LIMIT 1`

// MatchPlace is the place a position is in, nil when none holds it.
func (t *Tx) MatchPlace(lat, lon float64) (*Match, error) {
	m := &Match{}
	var d2 float64
	err := t.tx.QueryRowContext(t.ctx, matchSQL, sql.Named("lat", lat), sql.Named("lon", lon), sql.Named("m_per_deg_lon", metresPerDegreeLon(lat))).
		Scan(&m.ID, &m.Title, &m.LinkDays, &d2)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// metresPerDegreeLon is what the match binds: the metres a degree of longitude spans at a latitude (D21).
func metresPerDegreeLon(lat float64) float64 { return 111320 * math.Cos(lat*math.Pi/180) }

// Near reports whether two positions are within metres of each other, by the flat approximation the place match
// uses (matchSQL): what the match would say of a place at the first position with that radius.
func Near(lat1, lon1, lat2, lon2, metres float64) bool {
	dy := (lat1 - lat2) * 111320
	dx := (lon1 - lon2) * metresPerDegreeLon(lat1)
	return dx*dx+dy*dy <= metres*metres
}

// placeFor is the place a photo's owner names (cookbook/place-of-a-photo.md): an existing place, a plain page
// promoted to one, or a new place.
func (t *Tx) placeFor(title string) (int64, error) {
	if err := checkTitle("title", title); err != nil {
		return 0, err
	}
	p, err := t.lookup(title)
	switch {
	case err != nil:
		return 0, err
	case p == nil:
		id, _, err := t.CreatePlace(title, "")
		return id, err
	case p.Type == "place":
		if p.Deleted {
			return 0, Conflict("the place %s is deleted: revive it first", p.Title)
		}
		return p.ID, nil
	case p.Type == "page" && !p.DayPage:
		return p.ID, t.Promote(p.ID, "place", "")
	}
	return 0, &ExistsError{p.ID, p.Title}
}

// placeOf is the point of a place page, nil when it has none.
func (s *Store) placeOf(ctx context.Context, id int64) (*Point, error) {
	p := &Point{}
	err := s.DB.R.QueryRowContext(ctx, `SELECT lat, lon, radius_m, link_days FROM places WHERE id = ?`, id).Scan(&p.Lat, &p.Lon, &p.RadiusM, &p.LinkDays)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) Locate(ctx context.Context, source string, id int64, p Point) error {
	return s.Do(ctx, source, func(t *Tx) error { return t.Locate(id, p) })
}

// MapLink is a position on OpenStreetMap, for the owner to open: the writer itself never requests it.
func MapLink(lat, lon float64) string {
	return "https://www.openstreetmap.org/?mlat=" + strconv.FormatFloat(lat, 'f', 5, 64) + "&mlon=" + strconv.FormatFloat(lon, 'f', 5, 64) + "#map=16/" +
		strconv.FormatFloat(lat, 'f', 5, 64) + "/" + strconv.FormatFloat(lon, 'f', 5, 64)
}

// round6 is a coordinate as reported: six decimals, about 10 cm.
func round6(x float64) float64 { return math.Round(x*1e6) / 1e6 }
