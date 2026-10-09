# Los — Design Spec

Date: 2026-10-09
Status: Approved (design), pending spec review

## Purpose

Los is a small boat-watching website. Someone standing at a fixed spot on the
coast (set via `LOS_LAT`/`LOS_LON`) sees a boat, opens
Los on their phone, and gets a list of the boats currently in view: photo, name,
and where the boat is heading.

> **Update:** the observer location was first a hardcoded default. Before
> publishing the repo it became the required `LOS_LAT`/`LOS_LON` env vars, so
> the spot stays out of source control.

## Requirements

What the user asked for:

- Lists boats visible from one fixed location.
- Uses open AIS data.
- Per boat: image, name, travel route.
- Runs as a Docker container hosting a website.
- No login for visitors.
- Minimal UI.

Decisions made during brainstorming:

- **Main use:** a phone, opened on demand when a boat is spotted. Mobile-first.
- **Visibility:** an arc (circular sector) around the observer. "Visible" means
  inside the arc. No line-of-sight calculation.
- **Debug route:** `/debug` shows a map with the arc overlay and the boats, used
  to tune the arc.
- **Route:** destination only (`→ Rotterdam, NL · ETA …`). An inferred origin
  port is out of scope for now (see Future work).
- **Images:** Wikimedia Commons lookup, falling back to a ship-type icon. Each
  boat also gets a "More info ↗" link to its VesselFinder page. No scraping or
  hotlinking of third-party photo sites.
- **Data source:** the BarentsWatch Live AIS API (free registration, client
  credentials). The backend may use keys; visitors never log in.
- **Stack:** Go, one binary, one container.
- **Scale:** single instance, low traffic.
- **Coverage limit (accepted):** the open BarentsWatch data excludes leisure and
  sailing vessels under 45 m and fishing vessels under 15 m.

## Architecture

```
phone ──HTTP──> Los (Go binary in container)
                 ├─ GET /         list of boats in the arc (server-rendered HTML)
                 ├─ GET /debug    Leaflet map: arc overlay + boat markers
                 ├─ GET /static/* embedded CSS and icons
                 └─ GET /healthz  liveness
                     │
                     ├─ ais client ───> BarentsWatch Live AIS
                     │                  (token cached until expiry,
                     │                   results cached 30 s)
                     └─ image client ─> Wikimedia Commons API
                                        (lookups cached in SQLite on a volume)
```

### Packages

Go module `los`, layout:

```
cmd/los/main.go          wiring, startup, graceful shutdown
internal/config          env var parsing and validation
internal/geo             arc polygon, point-in-arc, distance, bearing
internal/ais             BarentsWatch token + latest/combined client
internal/vessel          Vessel model, ship-type labels, destination parsing
internal/images          Wikimedia lookup + SQLite cache
internal/web             HTTP handlers, templates, embedded static files
```

Each package has one job and can be tested on its own.

#### `config`

Reads environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `BW_CLIENT_ID` | required | BarentsWatch client id |
| `BW_CLIENT_SECRET` | required | BarentsWatch client secret |
| `LOS_LAT` | required | observer latitude |
| `LOS_LON` | required | observer longitude |
| `LOS_ARC_FROM` | `120` | arc start bearing, degrees true |
| `LOS_ARC_TO` | `270` | arc end bearing, degrees true, swept clockwise from `LOS_ARC_FROM` |
| `LOS_RADIUS_NM` | `12` | arc radius, nautical miles |
| `LOS_MAX_AGE` | `15m` | ignore boats whose last position is older than this |
| `LOS_DB_PATH` | `/data/los.db` | SQLite file |
| `LOS_ADDR` | `:8080` | listen address |

If a required value is missing or a value fails to parse, the app exits at
startup with a message naming the variable.

The arc defaults are a first guess. Tune them with `/debug`.

#### `geo`

