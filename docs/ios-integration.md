# iOS Integration Guide

> **For:** Rapture for iOS (Future)
> **Repository:** [rapture-ios](https://github.com/NoiseMeldOrg/rapture-ios)
> **Status:** Planned (post-launch of Android app)

## Overview

This guide will show how to integrate the Token Broker into the Rapture iOS app to securely access Google Cloud Speech-to-Text and Gemini APIs.

**Note:** This is a placeholder document for future iOS development. The integration pattern will be similar to the Android implementation with Swift/iOS-specific adjustments.

## Architecture

**Token Broker Flow (Same as Android):**
```
┌─────────────────┐
│  Rapture iOS    │ 1. User signs in with Google
│  (Swift/UIKit)  │    (Google Sign-In SDK for iOS)
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

## Prerequisites

When implementing iOS integration, you will need:

1. ✅ **Google Sign-In SDK for iOS** - For user authentication
2. ✅ **iOS OAuth 2.0 Client ID** - Separate from Android client ID
3. ✅ **Token Broker deployed** - Same service used by Android app
4. ✅ **Xcode** - Latest stable version
5. ✅ **Swift 5.5+** - For async/await support

## Planned Implementation

### Step 1: Add Dependencies (CocoaPods/SPM)

**Option A: CocoaPods**
```ruby
# Podfile
platform :ios, '15.0'

target 'Rapture' do
  use_frameworks!

  # Google Sign-In
  pod 'GoogleSignIn'

  # Networking (if not using URLSession)
  pod 'Alamofire', '~> 5.0'
end
```

**Option B: Swift Package Manager**
```swift
// Package.swift dependencies
dependencies: [
    .package(url: "https://github.com/google/GoogleSignIn-iOS", from: "7.0.0"),
    .package(url: "https://github.com/Alamofire/Alamofire.git", from: "5.8.0")
]
```

### Step 2: Create TokenBrokerClient

```swift
// Services/TokenBrokerClient.swift
import Foundation
import GoogleSignIn

struct AccessTokenResponse: Codable {
    let accessToken: String
    let tokenType: String
    let expiresIn: Int

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case tokenType = "token_type"
        case expiresIn = "expires_in"
    }
}

class TokenBrokerClient {
    static let shared = TokenBrokerClient()

    private let tokenBrokerURL = "https://rapture-tokenbroker.onrender.com"
    private let session = URLSession.shared

    // Token cache
    private var cachedToken: String?
    private var tokenExpiry: Date?

    private init() {}

    /// Get a valid GCP access token (from cache or by requesting from broker)
    func getAccessToken() async throws -> String {
        // Check cache first
        if let cached = getCachedToken() {
            print("Using cached GCP access token")
            return cached
        }

        // Request new token from broker
        print("Requesting new GCP access token from broker")
        let response = try await requestAccessToken()

        // Cache the token
        cacheToken(response.accessToken, expiresIn: response.expiresIn)

        return response.accessToken
    }

    /// Request a fresh access token from the token broker
    private func requestAccessToken() async throws -> AccessTokenResponse {
        // Get Google ID token
        guard let idToken = try await getGoogleIDToken() else {
            throw NSError(domain: "TokenBroker", code: 401,
                         userInfo: [NSLocalizedDescriptionKey: "User not signed in"])
        }

        // Build request
        let url = URL(string: "\(tokenBrokerURL)/token")!
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(idToken)", forHTTPHeaderField: "Authorization")

        // Make request
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw NSError(domain: "TokenBroker", code: -1,
                         userInfo: [NSLocalizedDescriptionKey: "Invalid response"])
        }

        // Handle rate limiting
        if httpResponse.statusCode == 429 {
            let retryAfter = httpResponse.value(forHTTPHeaderField: "Retry-After") ?? "60"
            throw NSError(domain: "TokenBroker", code: 429,
                         userInfo: [NSLocalizedDescriptionKey: "Rate limited - retry after \(retryAfter) seconds"])
        }

        guard httpResponse.statusCode == 200 else {
            let errorMsg = String(data: data, encoding: .utf8) ?? "Unknown error"
            throw NSError(domain: "TokenBroker", code: httpResponse.statusCode,
                         userInfo: [NSLocalizedDescriptionKey: "Token broker failed: \(errorMsg)"])
        }

