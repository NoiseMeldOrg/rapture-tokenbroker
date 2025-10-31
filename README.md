# Token Broker (Go + Render + Google OIDC → Google Cloud Access Token)

Verifies a **Google ID token** (Google Sign-In / One-Tap), then mints a **short-lived Google Cloud access token** from a **Service Account**.
Includes `/whoami` for debugging and **rate limiting** to protect the broker.

## Endpoints

| Endpoint  | Method | Description |
|-----------|--------|-------------|
| `/healthz` | GET   | Health check |
| `/whoami`  | GET   | Verify OIDC and return decoded claims (email/name/hd/sub) |
| `/token`   | GET   | Verify OIDC, then return `{ access_token, token_type, expires_in }` |

## Rate limiting

- Two token buckets:
  - **Per-user** (keyed by the Google OIDC `sub`)
  - **Per-IP** (pre-verification guard)
- If a request exceeds the limit, responds **429** with `Retry-After: <seconds>`.

### Env knobs

| Var | Default | Meaning |
|-----|---------|---------|
| `RATE_PER_MIN` | `60` | Allowed requests **per user** per minute |
| `RATE_BURST` | `30` | Burst tokens per user |
| `IP_RATE_PER_MIN` | `120` | Allowed requests **per IP** per minute |
| `IP_BURST` | `60` | Burst tokens per IP |
| `RATE_CLEANUP_MINS` | `30` | Evict idle limiter entries after N minutes |

> For multi-instance autoscaling, this in-memory limiter is **per instance**. For strict global limits, use a shared store (e.g., Redis) and a distributed rate limiter.

## Environment variables

Required:
- `GOOGLE_SA_JSON` – full Service Account JSON
- `OIDC_CLIENT_ID` – your **server** OAuth client ID

Optional:
- `TOKEN_SCOPE` (default `https://www.googleapis.com/auth/cloud-platform`)
- `CORS_ORIGIN` (default `*`)
- `ALLOWED_HD` (Workspace domain restriction)
- `PORT` (default `10000`)

**Rate limiting** (see table above).

## Local run

```bash
export GOOGLE_SA_JSON="$(cat path/to/sa.json)"
export OIDC_CLIENT_ID="YOUR_SERVER_OAUTH_CLIENT_ID"
go run .
```

Test:
```bash
curl -i http://localhost:10000/healthz
curl -H "Authorization: Bearer <GOOGLE_ID_TOKEN>" http://localhost:10000/whoami
curl -H "Authorization: Bearer <GOOGLE_ID_TOKEN>" http://localhost:10000/token
```

## Deploy to Render

Push to GitHub → Render → New → Web Service → connect repo.
Set env vars (GOOGLE_SA_JSON, OIDC_CLIENT_ID, etc.).
Uses render.yaml for health check and autoscaling.

## Android usage (snippet)

```kotlin
// Get Google ID token for OIDC_CLIENT_ID
val idToken: String = googleAccount.idToken!!

// Exchange for short-lived Google Cloud access token
val req = Request.Builder()
  .url("https://<service>.onrender.com/token")
  .header("Authorization", "Bearer $idToken")
  .build()
```

## Security

- `/token` & `/whoami` require a valid Google ID token (audience & issuer checked).
- Optional `ALLOWED_HD` to restrict domain.
- HTTPS only; don't log tokens.