- `Arc{Center, FromDeg, ToDeg, RadiusNM}`.
- `Arc.Polygon(stepDeg)` returns a closed GeoJSON ring: center, points along
  the circular edge from `FromDeg` clockwise to `ToDeg`, back to center. It
  handles arcs that cross north (for example from 300 to 60).
- `Arc.Contains(p)` is true when distance ≤ radius and the bearing lies within
  the clockwise sweep.
- `Distance(a, b)` in nautical miles (haversine) and `Bearing(a, b)` in degrees.

#### `ais`

- Gets a token with a POST to `https://id.barentswatch.no/connect/token`
  (`grant_type=client_credentials`, `scope=ais`). Caches it until 60 s before
  expiry.
- `LatestInArea(ctx, polygon, since) ([]vessel.Vessel, error)` calls
  `POST https://live.ais.barentswatch.no/v1/latest/combined` with
  `{"geometry": {"type": "Polygon", "coordinates": [<ring>]}, "since": <RFC3339>, "modelType": "Full", "modelFormat": "Json"}`.
  Coordinates are in GeoJSON `[lon, lat]` order.
- Maps `AisComboFull` fields: `mmsi`, `imoNumber`, `name`, `callSign`,
  `shipType`, `destination`, `eta`, `latitude`, `longitude`, `speedOverGround`,
  `courseOverGround`, `shipLength`, `msgtime`.
- Requests time out after 5 s.
- A 30 s in-memory cache, keyed by request, sits in front of the client. It
  also keeps the last good result for the stale fallback.

#### `vessel`

- `Vessel` struct: the AIS fields above, plus derived `DistanceNM`, `TypeLabel`,
  `TypeIcon`, `Destination` (parsed).
- Ship-type mapping from AIS type codes to a label and icon: 30 Fishing,
  31–32 Towing, 36 Sailing, 37 Pleasure, 40–49 High-speed, 50 Pilot,
  51 SAR, 52 Tug, 60–69 Passenger, 70–79 Cargo, 80–89 Tanker, everything else
  Other.
- Destination parsing: trim and uppercase. If the text looks like a UN/LOCODE
  (`NO SVG`, `NOSVG`, `NO-SVG`), resolve it through an embedded table of common
  North Sea and Baltic ports to `Stavanger, NO`. Otherwise show the raw text in
  title case. If the field is empty, show "Destination unknown".
- ETA: show only if it is in the future and within 60 days. AIS ETAs are often
  stale or set to placeholder values.

#### `images`

- `Lookup(ctx, v) (Image, bool)` returns a thumbnail URL (960 px wide, shown full-width at the top of each card) and
  a link to the Commons file page.
- Strategy: search Commons via the MediaWiki API, first by `IMO <number>`, then
  by the exact ship name in quotes, limited to the File namespace and bitmap
  images. Take the first result.
- Cache table:

  ```sql
  CREATE TABLE image_cache (
    key        TEXT PRIMARY KEY,   -- "imo:9123456" or "mmsi:257000000"
    thumb_url  TEXT,               -- NULL means "no image found"
    page_url   TEXT,
    fetched_at INTEGER NOT NULL,   -- unix seconds
    used_at    INTEGER NOT NULL    -- unix seconds
  );
  ```

- **Size bounds.** The cache stores URLs only, never image bytes. The phone
  loads thumbnails straight from `upload.wikimedia.org`. Hits expire after 30
  days and misses after 7 days. A hard cap of 5,000 rows evicts the least
  recently used. Pruning runs at startup and every 24 h, followed by
  `VACUUM`. Worst case is about 2 MB.
- A Wikimedia network error is not cached as a miss. Lookup is retried on the
  next request.
- The `/` handler runs lookups concurrently, at most 4 at a time, with a 3 s
  budget overall. Boats with no result by then get the icon this time.
- Requests send a descriptive `User-Agent`, as Wikimedia's API policy requires.

#### `web`

- Uses `html/template` with templates and static files embedded via `embed`.
- `GET /`: fetches the boats in the arc (filtered again with `Arc.Contains`,
  because the API polygon is approximate), drops anything older than
  `LOS_MAX_AGE`, sorts by distance ascending, resolves images, and renders.