        // Parse response
        let decoder = JSONDecoder()
        return try decoder.decode(AccessTokenResponse.self, from: data)
    }

    /// Get Google ID token from Google Sign-In
    private func getGoogleIDToken() async throws -> String? {
        // Get the current user
        guard let user = GIDSignIn.sharedInstance.currentUser else {
            return nil
        }

        // Refresh token if needed
        if user.accessToken.expirationDate < Date() {
            try await user.refreshTokensIfNeeded()
        }

        return user.idToken?.tokenString
    }

    /// Get cached token if valid and not expired
    private func getCachedToken() -> String? {
        guard let token = cachedToken,
              let expiry = tokenExpiry,
              Date() < expiry.addingTimeInterval(-5 * 60) else { // 5-minute buffer
            return nil
        }
        return token
    }

    /// Cache the access token with expiry time
    private func cacheToken(_ token: String, expiresIn: Int) {
        self.cachedToken = token
        self.tokenExpiry = Date().addingTimeInterval(TimeInterval(expiresIn))
        print("Cached GCP access token (expires in \(expiresIn)s)")
    }

    /// Clear cached token (call on sign-out)
    func clearCache() {
        cachedToken = nil
        tokenExpiry = nil
        print("Cleared GCP access token cache")
    }

    /// Debug method - verify Google ID token with /whoami endpoint
    func whoami() async throws -> [String: Any]? {
        guard let idToken = try await getGoogleIDToken() else {
            return nil
        }

        let url = URL(string: "\(tokenBrokerURL)/whoami")!
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(idToken)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse,
              httpResponse.statusCode == 200 else {
            return nil
        }

        return try JSONSerialization.jsonObject(with: data) as? [String: Any]
    }
}
```

### Step 3: Update Google Sign-In Configuration

```swift
// AppDelegate.swift or SceneDelegate.swift
import GoogleSignIn

