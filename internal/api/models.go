package api

type ForecastResponse struct {
	Lat   float64                `json:"lat"`
	Lon   float64                `json:"lon"`
	Data  map[string]any         `json:"data"`
	Cached bool                  `json:"cached"`
}

type ConditionsResponse struct {
	Zip     string      `json:"zip"`
	Partial bool        `json:"partial"`
	Data    map[string]any `json:"data"`
}

// TakehomeWeatherResponse is the National Weather Service–backed summary required by the takehome prompt.
type TakehomeWeatherResponse struct {
	ShortForecast   string  `json:"short_forecast"`
	TemperatureFeel string  `json:"temperature_feel"` // "hot", "cold", or "moderate"
	Lat             float64 `json:"lat"`
	Lon             float64 `json:"lon"`
}