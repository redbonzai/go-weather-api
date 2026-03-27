package weather

import (
	"context"
	"time"
)

// Base URL for The Weather Company API developer package.
// All endpoints share this root; the api key is passed as a query parameter.
const DefaultBaseURL = "https://api.weather.com"

// Units controls the unit system returned by the API.
// "e" = Imperial (°F, mph, in)  "m" = Metric (°C, km/h, mm)
// "h" = UK hybrid               "s" = SI
type Units string

const (
	UnitsImperial Units = "e"
	UnitsMetric   Units = "m"
	UnitsHybrid   Units = "h"
	UnitsSI       Units = "s"
)

// Config holds all settings needed to construct a weather service client.
type Config struct {
	// APIKey is the developer key issued by weather.com.
	APIKey string

	// BaseURL defaults to DefaultBaseURL; override in tests or staging.
	BaseURL string

	// Units is the unit system for numeric fields. Defaults to UnitsMetric.
	Units Units

	// Language is the BCP-47 language tag for translated narrative fields
	// (e.g. "en-US", "de-DE"). Defaults to "en-US".
	Language string

	// HTTPTimeout caps each individual HTTP round-trip.
	// The caller is still expected to pass a context with its own deadline.
	HTTPTimeout time.Duration
}

func (cfg *Config) defaults() {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Units == "" {
		cfg.Units = UnitsMetric
	}
	if cfg.Language == "" {
		cfg.Language = "en-US"
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 2 * time.Second
	}
}