// In application(_:didFinishLaunchingWithOptions:)
func application(_ application: UIApplication,
                didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {

    // Configure Google Sign-In
    // Use your iOS OAuth 2.0 Client ID (NOT the web client ID used by token broker)
    GIDSignIn.sharedInstance.configuration = GIDConfiguration(
        clientID: "YOUR_IOS_OAUTH_CLIENT_ID.apps.googleusercontent.com"
    )

    return true
}

// Handle OAuth callback
func application(_ app: UIApplication,
                open url: URL,
                options: [UIApplication.OpenURLOptionsKey: Any] = [:]) -> Bool {
    return GIDSignIn.sharedInstance.handle(url)
}
```

### Step 4: Update Transcription Service

```swift
// Services/TranscriptionService.swift
import Speech // Apple's Speech framework, or
// import GoogleCloudSpeech // Google's iOS SDK

class TranscriptionService {
    private let tokenBrokerClient = TokenBrokerClient.shared

    func startTranscription() async throws {
        // Get access token from broker
        let accessToken = try await tokenBrokerClient.getAccessToken()

        // Use token with Google Cloud Speech-to-Text API
        // Configure your gRPC or REST client with:
        // Authorization: Bearer {accessToken}

        // Example with URLRequest (REST API):
        var request = URLRequest(url: speechAPIURL)
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        // ... rest of transcription logic
    }
}
```

### Step 5: Update Gemini/Formatting Service

```swift
// Services/FormattingService.swift
class FormattingService {
    private let tokenBrokerClient = TokenBrokerClient.shared

    func formatNote(_ text: String) async throws -> String {
        // Get access token from broker
        let accessToken = try await tokenBrokerClient.getAccessToken()

        // Call Gemini API with bearer token
        let url = URL(string: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent")!
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        // ... rest of formatting logic
    }
}
```

### Step 6: Handle Sign-Out

```swift
// ViewControllers/SettingsViewController.swift
class SettingsViewController: UIViewController {

    @IBAction func signOutTapped(_ sender: UIButton) {
        // Sign out from Google
        GIDSignIn.sharedInstance.signOut()

        // Clear token broker cache
        TokenBrokerClient.shared.clearCache()

        // Navigate to onboarding
        // ... navigation logic
    }
}
```

## Expected Differences from Android

### SwiftUI vs UIKit
- Android uses Fragments/Jetpack Compose
- iOS can use UIKit (UIViewController) or SwiftUI (Views)
- Token broker client pattern remains the same

### Async/Await
- Android uses Kotlin coroutines (`suspend fun`)
- iOS uses Swift async/await (`async func`)
- Both have similar patterns for asynchronous code

### Dependency Management
- Android uses Gradle
- iOS uses CocoaPods or Swift Package Manager
- Token broker URL is the same for both platforms

### Google Sign-In SDK
- Different SDKs but same OAuth 2.0 flow
- iOS SDK: `GoogleSignIn` pod
- Android SDK: `play-services-auth`
- Both provide Google ID tokens for token broker

## Configuration Requirements

### Info.plist Updates

```xml
<!-- Google Sign-In URL scheme -->
<key>CFBundleURLTypes</key>
<array>
    <dict>
        <key>CFBundleURLSchemes</key>
        <array>
            <string>com.googleusercontent.apps.YOUR_REVERSED_CLIENT_ID</string>
        </array>
    </dict>
</array>

<!-- Allow HTTP for localhost testing (remove for production) -->
<key>NSAppTransportSecurity</key>
<dict>
    <key>NSAllowsArbitraryLoads</key>
    <true/>
</dict>
```

### Entitlements

```xml
<!-- Keychain access for token storage -->
<key>keychain-access-groups</key>
<array>
    <string>$(AppIdentifierPrefix)com.noisemeld.rapture</string>
</array>
```

## Testing (Planned)

### Test Token Broker Connection

```swift
// Debug button action
@IBAction func testTokenBroker(_ sender: UIButton) {
    Task {
        do {
            let whoami = try await TokenBrokerClient.shared.whoami()
            if let email = whoami?["email"] as? String,
               let sub = whoami?["sub"] as? String {
                showAlert(title: "✅ Success",
                         message: "Token broker working!\nUser: \(email)\nSub: \(sub)")
            } else {
                showAlert(title: "❌ Failed", message: "Check logs for details")
            }
        } catch {
            showAlert(title: "❌ Error", message: error.localizedDescription)
        }
    }
}
```

### Monitor Console Logs

```bash
# iOS Simulator/Device logs
xcrun simctl spawn booted log stream --predicate 'subsystem contains "com.noisemeld.rapture"' --level debug
```

Expected logs:
```
TokenBrokerClient: Requesting new GCP access token from broker
TokenBrokerClient: Cached GCP access token (expires in 3600s)
TranscriptionService: Using token from broker
TokenBrokerClient: Using cached GCP access token
```

## Security Considerations (iOS-Specific)

### Keychain Storage (Optional Enhancement)
For extra security, store cached tokens in iOS Keychain instead of UserDefaults:

```swift
import Security

class KeychainHelper {
    static func save(token: String, forKey key: String) {
        let data = token.data(using: .utf8)!
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: key,
            kSecValueData as String: data
        ]
        SecItemDelete(query as CFDictionary)
        SecItemAdd(query as CFDictionary, nil)
    }

    static func load(forKey key: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true
        ]
        var result: AnyObject?
        SecItemCopyMatching(query as CFDictionary, &result)

        guard let data = result as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }
}
```

## Timeline

The iOS app integration will follow after the Android app launch:

1. **Phase 1:** Android app launch with token broker (Phase 9-11)
2. **Phase 2:** Monitor Android app performance and token broker stability
3. **Phase 3:** Begin iOS app development with proven token broker architecture
4. **Phase 4:** iOS app launch using same token broker service

**Advantage:** iOS development benefits from lessons learned during Android integration.

## Resources

### Documentation
- [Google Sign-In for iOS](https://developers.google.com/identity/sign-in/ios)
- [Google Cloud Speech-to-Text iOS SDK](https://cloud.google.com/speech-to-text/docs/reference/libraries#client-libraries-install-swift)
- [Gemini API Documentation](https://ai.google.dev/docs)

### Sample Code
- Android integration guide (completed): [android-integration.md](android-integration.md)
- Token broker repository: [rapture-tokenbroker](https://github.com/NoiseMeldOrg/rapture-tokenbroker)

---

**Status:** 📋 Specification ready for future iOS development
**Next Step:** Complete Android app launch, then begin iOS implementation
**Same Token Broker:** iOS will use the exact same Render.com service as Android
