# The world outline

`world-50m-v5.1.2.svg` is the land and the country borders that the places map draws under its points
(`placemap.go`). It is made from [Natural Earth](https://www.naturalearthdata.com/), which is in the public domain,
at the 1:50m scale. `GET /assets/world-50m-v5.1.2.svg` serves it with a cache of one year, so **a changed file gets
a new name**, and `placemap.go` names it.

A unit of the file is a hundredth of a degree: x is the longitude and y the latitude negated, so north is up. The
paths have no colour, so the page gives them the colours of its theme.

The source files were fetched on 2026-10-15 from tag `v5.1.2` of `github.com/nvkelso/natural-earth-vector`, and the
file was made with:

```
go run ./tools/worldmap ne_50m_land.geojson ne_50m_admin_0_boundary_lines_land.geojson internal/api/html/world-50m-v5.1.2.svg
```

| file | source | sha256 |
|---|---|---|
| `ne_50m_land.geojson` | `raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_50m_land.geojson` | `e874b27a51d146452be360cafb3cc50c86001074a67d534113e6534682f9826b` |
| `ne_50m_admin_0_boundary_lines_land.geojson` | `raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_50m_admin_0_boundary_lines_land.geojson` | `2faac4f6b34386f3d21b6e018cf151f241f00e5c936d44dd17d7d9bfb147fa48` |
| `world-50m-v5.1.2.svg` | made by `tools/worldmap` | `1302e06d9cc5e7c0914c7d69f618384026601e49b7ed91225905e64ffbfcd726` |