// Service is the read interface for all Weather Company Developer Package endpoints.
// Handlers depend on this interface, not on the concrete *client, so the
// implementation can be swapped or mocked in tests.
type Service interface {
	// CurrentConditions returns real-time observations for the given coordinates.
	// Sourced from the Currents On Demand (CoD) system at 4 km global resolution.
	CurrentConditions(ctx context.Context, lat, lon float64) (*CurrentConditionsResponse, error)

	// HourlyForecast returns up to 24 hours of hour-by-hour forecast data.
	HourlyForecast(ctx context.Context, lat, lon float64) (*HourlyForecastResponse, error)

	// DailyForecast returns a 7-day forecast with day, night, and 24-hour summary segments.
	DailyForecast(ctx context.Context, lat, lon float64) (*DailyForecastResponse, error)

	// AlertHeadlines returns active watches, warnings, and advisories for the
	// given coordinates from NWS, Environment Canada, MeteoAlarm, and other
	// authoritative government sources.
	AlertHeadlines(ctx context.Context, lat, lon float64) (*AlertHeadlinesResponse, error)

	// AlertDetails returns the full text and metadata for a single alert
	// identified by the detailKey returned inside an AlertHeadline.
	AlertDetails(ctx context.Context, detailKey string) (*AlertDetailsResponse, error)
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// CurrentConditionsResponse mirrors the top-level shape returned by the
// /v1/geocode/{lat}/{lon}/observations.json endpoint.
type CurrentConditionsResponse struct {
	Metadata   ResponseMetadata       `json:"metadata"`
	Observation map[string]any        `json:"observation"`
}

// HourlyForecastResponse mirrors the top-level shape returned by the
// /v1/geocode/{lat}/{lon}/forecast/hourly/24hour.json endpoint.
type HourlyForecastResponse struct {
	Metadata        ResponseMetadata `json:"metadata"`
	HourlyForecasts []HourlyPeriod  `json:"forecasts"`
}

// HourlyPeriod is one hour of the hourly forecast.
type HourlyPeriod struct {
	FcstValid       int64   `json:"fcst_valid"`
	FcstValidLocal  string  `json:"fcst_valid_local"`
	Num             int     `json:"num"`
	DayInd          string  `json:"day_ind"`
	Temp            int     `json:"temp"`
	Dewpt           int     `json:"dewpt"`
	Hi              int     `json:"hi"`
	Wc              int     `json:"wc"`
	FeelsLike       int     `json:"feels_like"`
	IconExtd        int     `json:"icon_extd"`
	Wxman           string  `json:"wxman"`
	IconCode        int     `json:"icon_code"`
	Dow             string  `json:"dow"`
	Phrase12Char    string  `json:"phrase_12char"`
	Phrase22Char    string  `json:"phrase_22char"`
	Phrase32Char    string  `json:"phrase_32char"`
	SubphrasePt1    string  `json:"subphrase_pt1"`
	SubphrasePt2    string  `json:"subphrase_pt2"`
	SubphrasePt3    string  `json:"subphrase_pt3"`
	Pop             int     `json:"pop"`
	PrecipType      string  `json:"precip_type"`
	Qpf             float64 `json:"qpf"`
	SnowQpf         float64 `json:"snow_qpf"`
	Rh              int     `json:"rh"`
	Wspd            int     `json:"wspd"`
	Wdir            int     `json:"wdir"`
	WdirCardinal    string  `json:"wdir_cardinal"`
	Gust            *int    `json:"gust"`
	Clds            int     `json:"clds"`
	Vis             float64 `json:"vis"`
	Mslp            float64 `json:"mslp"`
	UvIndexRaw      float64 `json:"uv_index_raw"`
	UvIndex         int     `json:"uv_index"`
	UvWarning       int     `json:"uv_warning"`
	UvDesc          string  `json:"uv_desc"`
	GolfIndex       *int    `json:"golf_index"`
	GolfCategory    string  `json:"golf_category"`
	Severity        int     `json:"severity"`
}

// DailyForecastResponse mirrors the top-level shape returned by the
// /v1/geocode/{lat}/{lon}/forecast/daily/7day.json endpoint.
type DailyForecastResponse struct {
	Metadata  ResponseMetadata `json:"metadata"`
	Forecasts []DailyPeriod   `json:"forecasts"`
}

// DailyPeriod is one day's forecast, containing day, night, and 24-hour segments.
type DailyPeriod struct {
	FcstValid      int64          `json:"fcst_valid"`
	FcstValidLocal string         `json:"fcst_valid_local"`
	Num            int            `json:"num"`
	MaxTemp        *int           `json:"max_temp"`
	MinTemp        *int           `json:"min_temp"`
	TorconIndex    *int           `json:"torcon_index"`
	StormconIndex  *int           `json:"stormcon_index"`
	Blurb          *string        `json:"blurb"`
	BlurbAuthor    *string        `json:"blurb_author"`
	LunarPhaseDay  int            `json:"lunar_phase_day"`
	Sunrise        string         `json:"sunrise"`
	Sunset         string         `json:"sunset"`
	Moonrise       string         `json:"moonrise"`
	Moonset        string         `json:"moonset"`
	QualifierCode  *string        `json:"qualifier_code"`
	QualifierPhrase *string       `json:"qualifier_phrase"`
	Narrative      string         `json:"narrative"`
	MoonPhase      string         `json:"moon_phase"`
	MoonPhaseCode  string         `json:"moon_phase_code"`
	Day            *DayNightPart  `json:"day"`
	Night          *DayNightPart  `json:"night"`
}

// DayNightPart holds the forecast for a single day or night half.
type DayNightPart struct {
	FcstValid      int64   `json:"fcst_valid"`
	FcstValidLocal string  `json:"fcst_valid_local"`
	DayInd         string  `json:"day_ind"`
	Num            int     `json:"num"`
	Temp           int     `json:"temp"`
	Hi             int     `json:"hi"`
	Wc             int     `json:"wc"`
	Pop            int     `json:"pop"`
	PrecipType     string  `json:"precip_type"`
	Qpf            float64 `json:"qpf"`
	SnowQpf        float64 `json:"snow_qpf"`
	Rh             int     `json:"rh"`
	Wspd           int     `json:"wspd"`
	Wdir           int     `json:"wdir"`
	WdirCardinal   string  `json:"wdir_cardinal"`
	Clds           int     `json:"clds"`
	Narrative      string  `json:"narrative"`
	IconCode       int     `json:"icon_code"`
	IconExtd       int     `json:"icon_extd"`
	Phrase12Char   string  `json:"phrase_12char"`
	Phrase22Char   string  `json:"phrase_22char"`
	Phrase32Char   string  `json:"phrase_32char"`
	UvIndexRaw     float64 `json:"uv_index_raw"`
	UvIndex        int     `json:"uv_index"`
	UvDesc         string  `json:"uv_desc"`
	GolfIndex      *int    `json:"golf_index"`
	GolfCategory   string  `json:"golf_category"`
}

// AlertHeadlinesResponse mirrors the top-level shape returned by the
// /v1/geocode/{lat}/{lon}/alerts/headlines.json endpoint.
type AlertHeadlinesResponse struct {
	Metadata  ResponseMetadata `json:"metadata"`
	Alerts    []AlertHeadline  `json:"alerts"`
}

// AlertHeadline is a single active alert headline.
// Use DetailKey to retrieve full content via AlertDetails.
type AlertHeadline struct {
	Key           string `json:"key"`
	DetailKey     string `json:"detail_key"`
	MessageTypeCD string `json:"messageTypeCD"`
	MessageType   string `json:"messageType"`
	ProductIdentifier string `json:"productIdentifier"`
	Phenomena     string `json:"phenomena"`
	Significance  string `json:"significance"`
	EventTrackingNumber string `json:"eventTrackingNumber"`
	OfficeCd      string `json:"officeCd"`
	OfficeNm      string `json:"officeNm"`
	OfficeAdminDistrictCD string `json:"officeAdminDistrictCD"`
	OfficeCountryCd string `json:"officeCountryCd"`
	EventDescription string `json:"eventDescription"`
	AreaTypeCD    string `json:"areaTypeCD"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	EffectiveTimeLocal string `json:"effectiveTimeLocal"`
	EffectiveTimeLocalTzAbbr string `json:"effectiveTimeLocalTzAbbr"`
	ExpireTimeLocal string `json:"expireTimeLocal"`
	ExpireTimeLocalTzAbbr string `json:"expireTimeLocalTzAbbr"`
	ExpireTimeUTC int64  `json:"expireTimeUTC"`
	OnsetTimeLocal string `json:"onsetTimeLocal"`
	OnsetTimeLocalTzAbbr string `json:"onsetTimeLocalTzAbbr"`
	Severity      string `json:"severity"`
	Urgency       string `json:"urgency"`
	Certainty     string `json:"certainty"`
	CountyID      string `json:"countyID"`
	Identifier    string `json:"identifier"`
	Ping          string `json:"ping"`
	Source        string `json:"source"`
}

// AlertDetailsResponse mirrors the top-level shape returned by the
// /v1/alerts/{detailKey}/details.json endpoint.
type AlertDetailsResponse struct {
	Metadata ResponseMetadata `json:"metadata"`
	AlertDetail AlertDetail   `json:"alertDetail"`
}

// AlertDetail contains the full narrative and metadata for a single alert.
type AlertDetail struct {
	Key           string   `json:"key"`
	DetailKey     string   `json:"detail_key"`
	Source        string   `json:"source"`
	EventDescription string `json:"eventDescription"`
	Severity      string   `json:"severity"`
	Urgency       string   `json:"urgency"`
	Certainty     string   `json:"certainty"`
	EffectiveTimeLocal string `json:"effectiveTimeLocal"`
	ExpireTimeLocal string `json:"expireTimeLocal"`
	OnsetTimeLocal  string `json:"onsetTimeLocal"`
	AreaName        string `json:"areaName"`
	CountyID        string `json:"countyID"`
	StateCode       string `json:"stateCode"`
	Identifier      string `json:"identifier"`
	MessageTypeCD   string `json:"messageTypeCD"`
	Phenomena       string `json:"phenomena"`
	Significance    string `json:"significance"`
	HeadlineText    string `json:"headlineText"`
	Overview        *AlertOverview `json:"overview"`
	Texts           []AlertText    `json:"texts"`
}

// AlertOverview is an optional short summary block within an AlertDetail.
type AlertOverview struct {
	Overview string `json:"overview"`
}

// AlertText holds a paragraph of the full alert narrative.
type AlertText struct {
	LanguageCd  string `json:"languageCd"`
	Description string `json:"description"`
	Instruction string `json:"instruction"`
}

// ResponseMetadata is the envelope present on every API response.
type ResponseMetadata struct {
	Language      string  `json:"language"`
	TransactionID string  `json:"transaction_id"`
	Version       string  `json:"version"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Units         string  `json:"units"`
	ExpireTimeGmt int64   `json:"expire_time_gmt"`
	StatusCode    int     `json:"status_code"`
}
