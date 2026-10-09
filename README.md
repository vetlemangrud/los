# Los
Dette prosjektet er sykt vibecoded, men vi hadde lyst på en enkel oversikt over båtene vi kan se fra hytta

----

A tiny website that lists the boats you can see from one spot on the
Norwegian coast. Open it on your phone when a boat passes.

Data: [BarentsWatch Live AIS](https://developer.barentswatch.no/docs/AIS/live-ais-api)
(open data from the Norwegian Coastal Administration). Images: Wikimedia Commons.

> The open AIS dataset excludes leisure and sailing vessels under 45 m and
> fishing vessels under 15 m, so small pleasure boats won't appear.

## Setup

1. Register at [barentswatch.no](https://www.barentswatch.no/), open *Min side*,
   and create an API client with access to AIS.
2. `cp .env.example .env` and fill in `BW_CLIENT_ID`, `BW_CLIENT_SECRET`, and
   your observation spot as `LOS_LAT` / `LOS_LON` (decimal degrees).
3. `docker compose up -d --build`
4. Open `http://<host>:8080/` on your phone.

## Tuning the visibility arc

Open `http://<host>:8080/debug`. The blue sector is what counts as "visible".
Green markers are inside it, red are outside. Adjust `LOS_ARC_FROM`,
`LOS_ARC_TO` (degrees true, swept clockwise) and `LOS_RADIUS_NM` in `.env`,
then `docker compose up -d`.

## Development

No local Go needed: `./gow test ./...` runs the Go toolchain in Docker.
