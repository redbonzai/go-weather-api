# weather-api

HTTP service written in Go. Includes interview-practice endpoints plus a **takehome-aligned** National Weather Service summary.

## Build and run

```bash
go build -o weather-api ./cmd/api
./weather-api
```

Or:

```bash
go run ./cmd/api
```

Listens on **http://localhost:8080** by default. Override with `LISTEN_ADDR` (e.g. `LISTEN_ADDR=:3000 ./weather-api`).

### Kubernetes

- **Docker:** `make docker-build` (override `IMAGE=registry/repo:tag`) or `docker build -t weather-api:local .`
- **Cluster:** `kubectl apply -k deploy/k8s` (uses [deploy/k8s/base](deploy/k8s/base)).
- **Local cluster:** enable Kubernetes (Docker Desktop) or `minikube start`, then `make k8s-local-up`. If you see **`ErrImageNeverPull`**, the cluster doesn’t see `docker build` images — run **`kind load docker-image weather-api:local --name kind`** (or `KIND_CLUSTER=… make k8s-local-maybe-load`). Details: [deploy/k8s/README.md](deploy/k8s/README.md).

### Where `WEATHER_API_KEY` comes from (Weather Company / IBM)

That key is **not** from weather.gov, not from this repo, and not something you can “look up” online for free. It is issued **only** when you use **IBM’s The Weather Company Data APIs** (commercial product; `api.weather.com`).

**Typical path:**

1. Open **[The Weather Company Data — developer hub](https://developer.weather.com/)**.
2. Follow **[Getting started](https://developer.weather.com/docs/getting-started)** — register, request access / a trial (flow depends on what IBM offers at the time; you may get a key in a **customer portal** after signup).
3. Read the docs for the package you bought or trialed (e.g. **[API Developer Package](https://developer.weather.com/docs/api-developer-package)**), then copy the **API key** into your environment:
   ```bash
   export WEATHER_API_KEY='paste-key-here'
   ```

There is **no** universal public key. If you cannot or do not want to go through IBM, **do not use** `/weather/current`, `/weather/forecast/*`, or `/weather/alerts*` — use **`GET /weather?lat=&lon=`** (NWS) instead; that path only needs **`NWS_USER_AGENT`**, not `WEATHER_API_KEY`.

If **`WEATHER_API_KEY`** is unset, those Weather Company routes return **503** with a short message (the server does not call IBM with an empty key).

### `GET /conditions` and `POST /events`

- **`/conditions`** needs a **`zip`** query parameter. Calling `/conditions` without `?zip=` returns **400** `missing zip`.
- **`/conditions/`** (with trailing slash) responds with **307** to **`/conditions`** and the same query; browsers follow automatically; with `curl`, add **`-L`**.
- **`/events`** must be **POST**. An **empty body** is accepted (same as `{}`). **`GET /events`** returns **405** Method Not Allowed.

## Takehome endpoint (National Weather Service)

**`GET /weather?lat={latitude}&lon={longitude}`**

Requests go to the **API host** [https://api.weather.gov](https://api.weather.gov) (see [API documentation](https://www.weather.gov/documentation/services-web-api)). Flow: `GET /points/{lat},{lon}`, then `GET` the grid `forecast` URL from the response. Returns JSON:

- `short_forecast` — NWS `shortForecast` for the **Today** period when present, otherwise the first forecast period.
- `temperature_feel` — `hot`, `cold`, or `moderate` from the period’s temperature (converted to °F when the API uses °C):
  - **cold:** below 50 °F  
  - **hot:** above 82 °F  
  - **moderate:** otherwise  

NWS requires a descriptive `User-Agent`. Set:

```bash
export NWS_USER_AGENT="YourAppName (you@example.com)"
```

If unset, a generic default is sent (fine for local dev; use your own for production).

Example (Washington, DC):

```bash
curl -s 'http://localhost:8080/weather?lat=38.8951&lon=-77.0364'
```

## Other endpoints

| Path | Notes |
|------|--------|
| `GET /health` | Liveness |
| `GET /forecast?lat=&lon=` | Cached stub upstream (interview scenario) |
| `GET /conditions?zip=` | Aggregates stub upstreams by ZIP (**`zip` required**) |
| `POST /events` | Queue JSON body (empty body OK); returns **202** |
| `GET /metrics` | Prometheus metrics |
| `GET /weather?lat=&lon=` | NWS takehome summary (not Weather Company) |
| `GET /weather/current?lat=&lon=` | Current conditions ([Weather Company](https://developer.weather.com/); needs `WEATHER_API_KEY`) |
| `GET /weather/forecast/hourly?lat=&lon=` | Hourly forecast (**not** `/weather/current/hourly`) |
| `GET /weather/forecast/daily?lat=&lon=` | Daily forecast (**not** `/weather/current/daily`) |
| `GET /weather/alerts?lat=&lon=` | Alert headlines |
| `GET /weather/alerts/detail?key=` | Single alert detail |

Typos **`/weather/current/hourly`** and **`/weather/current/daily`** are redirected (307) to the **`/weather/forecast/...`** paths above. Trailing slashes on these paths also redirect to the canonical URL (use `curl -L` if needed).

## Shortcut (takehome vs interview code)

`/forecast` and `/conditions` use **simulated** dependencies. The takehome requirement (**NWS** + today’s short forecast + temperature feel) is implemented only on **`GET /weather`**.