- `GET /debug`: queries a wider bounding box (the arc's bounding box plus 50%),
  marks each boat inside or outside the arc, and renders a Leaflet map. It shows
  the observer, the arc polygon, and boat markers coloured by in/out with popups
  of raw AIS fields, plus the current config values. Leaflet is loaded from
  cdnjs at a pinned version, with OpenStreetMap tiles.
- `GET /healthz`: returns `200 ok`.

## UI

Mobile-first, a single column, a system font stack, light and dark modes via
`prefers-color-scheme`. No client-side JavaScript on `/`.

```
Los                         updated 14:32 ↻
────────────────────────────────────────
[photo]  KRONPRINS HAAKON        2.1 nm
         Passenger · 9.8 kn
         → Bergen, NO · ETA 10 Oct 06:00
         More info ↗
────────────────────────────────────────
[icon]   NORDIC STAR             5.7 nm
         Cargo · 12.1 kn
         → Rotterdam, NL
         More info ↗
```

- "↻" is a plain link to `/`. Pulling to refresh in the phone browser does the
  same.
- "More info ↗" opens `https://www.vesselfinder.com/vessels/details/<IMO>`, or
  `<MMSI>` when there is no IMO, in a new tab.
- Empty state: "No boats in view right now."
- Stale state: a "Data from 14:20 (stale)" note under the header.
- Times are shown in `Europe/Oslo`.
- Language: English and Norwegian Bokmål (`nb`), chosen from the browser's
  `Accept-Language` (nb, no and nn map to nb; anything else gets English).
  Norwegian uses decimal commas and `10. okt 08:00` dates. `/debug` stays
  English.

## Error handling

| Situation | Behaviour |
|---|---|
| Missing or invalid config | Exit at startup with a clear message |
| BarentsWatch timeout, 5xx, or token failure | Log it. Serve the last good result marked stale. With no previous result, show "Couldn't reach AIS data, try again shortly." and return status 503 |
| BarentsWatch 401 | Drop the cached token and retry once |
| Wikimedia error | Use the icon fallback and don't cache the miss |
| SQLite error | Log it and continue without the cache (lookups still work) |

## Testing

- **Unit tests:** `geo` (polygon shape, wrap-around arcs, contains, distance
  against known values), `vessel` (type mapping, LOCODE and free-text
  destinations, ETA sanity), `config` (defaults, required vars, parse errors).
- **`images`:** cache TTL, LRU cap eviction, and miss handling against a temp
  SQLite file. Lookup against an `httptest` fake MediaWiki API.
- **`ais`:** token caching, the 401 retry, request body shape, and response
  mapping against an `httptest` fake with recorded JSON fixtures.
- **`web`:** handler tests with fake AIS and image dependencies, behind
  interfaces. They check the rendered list, the empty state, the stale state,
  and the 503.
- **Manual:** run `docker compose up`, open `/` on a phone, and use `/debug` to
  tune the arc.

## Deployment

- A multi-stage `Dockerfile` builds with `golang:<pinned>` and runs on
  `gcr.io/distroless/static`, as a non-root user.
- SQLite uses pure-Go `modernc.org/sqlite`, so there is no CGO.
- `compose.yaml` holds one service, `env_file: .env`, a named volume mounted at
  `/data`, port `8080`, and `restart: unless-stopped`.
- `.env.example` documents the variables. `.env` is in `.gitignore`.
- `README.md` covers BarentsWatch client registration, setup, and tuning the arc.

## Out of scope

- Inferring the origin port (planned, see Future work).
- Line-of-sight or horizon calculations.
- User accounts, auth, and rate limiting.
- Historical tracks and notifications.
- Scraping or hotlinking third-party ship photos.

## Future work

- **Origin port:** track each ship's last stop (speed about 0 near a known port
  polygon) to show `Bergen, NO → Rotterdam, NL`.
