# Token Broker Overview

> **For:** Rapture Android and Rapture iOS applications
> **Purpose:** Secure API key management for Google Cloud services

## What is the Token Broker?

The Token Broker is a lightweight Go service that securely manages Google Cloud API keys for the Rapture voice recording applications. It acts as a secure intermediary between the mobile apps and Google Cloud services (Speech-to-Text API and Gemini API).

## Why Do We Need It?

### The Problem

Mobile apps need API keys to access Google Cloud services, but:
- ❌ **Hardcoded keys in apps can be extracted** - Anyone can decompile an APK/IPA and steal your API keys
- ❌ **Stolen keys = unlimited usage** - Attackers can run up massive bills on your account
- ❌ **No per-user limits** - Can't rate-limit individual users if keys are shared in the app
- ❌ **Can't revoke access** - If a key leaks, you have to rebuild and redeploy the entire app

### The Solution

The Token Broker service:
- ✅ **Hides API keys on the server** - Keys stored as environment variables, never in mobile app code
- ✅ **Verifies user identity** - Only authenticated Google users can get access tokens
- ✅ **Rate limits per user** - 60 requests/minute per user, 120/minute per IP
- ✅ **Short-lived tokens** - Access tokens expire in 1 hour (auto-renewed by Google)
- ✅ **Revocable access** - Update the backend without touching mobile apps
- ✅ **Audit trail** - Render logs show which users made requests

## How It Works

### Authentication Flow

```
┌─────────────────┐
│  Rapture App    │ 1. User signs in with Google
│  (Android/iOS)  │    (Google Sign-In SDK)
└────────┬────────┘
         │ 2. App gets Google ID token
         │    (JWT with user identity)
         ▼
┌─────────────────┐
│  Token Broker   │ 3. Broker verifies ID token
│  (Render.com)   │    (checks signature, audience, issuer)
└────────┬────────┘
         │ 4. Broker mints GCP access token
         │    (using Service Account)
         ▼
┌─────────────────┐
│  Google Cloud   │ 5. App uses access token
│  STT / Gemini   │    (for transcription/formatting)
└─────────────────┘
```

### Step-by-Step

1. **User signs into Rapture** using Google Sign-In
2. **App requests Google ID token** from Google Sign-In SDK
3. **App sends ID token to Token Broker** via `GET /token` endpoint
4. **Token Broker verifies the ID token** (checks it's valid and from the right OAuth client)
5. **Token Broker mints a GCP access token** using the Service Account credentials
6. **Token Broker returns access token** to the app (expires in 1 hour)
7. **App uses access token** to call Google Cloud Speech-to-Text and Gemini APIs

## Supported Applications

### Rapture for Android
- **Status:** In development (Phase 9)
- **Repository:** [rapture-android](https://github.com/NoiseMeldOrg/rapture-android)
- **Integration:** StreamingTranscriptionService, GeminiApiClient
- **Platform:** Android 7.0+ (API 24+)

### Rapture for iOS
- **Status:** Planned (post-launch)
- **Repository:** [rapture-ios](https://github.com/NoiseMeldOrg/rapture-ios)
- **Integration:** TBD (similar pattern to Android)
- **Platform:** iOS 15.0+

## Security Features

### Google OIDC Verification
- Validates ID token signature using Google's public keys
- Checks `aud` (audience) matches your OAuth 2.0 client ID
- Checks `iss` (issuer) is `https://accounts.google.com`
- Extracts user identity (`sub`, `email`, `name`)

### Rate Limiting
- **Per-User:** 60 requests/minute (burst: 30 tokens)
  - Keyed by Google `sub` (unique user ID)
  - Prevents individual users from abusing the API
- **Per-IP:** 120 requests/minute (burst: 60 tokens)
  - Pre-verification guard against DDoS
  - Protects before expensive OIDC verification

### Optional Domain Restriction
- Set `ALLOWED_HD` environment variable to restrict to your Google Workspace domain
- Example: `ALLOWED_HD=noisemeld.com` only allows `@noisemeld.com` users
- Useful for internal testing or enterprise deployments

### HTTPS Only
- All communication encrypted with TLS 1.3
- CORS configurable (default: `*`, restrict in production)
- No token logging (tokens never written to stdout)

## Endpoints

### `GET /healthz`
Health check endpoint for Render monitoring.

**Response:**
```
200 OK
ok
```

### `GET /whoami`
Debug endpoint to verify your Google ID token and see user claims.

**Request:**
```bash
curl -H "Authorization: Bearer <GOOGLE_ID_TOKEN>" \
     https://rapture-tokenbroker.onrender.com/whoami
```

**Response:**
```json
{
  "sub": "1234567890",
  "email": "user@example.com",
  "name": "John Doe",
  "picture": "https://lh3.googleusercontent.com/...",
  "hd": "example.com",
  "iss": "https://accounts.google.com",
  "aud": "YOUR_OAUTH_CLIENT_ID.apps.googleusercontent.com",
  "exp": 1698765432
}
```

### `GET /token`
Main endpoint - exchanges Google ID token for GCP access token.

**Request:**
```bash
curl -H "Authorization: Bearer <GOOGLE_ID_TOKEN>" \
     https://rapture-tokenbroker.onrender.com/token
```

**Response:**
```json
{
  "access_token": "ya29.c.b0Aaekm1...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

**Rate Limit Response (429):**
```
429 Too Many Requests
Retry-After: 42

rate limit (user)
```

## Cost & Scaling

### Render.com Hosting
- **Development:** Free tier (sleeps after 15 min inactivity)
- **Production:** Standard plan - $7/month
  - 2 minimum instances (no sleep)
  - Auto-scaling up to 20 instances
  - CPU threshold: 60%
  - Memory threshold: 70%

### No Database Costs
- Stateless service (no PostgreSQL, Redis, etc.)
- In-memory rate limiting (per instance)
- For global rate limiting, add Redis (future enhancement)

### API Costs
- $0 - Token broker doesn't call external paid APIs
- Google Cloud Speech-to-Text and Gemini costs paid by your Google Cloud project
- Service Account usage is free (no per-token charges)

## Deployment

See [deployment.md](deployment.md) for complete deployment instructions.

## Client Integration

See integration guides:
- [Android Integration](android-integration.md)
- [iOS Integration](ios-integration.md) (future)

## Monitoring & Logs

### Render Dashboard
- View real-time logs: https://dashboard.render.com/
- Monitor CPU, memory, request count
- Set up alerts for errors or rate limit hits

### Log Format
```
2025/10/31 17:00:00 listening on :10000
```

**Note:** Tokens are NEVER logged for security reasons.

## Future Enhancements

### Phase 10+: Potential Improvements
- **Redis-backed rate limiting** - Global limits across all instances
- **Usage analytics** - Track API usage per user
- **Webhook support** - Notify on quota exhaustion
- **Multi-region deployment** - Lower latency for global users
- **API key rotation** - Automated Service Account key rotation

---

**Repository:** https://github.com/NoiseMeldOrg/rapture-tokenbroker
**License:** TBD (recommend MIT or Apache 2.0)
**Maintainer:** Noise Meld (Bensolutions LLC)
