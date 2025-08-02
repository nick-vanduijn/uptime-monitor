# Status Checker

A modern, container-ready uptime and response time monitoring tool with a beautiful web dashboard.

## Features
- Monitors multiple HTTP(S) endpoints (default: Google, GitHub, Example, Cloudflare)
- Tracks uptime, response time, and error rates
- Real-time dashboard with HTMX and Chart.js
- Collapsible response time charts per service (click to expand/collapse)
- Toggle chevron icon for each service card
- Responsive, modern UI (Tailwind, Chart.js)
- Docker and Docker Compose support

## Quick Start

### Local (Go)
```sh
go run main.go
```
Visit [http://localhost:8080](http://localhost:8080)

### Docker Compose
```sh
docker-compose up --build
```
*Note: The build context is now `src/` and the frontend is served from `src/index.html`.*

## Configuration
- Set environment variables in `.env` or via Docker Compose:
  - `SERVICES` (comma-separated URLs)
  - `CHECK_INTERVAL` (e.g. `10s`)
  - `HTTP_PORT` (default: 8080)

## Endpoints
- `/` - Dashboard
- `/status` - JSON status
- `/status/html` - HTML status cards
- `/response_times` - JSON response time history
- `/metrics` - Prometheus metrics

## Development
- Frontend: edit `src/index.html` (HTMX, Chart.js, Tailwind)
- Backend: edit `main.go` (Go)
- Dockerfile: `src/Dockerfile`
- Compose: `docker-compose.yml` (builds from `src/`)

## License
MIT
