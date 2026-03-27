package mongo

import (
	"context"
	"time"
)

// ForecastDoc design (sample):
// {
//   _id: "loc:{lat}:{lon}",   // or geo hash
//   loc: { lat: ..., lon: ... },
//   updated_at: ISODate(...),
//   valid_until: ISODate(...),  // for TTL-like patterns (or store in separate cache collection)
//   payload: { ...forecast json... }
// }
//
// Indexes you should describe:
// 1) {_id: 1} (default)
// 2) { "loc.lat": 1, "loc.lon": 1, "updated_at": -1 } if not using _id keying
// 3) TTL index if you store expirable cache docs in a separate collection:
//    { "valid_until": 1 } with expireAfterSeconds: 0

type Repo interface {
	GetLatestForecast(ctx context.Context, lat, lon float64) (map[string]any, error)
	PutForecast(ctx context.Context, lat, lon float64, payload map[string]any, validFor time.Duration) error
}