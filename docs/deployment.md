# Deployment Guide

> **For:** Rapture Android and Rapture iOS applications
> **Platform:** Render.com

## Prerequisites

Before deploying the Token Broker, you need:

1. ✅ **Google Cloud Project** with Speech-to-Text and Gemini APIs enabled
2. ✅ **Service Account** with appropriate permissions
3. ✅ **OAuth 2.0 Client ID** for OIDC verification
4. ✅ **Render.com Account** (free or paid)
5. ✅ **GitHub Repository** (already created: https://github.com/NoiseMeldOrg/rapture-tokenbroker)

## Step 1: Create Google Cloud Service Account

### 1.1 Create the Service Account

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Select your project (or create a new one)
3. Navigate to **IAM & Admin** → **Service Accounts**
4. Click **Create Service Account**
5. Fill in details:
   - **Name:** `rapture-token-broker`
   - **Description:** `Service account for minting access tokens in the Rapture token broker`
6. Click **Create and Continue**

### 1.2 Grant Required Roles

Grant the following roles:
- **Service Account Token Creator** - Allows creating tokens
- **Cloud Speech Client** - Access to Speech-to-Text API (if not already granted)
- **Vertex AI User** - Access to Gemini API (if using Vertex AI)

Click **Continue** → **Done**

### 1.3 Create and Download Key

1. Find your new service account in the list
2. Click the three-dot menu → **Manage keys**
3. Click **Add Key** → **Create new key**
4. Select **JSON** format
5. Click **Create**
6. Save the downloaded JSON file securely (you'll need this for Render)

**⚠️ IMPORTANT:** This JSON file contains sensitive credentials. Never commit it to git!

## Step 2: Create OAuth 2.0 Client ID

### 2.1 Configure OAuth Consent Screen (if not already done)

1. Navigate to **APIs & Services** → **OAuth consent screen**
2. Select **External** user type (unless you have Google Workspace)
3. Fill in app information:
   - **App name:** Rapture
   - **User support email:** Your email
   - **Developer contact information:** Your email
4. Add scopes (optional for token broker):
   - `openid`
   - `email`
   - `profile`
5. Save and continue

### 2.2 Create Web Application Client ID

1. Navigate to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **OAuth 2.0 Client ID**
3. Select **Application type:** Web application
4. Fill in details:
   - **Name:** `Rapture Token Broker OIDC`
   - **Authorized JavaScript origins:** (leave empty)
   - **Authorized redirect URIs:** (leave empty - we're using OIDC verification only)
5. Click **Create**
6. Copy the **Client ID** (looks like: `123456789-abcdef.apps.googleusercontent.com`)
7. Save it - you'll need this for Render environment variables

**Note:** This is different from your Android/iOS OAuth client IDs. This client ID is specifically for the token broker to verify ID tokens.

## Step 3: Deploy to Render

### 3.1 Create New Web Service

1. Go to [Render Dashboard](https://dashboard.render.com/)
2. Click **New** → **Web Service**
3. Click **Connect a repository**
4. Authorize Render to access your GitHub account (if first time)
5. Select repository: `NoiseMeldOrg/rapture-tokenbroker`
6. Click **Connect**

### 3.2 Configure Service

Render will auto-detect `render.yaml` and pre-fill most settings:

- **Name:** `rapture-tokenbroker` (or customize)
- **Environment:** `Go`
- **Region:** Choose closest to your users (e.g., Oregon for US West)
- **Branch:** `main`
- **Build Command:** `go build -o server .`
- **Start Command:** `./server`

### 3.3 Select Plan

**For Development:**
- Select **Free** plan
- ⚠️ Service will sleep after 15 minutes of inactivity
- Cold start takes ~30 seconds
- Good for testing only

**For Production:**
- Select **Standard** plan ($7/month)
- Minimum 2 instances (no sleep)
- Auto-scaling enabled (up to 20 instances)
- Health checks enabled
- Recommended for live apps

### 3.4 Set Environment Variables

Click **Advanced** → **Add Environment Variable**

Add the following:

#### Required Variables

| Key | Value | Notes |
|-----|-------|-------|
| `GOOGLE_SA_JSON` | Paste entire Service Account JSON | The file you downloaded in Step 1.3 |
| `OIDC_CLIENT_ID` | `123456789-abcdef.apps.googleusercontent.com` | The Client ID from Step 2.2 |

**⚠️ Important:**
- For `GOOGLE_SA_JSON`, click **Add from file** or paste the entire JSON content
- Make sure to paste the ENTIRE JSON (starts with `{` and ends with `}`)
- Do NOT add quotes around the JSON

#### Optional Variables (with defaults)

| Key | Default | Purpose |
|-----|---------|---------|
| `TOKEN_SCOPE` | `https://www.googleapis.com/auth/cloud-platform` | GCP API scope |
| `CORS_ORIGIN` | `*` | CORS allowed origin (set to your app URL in production) |
| `PORT` | `10000` | Port to listen on (Render uses this automatically) |
| `RATE_PER_MIN` | `60` | Requests per user per minute |
| `RATE_BURST` | `30` | Burst tokens per user |
| `IP_RATE_PER_MIN` | `120` | Requests per IP per minute |
| `IP_BURST` | `60` | Burst tokens per IP |
| `RATE_CLEANUP_MINS` | `30` | Cleanup idle limiters after N minutes |

**Optional: Domain Restriction**
- `ALLOWED_HD` - Restrict to Google Workspace domain (e.g., `noisemeld.com`)
- Leave empty to allow all Google users

### 3.5 Deploy

1. Click **Create Web Service**
2. Render will:
   - Clone your repository
   - Run `go build -o server .`
   - Start the service with `./server`
   - Begin health checks on `/healthz`

3. Wait for deployment to complete (~2-3 minutes)
4. You'll see: **"Your service is live 🎉"**

### 3.6 Note Your Service URL

Render will assign a URL like:
```
https://rapture-tokenbroker.onrender.com
```

**Save this URL** - you'll need it for mobile app integration.

## Step 4: Verify Deployment

### 4.1 Test Health Check

```bash
curl https://rapture-tokenbroker.onrender.com/healthz
```

Expected response:
```
ok
```

### 4.2 Test with Google ID Token

**Get a Google ID token** from your Android/iOS app (see integration docs), then:

```bash
curl -H "Authorization: Bearer YOUR_GOOGLE_ID_TOKEN" \
     https://rapture-tokenbroker.onrender.com/whoami
```

Expected response:
```json
{
  "sub": "1234567890",
  "email": "you@example.com",
  "name": "Your Name",
  ...
}
```

### 4.3 Test Token Exchange

```bash
curl -H "Authorization: Bearer YOUR_GOOGLE_ID_TOKEN" \
     https://rapture-tokenbroker.onrender.com/token
```

Expected response:
```json
{
  "access_token": "ya29.c.b0Aaekm1...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

## Step 5: Update Mobile Apps

### Rapture Android

Update `build.gradle.kts` or a constants file:

```kotlin
// app/src/main/java/com/noisemeld/rapture/Constants.kt
object ApiConstants {
    const val TOKEN_BROKER_URL = "https://rapture-tokenbroker.onrender.com"
}
```

See [android-integration.md](android-integration.md) for full integration guide.

### Rapture iOS (Future)

Similar pattern - create a constants file with the token broker URL.

See [ios-integration.md](ios-integration.md) when available.

## Monitoring & Maintenance

### View Logs

1. Go to [Render Dashboard](https://dashboard.render.com/)
2. Select your service: `rapture-tokenbroker`
3. Click **Logs** tab
4. View real-time logs

### Monitor Metrics

1. Click **Metrics** tab
2. View:
   - Request count
   - CPU usage
   - Memory usage
   - Response times

### Set Up Alerts

1. Click **Settings** → **Notifications**
2. Add email or Slack webhook
3. Configure alerts for:
   - Service down
   - High CPU (>80%)
   - High memory (>80%)
   - Error rate spike

## Scaling

### Auto-Scaling (Already Configured)

From `render.yaml`:
```yaml
autoScaling:
  minInstances: 2
  maxInstances: 20
  cpuThreshold: 60
  memoryThreshold: 70
```

- **Minimum 2 instances** for high availability
- **Scales up to 20 instances** under load
- **Triggers:** CPU >60% or Memory >70%
- **Cost:** $7/month for 2 instances, $3.50/month per additional instance

### Manual Scaling (if needed)

1. Go to service **Settings**
2. Edit `render.yaml` in GitHub
3. Adjust `minInstances` and `maxInstances`
4. Commit and push - Render auto-deploys

## Security Best Practices

### Production Checklist

- [ ] Use **Standard plan** (not Free - Free tier sleeps)
- [ ] Set `CORS_ORIGIN` to your app's URL (not `*`)
- [ ] Enable **Auto Deploy** for automatic security updates
- [ ] Set up **Slack/email alerts** for downtime
- [ ] Review logs weekly for suspicious activity
- [ ] Rotate Service Account keys every 90 days
- [ ] Monitor rate limit hits (may indicate abuse)

### Optional: Add Domain Restriction

If you only want to allow your Google Workspace users:

1. Add environment variable: `ALLOWED_HD=noisemeld.com`
2. Redeploy service
3. Only users with `@noisemeld.com` emails can get tokens

## Troubleshooting

### "Invalid ID token" Error

**Cause:** OIDC_CLIENT_ID doesn't match the ID token's `aud` claim

**Fix:**
1. Get a fresh ID token from your mobile app
2. Decode it at https://jwt.io/
3. Check the `aud` field matches your `OIDC_CLIENT_ID` env var
4. Make sure you're using the **Web Application Client ID** (not Android/iOS client IDs)

### "Token mint failed" Error

**Cause:** Service Account JSON is invalid or missing permissions

**Fix:**
1. Verify `GOOGLE_SA_JSON` env var is the complete JSON
2. Check Service Account has "Service Account Token Creator" role
3. Check Service Account has access to Speech-to-Text and Gemini APIs
4. Try creating a new Service Account key

### "Rate limit (user)" Error

**Cause:** User exceeded 60 requests/minute

**Fix:**
- This is normal - user should wait for the `Retry-After` seconds
- If legitimate high usage, increase `RATE_PER_MIN` env var
- Check mobile app isn't making excessive requests (possible bug)

### Service Sleeping (Free Tier)

**Cause:** Free tier services sleep after 15 minutes of inactivity

**Fix:**
- Upgrade to Standard plan ($7/month)
- Or: Accept 30-second cold start on first request after sleep

## Cost Breakdown

### Render Hosting

| Plan | Instances | Cost/Month | Notes |
|------|-----------|------------|-------|
| Free | 1 | $0 | Sleeps after 15 min, 750 hrs/month |
| Standard | 2 | $7 | Always on, auto-scaling |
| Standard | 3-20 | $3.50 each | Auto-scales under load |

**Example:** 2 instances always + 3 instances during peak hours (8hrs/day) = $7 + ($3.50 × 3 × 8/24) = $10.50/month

### Total Infrastructure Costs

- **Token Broker:** $7/month (Standard plan, 2 instances)
- **Database:** $0 (stateless, no database needed)
- **Total:** **$7/month**

**vs. Old Architecture:**
- Trial backend: $7/month (web) + $7/month (PostgreSQL) = $14/month
- **Savings:** $7/month (50% reduction)

---

**Next Steps:**
1. ✅ Service deployed and verified
2. → Integrate with Rapture Android app (see [android-integration.md](android-integration.md))
3. → (Future) Integrate with Rapture iOS app (see [ios-integration.md](ios-integration.md))
