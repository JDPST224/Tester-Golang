package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Middleware for rate limiting
func rateLimiter(next http.Handler) http.Handler {
	sem := make(chan struct{}, 100) // Allow 100 concurrent requests
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
		}
	})
}

// IP filtering middleware
func ipFilter(allowedIPs []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := strings.Split(r.RemoteAddr, ":")[0]
		for _, ip := range allowedIPs {
			if clientIP == ip {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
}

func main() {
	// Target server to proxy requests to
	target := "https://example.com"
	targetURL, err := url.Parse(target)
	if err != nil {
		log.Fatalf("Failed to parse target URL: %v", err)
	}

	// Reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy error: %v", err)
		http.Error(w, "Proxy error", http.StatusBadGateway)
	}

	// Allowed IPs (example)
	allowedIPs := []string{"127.0.0.1", "192.168.1.1"}

	// Middleware stack
	mux := http.NewServeMux()
	mux.Handle("/", ipFilter(allowedIPs, rateLimiter(proxy)))

	// HTTPS server setup
	server := &http.Server{
		Addr:         ":443",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	log.Println("Starting proxy server on :443")
	err = server.ListenAndServeTLS("server.crt", "server.key") // Replace with your certificate and key files
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
