# Android Integration Guide

> **For:** Rapture for Android
> **Repository:** [rapture-android](https://github.com/NoiseMeldOrg/rapture-android)
> **Phase:** 9 (Token Broker Backend)

## Overview

This guide shows how to integrate the Token Broker into the Rapture Android app to securely access Google Cloud Speech-to-Text and Gemini APIs.

## Architecture

**Before Token Broker (Phase 5-8):**
```kotlin
// ❌ API key exposed in BuildConfig
val apiKey = BuildConfig.GOOGLE_CLOUD_API_KEY // Extractable from APK!

val credentials = MetadataUtils.newAttachHeadersInterceptor(
    Metadata().apply {
        put(Metadata.Key.of("x-goog-api-key", Metadata.ASCII_STRING_MARSHALLER), apiKey)
    }
)
```

**After Token Broker (Phase 9+):**
```kotlin
// ✅ Get short-lived token from broker (hidden API key)
val accessToken = tokenBrokerClient.getAccessToken()

val credentials = MetadataUtils.newAttachHeadersInterceptor(
    Metadata().apply {
        put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER),
            "Bearer $accessToken")
    }
)
```

## Step 1: Add Constants

Create or update `ApiConstants.kt`:

```kotlin
// app/src/main/java/com/noisemeld/rapture/utils/ApiConstants.kt
package com.noisemeld.rapture.utils

object ApiConstants {
    // Token Broker URL (set in BuildConfig or here)
    const val TOKEN_BROKER_URL = "https://rapture-tokenbroker.onrender.com"

    // Endpoints
    const val TOKEN_ENDPOINT = "$TOKEN_BROKER_URL/token"
    const val WHOAMI_ENDPOINT = "$TOKEN_BROKER_URL/whoami"
    const val HEALTH_ENDPOINT = "$TOKEN_BROKER_URL/healthz"
}
```

**Alternative:** Use BuildConfig for environment-specific URLs:

```kotlin
// In app/build.gradle.kts
android {
    buildTypes {
        debug {
            buildConfigField("String", "TOKEN_BROKER_URL",
                "\"https://rapture-tokenbroker-dev.onrender.com\"")
        }
        release {
            buildConfigField("String", "TOKEN_BROKER_URL",
                "\"https://rapture-tokenbroker.onrender.com\"")
        }
    }
}

// Then use:
object ApiConstants {
    const val TOKEN_BROKER_URL = BuildConfig.TOKEN_BROKER_URL
}
```

## Step 2: Create TokenBrokerClient

```kotlin
// app/src/main/java/com/noisemeld/rapture/services/TokenBrokerClient.kt
package com.noisemeld.rapture.services

import android.content.Context
import android.util.Log
import com.noisemeld.rapture.auth.GoogleAuthManager
import com.noisemeld.rapture.utils.ApiConstants
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.TimeUnit

data class AccessTokenResponse(
    val accessToken: String,
    val tokenType: String,
    val expiresIn: Int
)

class TokenBrokerClient(private val context: Context) {

    companion object {
        private const val TAG = "TokenBrokerClient"
        private const val CACHE_KEY = "gcp_access_token"
        private const val CACHE_EXPIRY_KEY = "gcp_access_token_expiry"

        @Volatile
        private var instance: TokenBrokerClient? = null

        fun getInstance(context: Context): TokenBrokerClient {
            return instance ?: synchronized(this) {
                instance ?: TokenBrokerClient(context.applicationContext).also {
                    instance = it
                }
            }
        }
    }

    private val httpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(10, TimeUnit.SECONDS)
        .build()

    private val authManager = GoogleAuthManager.getInstance(context)
    private val prefs = context.getSharedPreferences("token_broker", Context.MODE_PRIVATE)

    /**
     * Get a valid GCP access token (from cache or by requesting from broker).
     * Automatically refreshes if token is expired or missing.
     */
    suspend fun getAccessToken(): String = withContext(Dispatchers.IO) {
        // Check cache first
        val cachedToken = getCachedToken()
        if (cachedToken != null) {
            Log.d(TAG, "Using cached GCP access token")
            return@withContext cachedToken
        }

        // Cache miss or expired - request new token from broker
        Log.d(TAG, "Requesting new GCP access token from broker")
        val response = requestAccessToken()

        // Cache the token
        cacheToken(response.accessToken, response.expiresIn)

        response.accessToken
    }

    /**
     * Request a fresh access token from the token broker.
     */
    private suspend fun requestAccessToken(): AccessTokenResponse {
        // Get Google ID token from auth manager
        val idToken = authManager.getIdToken()
            ?: throw IllegalStateException("User not signed in - cannot get ID token")

        // Call token broker
        val request = Request.Builder()
            .url(ApiConstants.TOKEN_ENDPOINT)
            .header("Authorization", "Bearer $idToken")
            .get()
            .build()

        val response = httpClient.newCall(request).execute()

        if (!response.isSuccessful) {
            val errorBody = response.body?.string() ?: "Unknown error"

            // Handle rate limiting
            if (response.code == 429) {
                val retryAfter = response.header("Retry-After")?.toIntOrNull() ?: 60
                throw IOException("Rate limited - retry after $retryAfter seconds")
            }

            throw IOException("Token broker request failed: ${response.code} - $errorBody")
        }

        val body = response.body?.string()
            ?: throw IOException("Empty response from token broker")

        val json = JSONObject(body)
        return AccessTokenResponse(
            accessToken = json.getString("access_token"),
            tokenType = json.getString("token_type"),
            expiresIn = json.getInt("expires_in")
        )
    }

    /**
     * Get cached token if valid and not expired.
     */
    private fun getCachedToken(): String? {
        val token = prefs.getString(CACHE_KEY, null) ?: return null
        val expiry = prefs.getLong(CACHE_EXPIRY_KEY, 0L)

        // Check if expired (with 5-minute buffer for safety)
        val now = System.currentTimeMillis()
        val bufferMs = 5 * 60 * 1000 // 5 minutes

        return if (now < (expiry - bufferMs)) {
            token
        } else {
            Log.d(TAG, "Cached token expired")
            null
        }
    }

    /**
     * Cache the access token with expiry time.
     */
    private fun cacheToken(token: String, expiresInSeconds: Int) {
        val expiryTime = System.currentTimeMillis() + (expiresInSeconds * 1000L)
        prefs.edit()
            .putString(CACHE_KEY, token)
            .putLong(CACHE_EXPIRY_KEY, expiryTime)
            .apply()

        Log.d(TAG, "Cached GCP access token (expires in ${expiresInSeconds}s)")
    }

    /**
     * Clear cached token (call on sign-out or if token becomes invalid).
     */
    fun clearCache() {
        prefs.edit()
            .remove(CACHE_KEY)
            .remove(CACHE_EXPIRY_KEY)
            .apply()

        Log.d(TAG, "Cleared GCP access token cache")
    }

    /**
     * Debug method - verify Google ID token with /whoami endpoint.
     */
    suspend fun whoami(): JSONObject? = withContext(Dispatchers.IO) {
        try {
            val idToken = authManager.getIdToken() ?: return@withContext null

            val request = Request.Builder()
                .url(ApiConstants.WHOAMI_ENDPOINT)
                .header("Authorization", "Bearer $idToken")
                .get()
                .build()

            val response = httpClient.newCall(request).execute()

            if (response.isSuccessful) {
                val body = response.body?.string() ?: return@withContext null
                JSONObject(body)
            } else {
                Log.w(TAG, "whoami failed: ${response.code}")
                null
            }
        } catch (e: Exception) {
            Log.e(TAG, "whoami error", e)
            null
        }
    }
}
```

## Step 3: Update GoogleAuthManager

Add a method to get the Google ID token:

```kotlin
// app/src/main/java/com/noisemeld/rapture/auth/GoogleAuthManager.kt
class GoogleAuthManager private constructor(private val context: Context) {

    // ... existing code ...

    /**
     * Get the Google ID token for the currently signed-in user.
     * This token is used to authenticate with the token broker.
     */
    suspend fun getIdToken(): String? = withContext(Dispatchers.IO) {
        try {
            val account = GoogleSignIn.getLastSignedInAccount(context)
            if (account == null) {
                Log.w(TAG, "No signed-in account found")
                return@withContext null
            }

            // Request fresh ID token
            val scope = Scope(DriveScopes.DRIVE_FILE)
            val serverAuthCode = account.serverAuthCode

            // ID token is in the GoogleSignInAccount object
            account.idToken?.also {
                Log.d(TAG, "Got ID token for user: ${account.email}")
            }
        } catch (e: Exception) {
            Log.e(TAG, "Failed to get ID token", e)
            null
        }
    }

    // ... rest of existing code ...
}
```

## Step 4: Update StreamingTranscriptionService

Replace direct API key usage with token broker:

```kotlin
// app/src/main/java/com/noisemeld/rapture/transcription/StreamingTranscriptionService.kt
class StreamingTranscriptionService(private val context: Context) {

    private val tokenBrokerClient = TokenBrokerClient.getInstance(context)

    // ... existing code ...

    suspend fun startSession(listener: TranscriptionListener) {
        // ... existing session setup ...

        // ❌ OLD: Direct API key from BuildConfig
        // val credentials = MetadataUtils.newAttachHeadersInterceptor(
        //     Metadata().apply {
        //         put(Metadata.Key.of("x-goog-api-key", Metadata.ASCII_STRING_MARSHALLER),
        //             BuildConfig.GOOGLE_CLOUD_API_KEY)
        //     }
        // )

        // ✅ NEW: Get access token from token broker
        val accessToken = tokenBrokerClient.getAccessToken()
        val credentials = MetadataUtils.newAttachHeadersInterceptor(
            Metadata().apply {
                put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER),
                    "Bearer $accessToken")
            }
        )

        // Build gRPC channel with credentials
        channel = ManagedChannelBuilder
            .forAddress("speech.googleapis.com", 443)
            .useTransportSecurity()
            .intercept(credentials)
            .build()

        // ... rest of existing code ...
    }

    // ... rest of existing code ...
}
```

## Step 5: Update GeminiApiClient

Replace direct API key usage with token broker:

```kotlin
// app/src/main/java/com/noisemeld/rapture/services/GeminiApiClient.kt
class GeminiApiClient(private val context: Context) {

    private val tokenBrokerClient = TokenBrokerClient.getInstance(context)

    // ... existing code ...

    suspend fun scrubTranscription(rawText: String): String {
        // ❌ OLD: Direct API key from BuildConfig
        // val apiKey = BuildConfig.GEMINI_API_KEY
        // val url = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key=$apiKey"

        // ✅ NEW: Get access token from token broker
        val accessToken = tokenBrokerClient.getAccessToken()
        val url = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"

        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer $accessToken") // Use bearer token instead
            .header("Content-Type", "application/json")
            .post(requestBody.toRequestBody("application/json".toMediaType()))
            .build()

        // ... rest of existing code ...
    }

    suspend fun formatNote(cleanedText: String): String {
        // Same pattern - use token broker instead of API key
        val accessToken = tokenBrokerClient.getAccessToken()
        // ... rest of existing code ...
    }
}
```

## Step 6: Handle Sign-Out

Clear the token cache when user signs out:

```kotlin
// app/src/main/java/com/noisemeld/rapture/ui/settings/SettingsFragment.kt
class SettingsFragment : Fragment() {

    private val tokenBrokerClient by lazy { TokenBrokerClient.getInstance(requireContext()) }

    private fun handleSignOut() {
        // Existing sign-out logic
        GoogleAuthManager.getInstance(requireContext()).signOut()

        // Clear token broker cache
        tokenBrokerClient.clearCache()

        // Navigate to onboarding
        // ... existing code ...
    }
}
```

## Step 7: Remove Sensitive API Keys from BuildConfig

Once the token broker is integrated and working, remove API keys from `local.properties` and `build.gradle.kts`:

```kotlin
// app/build.gradle.kts

android {
    // ❌ REMOVE THESE (no longer needed):
    // buildConfigField("String", "GOOGLE_CLOUD_API_KEY", "\"${localProps["GOOGLE_CLOUD_API_KEY"]}\"")
    // buildConfigField("String", "GEMINI_API_KEY", "\"${localProps["GEMINI_API_KEY"]}\"")

    // ✅ ADD THIS (token broker URL):
    buildConfigField("String", "TOKEN_BROKER_URL",
        "\"https://rapture-tokenbroker.onrender.com\"")
}
```

## Step 8: Test Integration

### 8.1 Test with Debug Endpoint

Add a debug button in SettingsFragment:

```kotlin
// In SettingsFragment debug section
binding.btnTestTokenBroker.setOnClickListener {
    lifecycleScope.launch {
        try {
            val whoami = tokenBrokerClient.whoami()
            if (whoami != null) {
                val email = whoami.optString("email", "unknown")
                val sub = whoami.optString("sub", "unknown")
                Toast.makeText(requireContext(),
                    "✅ Token broker working!\nUser: $email\nSub: $sub",
                    Toast.LENGTH_LONG).show()
            } else {
                Toast.makeText(requireContext(),
                    "❌ Token broker failed - check logs",
                    Toast.LENGTH_SHORT).show()
            }
        } catch (e: Exception) {
            Toast.makeText(requireContext(),
                "❌ Error: ${e.message}",
                Toast.LENGTH_SHORT).show()
            Log.e(TAG, "Token broker test failed", e)
        }
    }
}
```

### 8.2 Test Transcription Flow

1. Sign in with Google
2. Tap "Test Token Broker" button (should show your email)
3. Record a voice note
4. Verify transcription works (check logs for "Using cached GCP access token")
5. Format a note
6. Verify formatting works (check logs for token refresh if needed)

### 8.3 Monitor Logs

```bash
adb logcat -s TokenBrokerClient:* StreamingTranscriptionService:* GeminiApiClient:*
```

Expected logs:
```
TokenBrokerClient: Requesting new GCP access token from broker
TokenBrokerClient: Cached GCP access token (expires in 3600s)
StreamingTranscriptionService: Starting session with token broker credentials
TokenBrokerClient: Using cached GCP access token
GeminiApiClient: Scrubbing transcription with token broker
```

## Error Handling

### Rate Limiting (429)

```kotlin
try {
    val token = tokenBrokerClient.getAccessToken()
} catch (e: IOException) {
    if (e.message?.contains("Rate limited") == true) {
        // Show user-friendly message
        Toast.makeText(context,
            "Too many requests - please wait a moment",
            Toast.LENGTH_SHORT).show()

        // Optionally: Extract retry-after seconds and schedule retry
        val retryAfter = extractRetryAfter(e.message) // Parse from error message
        // Schedule retry with WorkManager or Handler
    }
}
```

### Token Broker Unavailable

```kotlin
try {
    val token = tokenBrokerClient.getAccessToken()
} catch (e: IOException) {
    // Fallback: Use cached token or show offline message
    Log.e(TAG, "Token broker unavailable", e)

    // Option 1: Show error to user
    Toast.makeText(context,
        "Cannot connect to token service - check internet connection",
        Toast.LENGTH_SHORT).show()

    // Option 2: Gracefully degrade (disable premium features temporarily)
    FeatureFlags.isPremiumTemporarilyDisabled = true
}
```

### Sign-In Required

```kotlin
try {
    val token = tokenBrokerClient.getAccessToken()
} catch (e: IllegalStateException) {
    if (e.message?.contains("not signed in") == true) {
        // Redirect to sign-in
        Toast.makeText(context,
            "Please sign in to use transcription",
            Toast.LENGTH_SHORT).show()

        // Navigate to onboarding
        findNavController().navigate(R.id.action_to_onboarding)
    }
}
```

## Security Considerations

### Do NOT Log Tokens

```kotlin
// ❌ BAD - Never log tokens
Log.d(TAG, "Access token: $accessToken")

// ✅ GOOD - Log success without token value
Log.d(TAG, "Got access token from broker")
```

### Cache Tokens Securely

The `TokenBrokerClient` uses SharedPreferences for caching. This is acceptable because:
- Tokens are short-lived (1 hour)
- Tokens are not as sensitive as API keys (they expire quickly)
- SharedPreferences is sandboxed to the app

For extra security, you could use EncryptedSharedPreferences:

```kotlin
private val prefs = EncryptedSharedPreferences.create(
    "token_broker_secure",
    MasterKeys.getOrCreate(MasterKeys.AES256_GCM_SPEC),
    context,
    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
)
```

## Performance Optimization

### Token Caching Strategy

The `TokenBrokerClient` automatically caches tokens with a 5-minute safety buffer:
- **First call:** Requests token from broker (~200-500ms network call)
- **Subsequent calls:** Returns from cache (<1ms)
- **Expiry:** Auto-refreshes 5 minutes before actual expiry

This means:
- ✅ Fast transcription start (no waiting for token on each recording)
- ✅ Minimal network calls (1 per hour instead of per recording)
- ✅ Graceful refresh (never use expired tokens)

## Troubleshooting

### "User not signed in" Error

**Cause:** TokenBrokerClient called before Google Sign-In completed

**Fix:**
```kotlin
// Check if signed in before calling premium features
if (!GoogleAuthManager.getInstance(context).isSignedIn()) {
    Toast.makeText(context, "Sign in required", Toast.LENGTH_SHORT).show()
    return
}
```

### "Invalid ID token" Error from Broker

**Cause:** Token broker's `OIDC_CLIENT_ID` doesn't match the ID token's audience

**Fix:**
- Verify the token broker's `OIDC_CLIENT_ID` env var matches your **Web Application Client ID**
- Do NOT use the Android OAuth client ID (different client ID type)
- Check the ID token at https://jwt.io/ - look at the `aud` field

### Token Caching Not Working

**Cause:** Cache cleared on every app restart or SharedPreferences failing

**Fix:**
```kotlin
// Check cache is working
val token1 = tokenBrokerClient.getAccessToken()
delay(100)
val token2 = tokenBrokerClient.getAccessToken()

if (token1 == token2) {
    Log.d(TAG, "✅ Cache working")
} else {
    Log.w(TAG, "❌ Cache not working - requesting new tokens every time")
}
```

---

**Next Steps:**
- ✅ Integrate token broker into Android app
- → Test with physical device and live token broker
- → Remove API keys from `local.properties` and BuildConfig
- → Monitor Render logs for rate limit hits
- → (Future) Add similar integration to iOS app
