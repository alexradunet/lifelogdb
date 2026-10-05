package core

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"

	"lifelog/internal/text"
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
		return invalid("a point is a latitude -90..90 and a longitude -180..180")
	case p.Lat == 0 && p.Lon == 0:
		return invalid("0°, 0° is how photo metadata says \"no location\": it is never a place's point")
	case p.RadiusM < 10 || p.RadiusM > 100000:
		return invalid("a radius is 10 to 100000 metres")
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
	if err := t.tx.QueryRow(`SELECT entity_type FROM pages WHERE id = ?`, id).Scan(&typ); errors.Is(err, sql.ErrNoRows) {
		return notFound("no page %d", id)
	} else if err != nil {
		return err
	}
	if typ != "place" {
		return invalid("only a place has a point, not a %s", typ)
	}
	_, err := t.tx.Exec(`INSERT INTO places(id, lat, lon, radius_m, link_days) VALUES (?, ?, ?, ?, ?)
	                     ON CONFLICT(id) DO UPDATE SET lat = excluded.lat, lon = excluded.lon, radius_m = excluded.radius_m,
	                                                   link_days = excluded.link_days`, id, p.Lat, p.Lon, p.RadiusM, linkDays(p.LinkDays))
	return err
}

// pointIfNone gives a place a point only if it has none (cookbook/place-of-a-photo.md); set says it did.
func (t *Tx) pointIfNone(id int64, p Point) (set bool, err error) {
	if err := p.check(); err != nil {
		return false, err
	}
	res, err := t.tx.Exec(`INSERT INTO places(id, lat, lon, radius_m, link_days) VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`,
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

// matchSQL is cookbook/place-of-a-photo.md's match: the smallest circle that holds the position, then the nearest.
const matchSQL = `
SELECT id, title, link_days, d2
  FROM (SELECT pl.id, pg.title, pl.link_days, pl.radius_m,
               ((pl.lat - :lat) * 111320.0) * ((pl.lat - :lat) * 111320.0)
             + ((pl.lon - :lon) * :m_per_deg_lon) * ((pl.lon - :lon) * :m_per_deg_lon) AS d2
          FROM places pl
          JOIN pages pg   ON pg.id = pl.id
          JOIN entities e ON e.id = pl.id AND e.deleted_at IS NULL)
 WHERE d2 <= radius_m * radius_m
 ORDER BY radius_m, d2
 LIMIT 1`

// MatchPlace is the place a position is in, nil when none holds it.
func (t *Tx) MatchPlace(lat, lon float64) (*Match, error) {
	m := &Match{}
	var d2 float64
	err := t.tx.QueryRow(matchSQL, sql.Named("lat", lat), sql.Named("lon", lon), sql.Named("m_per_deg_lon", MetresPerDegreeLon(lat))).
		Scan(&m.ID, &m.Title, &m.LinkDays, &d2)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// MetresPerDegreeLon is what the match binds: the metres a degree of longitude spans at a latitude (D21).
func MetresPerDegreeLon(lat float64) float64 { return 111320 * math.Cos(lat*math.Pi/180) }

// placeFor is the place a photo's owner names (cookbook/place-of-a-photo.md): an existing place, a plain page
// promoted to one, or a new place.
func (t *Tx) placeFor(title string) (int64, error) {
	if !text.ValidTitle(title) {
		return 0, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", title)
	}
	p, err := t.Lookup(title)
	switch {
	case err != nil:
		return 0, err
	case p == nil:
		id, _, err := t.CreatePlace(title, "")
		return id, err
	case p.Type == "place":
		if p.Deleted {
			return 0, conflict("the place %s is deleted: revive it first", p.Title)
		}
		return p.ID, nil
	case p.Type == "page" && !p.DayPage && !p.Stub:
		return p.ID, t.Promote(p.ID, "place", "")
	}
	return 0, &ExistsError{p.ID, p.Title}
}

// embed shows a file in a day's page once: `![[title]]` appended after a blank line unless the page shows it
// already, then the page's links synced (cookbook/place-of-a-photo.md).
func (t *Tx) embed(dayID int64, title string) (bool, *Sync, error) {
	var body string
	if err := t.tx.QueryRow(`SELECT body FROM pages WHERE id = ?`, dayID).Scan(&body); err != nil {
		return false, nil, err
	}
	if text.HasEmbed(body, title) {
		return false, nil, nil
	}
	body += func() string {
		if body == "" {
			return ""
		}
		return "\n\n"
	}() + "![[" + title + "]]"
	keys, _, _ := text.Targets(body, "")
	named := false
	for _, key := range keys {
		if key == text.TitleKey(title) {
			named = true
		}
	}
	if !text.HasEmbed(body, title) || !named {
		return false, nil, invalid("automatic photo embed cannot render and link here; edit the day's Markdown before keeping the photo")
	}
	_, err := t.tx.Exec(`UPDATE pages SET body = ? WHERE id = ?`, body, dayID)
	if err != nil {
		return false, nil, err
	}

	sync, err := t.syncWikilinks(dayID, body)
	return true, &sync, err
}

// PlaceOf is the point of a place page, nil when it has none.
func (s *Store) PlaceOf(ctx context.Context, id int64) (*Point, error) {
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

func automaticEmbedTitle(title string) error {
	mark := "![[" + title + "]]"
	keys, _, _ := text.Targets(mark, "")
	if !text.HasEmbed(mark, title) || len(keys) != 1 || keys[0] != text.TitleKey(title) {
		return invalid("cannot automatically embed %q: give a new file an explicit representable title; an existing file's handle is not renamed", title)
	}
	return nil
}

func (t *Tx) preflightEmbed(day, title string) error {
	var body string
	err := t.tx.QueryRow(`SELECT body FROM pages WHERE title_key = ?`, day).Scan(&body)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && text.HasEmbed(body, title) {
		return nil
	}
	return automaticEmbedTitle(title)
}
