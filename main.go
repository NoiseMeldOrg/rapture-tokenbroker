package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2/google"
	"golang.org/x/time/rate"
)

type tokenResp struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type whoamiResp struct {
	Subject string `json:"sub"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
	HD      string `json:"hd,omitempty"`
	Issuer  string `json:"iss"`
	Aud     string `json:"aud"`
	Exp     int64  `json:"exp"`
}

// ------- env helpers -------
func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("missing required env var %s", key)
	}
	return v
}
func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

// ------- simple limiter registry -------
type limiterEntry struct {
	lim  *rate.Limiter
	last time.Time
}
type limiterRegistry struct {
	mu    sync.Mutex
	data  map[string]*limiterEntry
	rps   rate.Limit
	burst int
	ttl   time.Duration
}

func newLimiterRegistry(perMin, burst, cleanupMins int) *limiterRegistry {
	rps := rate.Limit(float64(perMin) / 60.0)
	return &limiterRegistry{
		data:  make(map[string]*limiterEntry),
		rps:   rps,
		burst: burst,
		ttl:   time.Duration(cleanupMins) * time.Minute,
	}
}

func (lr *limiterRegistry) allow(key string) (bool, time.Duration) {
	now := time.Now()
	lr.mu.Lock()
	defer lr.mu.Unlock()

	entry, ok := lr.data[key]
	if !ok {
		entry = &limiterEntry{
			lim:  rate.NewLimiter(lr.rps, lr.burst),
			last: now,
		}
		lr.data[key] = entry
	}
	entry.last = now
	ok = entry.lim.Allow()
	if ok {
		return true, 0
	}
	// compute retry-after ~ next allowed reservation
	res := entry.lim.ReserveN(now, 1)
	if !res.OK() {
		return false, 5 * time.Second
	}
	delay := res.DelayFrom(now)
	// We consumed a token reservation; cancel to avoid skew
	res.CancelAt(now)
	return false, delay
}

func (lr *limiterRegistry) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(lr.ttl / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cut := time.Now().Add(-lr.ttl)
			lr.mu.Lock()
			for k, v := range lr.data {
				if v.last.Before(cut) {
					delete(lr.data, k)
				}
			}
			lr.mu.Unlock()
		}
	}
}

// ------- ip helper -------
func clientIP(r *http.Request) string {
	// Respect X-Forwarded-For from Render's proxy
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ------- auth helpers -------
func bearerFromAuthz(h string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return "", errors.New("no bearer")
	}
	return strings.TrimSpace(h[len("bearer "):]), nil
}

func enableCORS(w http.ResponseWriter, origin string) {
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Headers", "authorization, content-type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
}

// ------- main -------
func main() {
	// Required
	saJSON := []byte(mustEnv("GOOGLE_SA_JSON"))
	oidcClientID := mustEnv("OIDC_CLIENT_ID")

	// Optional
	scope := getEnv("TOKEN_SCOPE", "https://www.googleapis.com/auth/cloud-platform")
	corsOrigin := getEnv("CORS_ORIGIN", "*")
	allowedHD := strings.TrimSpace(os.Getenv("ALLOWED_HD"))

	// Rate config
	userPerMin := getEnvInt("RATE_PER_MIN", 60)
	userBurst := getEnvInt("RATE_BURST", 30)
	ipPerMin := getEnvInt("IP_RATE_PER_MIN", 120)
	ipBurst := getEnvInt("IP_BURST", 60)
	cleanupMins := getEnvInt("RATE_CLEANUP_MINS", 30)

	// Registries
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	userRL := newLimiterRegistry(userPerMin, userBurst, cleanupMins)
	ipRL := newLimiterRegistry(ipPerMin, ipBurst, cleanupMins)
	go userRL.cleanupLoop(ctx)
	go ipRL.cleanupLoop(ctx)

	// SA token source
	jwtConf, err := google.JWTConfigFromJSON(saJSON, scope)
	if err != nil {
		log.Fatalf("JWTConfigFromJSON: %v", err)
	}

	// OIDC verifier
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		log.Fatalf("oidc.NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: oidcClientID})

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// whoami (ID token → claims)
	mux.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w, corsOrigin)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// pre-verify IP limiter
		ip := clientIP(r)
		if ok, retry := ipRL.allow("ip:" + ip); !ok {
			w.Header().Set("Retry-After", seconds(retry))
			http.Error(w, "rate limit (ip)", http.StatusTooManyRequests)
			return
		}

		raw, err := bearerFromAuthz(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		idTok, err := verifier.Verify(r.Context(), raw)
		if err != nil {
			http.Error(w, "invalid id token", http.StatusUnauthorized)
			return
		}

		// per-user limiter (after we know who they are)
		var claims whoamiResp
		_ = idTok.Claims(&claims)
		if claims.Subject == "" {
			http.Error(w, "no subject", http.StatusUnauthorized)
			return
		}
		if ok, retry := userRL.allow("user:" + claims.Subject); !ok {
			w.Header().Set("Retry-After", seconds(retry))
			http.Error(w, "rate limit (user)", http.StatusTooManyRequests)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(claims)
	})

	// token (ID token → short-lived GCP access token)
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w, corsOrigin)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// pre-verify IP limiter
		ip := clientIP(r)
		if ok, retry := ipRL.allow("ip:" + ip); !ok {
			w.Header().Set("Retry-After", seconds(retry))
			http.Error(w, "rate limit (ip)", http.StatusTooManyRequests)
			return
		}

		raw, err := bearerFromAuthz(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		idTok, err := verifier.Verify(r.Context(), raw)
		if err != nil {
			http.Error(w, "invalid id token", http.StatusUnauthorized)
			return
		}

		// domain gate (optional)
		if allowedHD != "" {
			var c struct{ HD string `json:"hd"` }
			_ = idTok.Claims(&c)
			if strings.ToLower(strings.TrimSpace(c.HD)) != strings.ToLower(allowedHD) {
				http.Error(w, "forbidden: wrong domain", http.StatusForbidden)
				return
			}
		}

		// per-user limiter after identity known
		var sub struct{ Sub string `json:"sub"` }
		_ = idTok.Claims(&sub)
		if sub.Sub == "" {
			http.Error(w, "no subject", http.StatusUnauthorized)
			return
		}
		if ok, retry := userRL.allow("user:" + sub.Sub); !ok {
			w.Header().Set("Retry-After", seconds(retry))
			http.Error(w, "rate limit (user)", http.StatusTooManyRequests)
			return
		}

		// mint short-lived GCP token
		accessTok, err := jwtConf.TokenSource(r.Context()).Token()
		if err != nil {
			http.Error(w, "token mint failed", http.StatusInternalServerError)
			return
		}
		ttl := 3600
		if !accessTok.Expiry.IsZero() {
			if d := time.Until(accessTok.Expiry); d > 0 {
				ttl = int(d.Seconds())
			} else {
				ttl = 0
			}
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResp{
			AccessToken: accessTok.AccessToken,
			TokenType:   accessTok.TokenType,
			ExpiresIn:   ttl,
		})
	})

	// Wrap with CORS for any future routes
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w, corsOrigin)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})

	addr := ":" + getEnv("PORT", "10000")
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func seconds(d time.Duration) string {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return strconv.Itoa(s)
}
