# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Rapture Token Broker** is a lightweight Go service that provides secure API key management for Google Cloud services (Speech-to-Text API and Gemini API) used by the Rapture mobile applications (Android and iOS).

**Purpose:** Exchange Google ID tokens for short-lived Google Cloud access tokens, preventing API key exposure in mobile apps.

**Repository:** https://github.com/NoiseMeldOrg/rapture-tokenbroker

## Architecture

### Single-File Service Design

This is a minimal stateless HTTP service implemented in a single [main.go](main.go) file with three main components:

1. **OIDC Verification** - Verifies Google ID tokens using the `github.com/coreos/go-oidc/v3` package
2. **Token Minting** - Creates short-lived GCP access tokens using Service Account credentials
3. **Rate Limiting** - In-memory per-user and per-IP rate limiting using `golang.org/x/time/rate`

### Key Components

- **`limiterRegistry`** (lines 70-135) - Thread-safe in-memory rate limiter with TTL-based cleanup
  - Maintains separate limiters for each user (`user:<sub>`) and IP (`ip:<address>`)
  - Uses token bucket algorithm via `golang.org/x/time/rate`
  - Automatically cleans up idle entries via background goroutine

- **HTTP Endpoints**:
  - `/healthz` - Health check for Render monitoring
  - `/whoami` - Debug endpoint returning decoded OIDC claims
  - `/token` - Main endpoint for token exchange

### Security Flow

```
Mobile App → Google Sign-In SDK → Google ID token
    ↓
Token Broker verifies ID token (audience, issuer, signature)
    ↓
Rate limit checks (IP-level, then user-level)
    ↓
Mint GCP access token from Service Account
    ↓
Return short-lived token (1 hour expiry)
```

## Development Commands

### Local Development

```bash
# Set required environment variables
export GOOGLE_SA_JSON="$(cat path/to/service-account.json)"
export OIDC_CLIENT_ID="YOUR_SERVER_OAUTH_CLIENT_ID.apps.googleusercontent.com"

# Run the service
go run .

# Alternative: build and run
go build -o server .
./server
```

The service listens on port 10000 by default (configurable via `PORT` env var).

### Testing Locally

```bash
# Health check
curl -i http://localhost:10000/healthz

# Test whoami endpoint (requires valid Google ID token)
curl -H "Authorization: Bearer <GOOGLE_ID_TOKEN>" \
     http://localhost:10000/whoami

# Test token exchange
curl -H "Authorization: Bearer <GOOGLE_ID_TOKEN>" \
     http://localhost:10000/token
```

### Building

```bash
# Build binary
go build -o server .

# Build for deployment (used by render.yaml)
go build -o server .
```

### Dependency Management

```bash
# Download dependencies
go mod download

# Update dependencies
go get -u ./...
go mod tidy

# Verify dependencies
go mod verify
```

## Environment Variables

### Required

- `GOOGLE_SA_JSON` - Complete Service Account JSON (from Google Cloud Console)
- `OIDC_CLIENT_ID` - OAuth 2.0 Web Application Client ID for token verification

### Optional (with defaults)

- `TOKEN_SCOPE` - GCP API scope (default: `https://www.googleapis.com/auth/cloud-platform`)
- `CORS_ORIGIN` - CORS allowed origin (default: `*`)
- `PORT` - HTTP server port (default: `10000`)
- `ALLOWED_HD` - Google Workspace domain restriction (e.g., `noisemeld.com`)

### Rate Limiting Configuration

- `RATE_PER_MIN` - Per-user requests/minute (default: `60`)
- `RATE_BURST` - Per-user burst tokens (default: `30`)
- `IP_RATE_PER_MIN` - Per-IP requests/minute (default: `120`)
- `IP_BURST` - Per-IP burst tokens (default: `60`)
- `RATE_CLEANUP_MINS` - Idle limiter cleanup interval (default: `30`)

## Deployment

This service is designed to run on Render.com using the [render.yaml](render.yaml) configuration.

### Render Configuration

- **Build Command:** `go build -o server .`
- **Start Command:** `./server`
- **Health Check:** `/healthz`
- **Auto-Scaling:** 2-20 instances (CPU: 60%, Memory: 70%)

### Deploy Steps

1. Push to GitHub main branch
2. Render auto-deploys via `autoDeploy: true`
3. Health checks verify service is ready
4. Service URL: `https://rapture-tokenbroker.onrender.com`

See [docs/deployment.md](docs/deployment.md) for complete deployment instructions.

## Code Patterns

### Error Handling

- Use `log.Fatalf()` for startup errors (missing env vars, OIDC provider init)
- Use `http.Error()` for runtime request errors (401, 403, 429, 500)
- Never log tokens or sensitive data

### Rate Limiting Pattern

1. Check IP-based limiter first (pre-verification guard)
2. Verify OIDC token
3. Check per-user limiter (after identity known)
4. Return `429 Too Many Requests` with `Retry-After` header on limit exceeded

### CORS Handling

- `enableCORS()` sets headers for all routes
- OPTIONS requests return `204 No Content`
- Global CORS wrapper at the handler level

## Related Repositories

- **Rapture Android:** https://github.com/NoiseMeldOrg/rapture-android
  - Integrates with `StreamingTranscriptionService` and `GeminiApiClient`
  - Uses Google Sign-In SDK to get ID tokens
- **Rapture iOS:** https://github.com/NoiseMeldOrg/rapture-ios (planned)

## Documentation

- [docs/overview.md](docs/overview.md) - What is the token broker and why do we need it?
- [docs/deployment.md](docs/deployment.md) - Step-by-step deployment to Render.com
- [docs/android-integration.md](docs/android-integration.md) - Integrate with Rapture Android app
- [docs/ios-integration.md](docs/ios-integration.md) - Integrate with Rapture iOS app (future)

## Important Notes

- **Stateless Design:** No database required - all rate limiting is in-memory per instance
- **Security:** HTTPS only, tokens never logged, OIDC signature verification
- **Scaling:** For global rate limits across instances, would need Redis-backed limiter
- **Go Version:** Requires Go 1.22+ (see [go.mod](go.mod))
