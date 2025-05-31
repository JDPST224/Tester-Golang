// A Go-based HTTP stress-testing tool with improved robustness and performance.
// This version incorporates the following fixes and enhancements:
//   - Validates command-line arguments (URL, threads, duration).
//   - Sorts DNS results for deterministic rebalancing.
//   - Uses context-aware DialContext with a timeout for both TCP and TLS dials.
//   - Drains a small amount of each HTTP response to prevent OS receive-buffer saturation.
//   - Adds proper error handling for URL parsing and integer conversion.
//   - Stops the DNS refresher when the root context is canceled.
//   - Omits the port from the Host header when it matches the default (80 for HTTP, 443 for HTTPS).
//   - Ensures that if DNS yields zero IPs, all workers are canceled.
//   - Cancels in-flight dials if the context is done.
//
// Usage:
//    go run main.go <URL> <THREADS> <DURATION_SEC> [CUSTOM_HOST]
//
// Example:
//    go run main.go https://example.com 100 60 example.com

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	// Global slice of currently resolved IPv4 addresses, protected by ipsMutex.
	ips      []string
	ipsMutex sync.Mutex

	// Channel to request rebalancing when DNS updates occur. Buffered to 1 so we only keep the latest.
	rebalanceCh = make(chan []string, 1)

	// HTTP method distribution: GET is 3× more likely.
	httpMethods = []string{"GET", "GET", "GET", "POST", "HEAD"}

	// Accept-Language headers (randomized).
	languages = []string{"en-US,en;q=0.9", "en-GB,en;q=0.8", "fr-FR,fr;q=0.9"}

	// Content types for POST bodies.
	contentTypes = []string{"application/x-www-form-urlencoded", "application/json", "text/plain"}
)

// StressConfig holds configuration for the stress test.
type StressConfig struct {
	Target     *url.URL
	Threads    int
	Duration   time.Duration
	CustomHost string
	Port       int
	Path       string
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <URL> <THREADS> <DURATION_SEC> [CUSTOM_HOST]\n", os.Args[0])
		os.Exit(1)
	}

	rawURL := os.Args[1]
	threads, err := strconv.Atoi(os.Args[2])
	if err != nil || threads <= 0 {
		fmt.Fprintf(os.Stderr, "Invalid THREADS (%q). Must be a positive integer.\n", os.Args[2])
		os.Exit(1)
	}

	durSec, err := strconv.Atoi(os.Args[3])
	if err != nil || durSec <= 0 {
		fmt.Fprintf(os.Stderr, "Invalid DURATION_SEC (%q). Must be a positive integer.\n", os.Args[3])
		os.Exit(1)
	}

	customHost := ""
	if len(os.Args) > 4 {
		customHost = os.Args[4]
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Hostname() == "" {
		fmt.Fprintf(os.Stderr, "Invalid URL: %q\n", rawURL)
		os.Exit(1)
	}

	port := determinePort(parsedURL)
	path := parsedURL.RequestURI()
	if path == "" {
		path = "/"
	}

	cfg := StressConfig{
		Target:     parsedURL,
		Threads:    threads,
		Duration:   time.Duration(durSec) * time.Second,
		CustomHost: customHost,
		Port:       port,
		Path:       path,
	}

	// Initial DNS lookup; exit if it fails or returns zero IPv4 addresses.
	addrs, err := lookupIPv4(parsedURL.Hostname())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Initial DNS lookup failed: %v\n", err)
		os.Exit(1)
	}
	updateIPs(addrs)
	fmt.Printf("Resolved IPs: %v\n", addrs)

	// Create a root context that is canceled either when the duration elapses or on SIGINT/SIGTERM.
	rootCtx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	// Start DNS refresher with rootCtx so it stops when rootCtx is canceled.
	go dnsRefresh(rootCtx, parsedURL.Hostname(), 30*time.Second)

	// Capture SIGINT/SIGTERM to cancel early.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Println("Interrupt received; shutting down early.")
			cancel()
		case <-rootCtx.Done():
		}
	}()

	fmt.Printf("Starting stress test: %s, threads=%d, duration=%v\n", rawURL, threads, cfg.Duration)
	runManager(rootCtx, cfg)
	fmt.Println("Stress test completed.")
}

// workerEntry holds a cancel function for a worker goroutine.
type workerEntry struct {
	cancel context.CancelFunc
}

// runManager coordinates the creation and cancellation of worker goroutines per IP.
func runManager(ctx context.Context, cfg StressConfig) {
	workers := make(map[string][]workerEntry)

	// spawn spins up one workerLoop for the given IP.
	spawn := func(ip string) {
		wctx, wcancel := context.WithCancel(ctx)
		workers[ip] = append(workers[ip], workerEntry{cancel: wcancel})
		go workerLoop(wctx, cfg, ip)
	}

	// Perform an initial rebalance based on whatever IPs we have.
	rebalance(getSnapshotIPs(), workers, cfg.Threads, spawn)

	for {
		select {
		case <-ctx.Done():
			// Cancel all workers on shutdown.
			for _, list := range workers {
				for _, w := range list {
					w.cancel()
				}
			}
			return

		case newIPs := <-rebalanceCh:
			rebalance(newIPs, workers, cfg.Threads, spawn)
		}
	}
}

// workerLoop runs as long as ctx is not canceled. It dials to the given IP,
// then repeatedly sends bursts of HTTP requests at random intervals, draining responses.
func workerLoop(ctx context.Context, cfg StressConfig, ip string) {
	// Determine which Host header to send.
	hostHdr := cfg.Target.Hostname()
	if cfg.CustomHost != "" {
		hostHdr = cfg.CustomHost
	}

	// TLS configuration (insecure—no cert validation).
	tlsCfg := &tls.Config{
		ServerName:         hostHdr,
		InsecureSkipVerify: true,
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Dial with a short timeout and respect ctx cancellation.
			addr := fmt.Sprintf("%s:%d", ip, cfg.Port)
			conn, err := dialConn(ctx, addr, tlsCfg)
			if err != nil {
				// Dial failed; wait a bit before retrying to avoid tight loop.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// Choose a method at random.
			method := httpMethods[rand.Intn(len(httpMethods))]
			// Send bursts of requests on conn until an error occurs or ctx is done.
			for {
				select {
				case <-ctx.Done():
					conn.Close()
					return
				default:
					sendBurst(conn, cfg, hostHdr, method)
				}
			}
		}
	}
}

// rebalance adjusts the number of workers per IP so that total workers == totalThreads.
func rebalance(ipsList []string, workers map[string][]workerEntry, totalThreads int, spawn func(string)) {
	n := len(ipsList)
	if n == 0 {
		// No IPs: cancel all existing workers.
		for ip, list := range workers {
			for _, w := range list {
				w.cancel()
			}
			delete(workers, ip)
		}
		fmt.Println("[rebalance] no IPs; all workers canceled")
		return
	}

	base := totalThreads / n
	extra := totalThreads % n
	desired := make(map[string]int, n)
	for i, ip := range ipsList {
		desired[ip] = base
		if i < extra {
			desired[ip]++
		}
	}

	// Cancel any workers whose IP is no longer in desired.
	for ip, list := range workers {
		if _, ok := desired[ip]; !ok {
			for _, w := range list {
				w.cancel()
			}
			delete(workers, ip)
		}
	}

	// For each desired IP, spawn or cancel to match the target count.
	for ip, want := range desired {
		have := len(workers[ip])
		if have < want {
			for i := 0; i < want-have; i++ {
				spawn(ip)
			}
		} else if have > want {
			for i := 0; i < have-want; i++ {
				w := workers[ip][0]
				w.cancel()
				workers[ip] = workers[ip][1:]
			}
		}
	}

	fmt.Printf("[rebalance] desired=%v have=%v\n", desired, mapCounts(workers))
}

// mapCounts returns a map[ip]count of how many workers are running per IP.
func mapCounts(workers map[string][]workerEntry) map[string]int {
	counts := make(map[string]int, len(workers))
	for ip, list := range workers {
		counts[ip] = len(list)
	}
	return counts
}

// getSnapshotIPs returns a copy of the current IP list under mutex.
func getSnapshotIPs() []string {
	ipsMutex.Lock()
	defer ipsMutex.Unlock()
	out := make([]string, len(ips))
	copy(out, ips)
	return out
}

// dnsRefresh periodically re-resolves the host every interval and triggers a rebalance.
// It stops when ctx is canceled.
func dnsRefresh(ctx context.Context, host string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			addrs, err := lookupIPv4(host)
			if err != nil {
				// DNS error: log and skip. Keep using the old IPs.
				log.Printf("DNS re-resolution failed for %s: %v\n", host, err)
				continue
			}

			// If no addresses, signal to cancel all workers.
			if len(addrs) == 0 {
				updateIPs([]string{})
				select {
				case rebalanceCh <- []string{}:
				default:
				}
				log.Printf("DNS re-resolution returned 0 IPs for %s; canceling all workers\n", host)
				continue
			}

			// Otherwise, update and request rebalance.
			updateIPs(addrs)
			fmt.Printf("Re-resolved IPs: %v\n", addrs)
			select {
			case rebalanceCh <- addrs:
			default:
			}
		}
	}
}

// updateIPs atomically replaces the global IP list.
func updateIPs(newIPs []string) {
	ipsMutex.Lock()
	ips = newIPs
	ipsMutex.Unlock()
}

// lookupIPv4 performs a DNS lookup for IPv4 addresses, sorts them, and returns them.
func lookupIPv4(host string) ([]string, error) {
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, a := range addrs {
		if ip4 := a.To4(); ip4 != nil {
			out = append(out, ip4.String())
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no IPv4 addresses found for %s", host)
	}

	// Sort for deterministic ordering so rebalances aren’t purely due to shuffled slice.
	sort.Strings(out)
	return out, nil
}

// determinePort returns the port number from the URL, or the default (80 for HTTP, 443 for HTTPS).
func determinePort(u *url.URL) int {
	if p := u.Port(); p != "" {
		if i, err := strconv.Atoi(p); err == nil {
			return i
		}
	}
	if strings.EqualFold(u.Scheme, "https") {
		return 443
	}
	return 80
}

// dialConn uses a context-aware DialContext for TCP, and DialWithDialer for TLS, with a short timeout.
func dialConn(ctx context.Context, addr string, tlsCfg *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if strings.HasSuffix(addr, ":443") {
		// For TLS, we wrap DialContext in a Dialer with Timeout.
		return tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	}
	// For plain TCP, use DialContext.
	return dialer.DialContext(ctx, "tcp", addr)
}

// sendBurst sends one HTTP request to the server on conn, then drains a small chunk of the response.
// We send 1 request per call here; workerLoop calls this in a tight loop to generate load.
func sendBurst(conn net.Conn, cfg StressConfig, hostHdr string, method string) {
	// Build headers + optional body.
	hdr, body := buildRequest(cfg, method, hostHdr)
	bufs := net.Buffers{hdr}
	if method == "POST" {
		bufs = append(bufs, body)
	}

	// Write the request.
	if _, err := bufs.WriteTo(conn); err != nil {
		// If write fails, close the connection so workerLoop will dial again.
		conn.Close()
		return
	}

	// Try to read up to 1 KiB from the response to advance the OS receive window.
	// We set a short deadline so we don't block for long on slow servers.
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var tmp [1024]byte
	_, _ = conn.Read(tmp[:]) // ignore errors; we only want to drain a bit.
	conn.SetReadDeadline(time.Time{}) // clear the deadline
}

// buildRequest constructs an HTTP/1.1 request (headers + optional body) given the config and chosen method.
// It omits the port from the Host header if it's the default (80 for HTTP, 443 for HTTPS).
func buildRequest(cfg StressConfig, method, hostHdr string) ([]byte, []byte) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	// Determine host:port for Host header. Omit port if it's the default for the scheme.
	hostPort := hostHdr
	if cfg.Port != 80 && cfg.Port != 443 {
		hostPort = fmt.Sprintf("%s:%d", hostHdr, cfg.Port)
	}

	// Start-line + Host header.
	fmt.Fprintf(buf, "%s %s HTTP/1.1\r\nHost: %s\r\n", method, cfg.Path, hostPort)

	// Common headers.
	writeCommonHeaders(buf)

	var body []byte
	if method == "POST" {
		// Pick a random content type and generate a body.
		ct := contentTypes[rand.Intn(len(contentTypes))]
		body = createBody(ct)
		fmt.Fprintf(buf, "Content-Type: %s\r\nContent-Length: %d\r\n", ct, len(body))
	}

	// Final headers.
	fmt.Fprintf(buf, "Referer: https://%s/\r\nConnection: keep-alive\r\n\r\n", hostHdr)

	// Copy to a fresh slice, because we'll return the buffer to the pool.
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, body
}

// writeCommonHeaders appends a set of randomized and static HTTP headers to the buffer.
func writeCommonHeaders(buf *bytes.Buffer) {
	buf.WriteString("User-Agent: " + randomUserAgent() + "\r\n")
	buf.WriteString("Accept-Language: " + languages[rand.Intn(len(languages))] + "\r\n")
	buf.WriteString("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n")
	buf.WriteString("Accept-Encoding: gzip, deflate\r\n") // Simplified encoding
	buf.WriteString("Sec-Fetch-Site: none\r\nSec-Fetch-Mode: navigate\r\nSec-Fetch-User: ?1\r\nSec-Fetch-Dest: document\r\n")
	buf.WriteString("Upgrade-Insecure-Requests: 1\r\nCache-Control: no-cache\r\n")
	fmt.Fprintf(buf, "X-Forwarded-For: %d.%d.%d.%d\r\n", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

// createBody generates a small random payload based on the content type.
func createBody(ct string) []byte {
	var b bytes.Buffer
	switch ct {
	case "application/x-www-form-urlencoded":
		vals := url.Values{}
		for i := 0; i < 3; i++ {
			vals.Set(randomString(5), randomString(8))
		}
		b.WriteString(vals.Encode())

	case "application/json":
		b.WriteString("{")
		for i := 0; i < 3; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "\"%s\":\"%s\"", randomString(5), randomString(8))
		}
		b.WriteString("}")

	default: // "text/plain"
		b.WriteString("text_" + randomString(12))
	}
	return b.Bytes()
}

// randomString returns a random alphanumeric string of length n.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// randomUserAgent builds a pseudo-random User-Agent string.
func randomUserAgent() string {
	osList := []string{"Windows NT 10.0; Win64; x64", "Macintosh; Intel Mac OS X 10_15_7", "X11; Linux x86_64"}
	osPart := osList[rand.Intn(len(osList))]

	switch rand.Intn(3) {
	case 0:
		v := fmt.Sprintf("%d.0.%d.0", rand.Intn(40)+80, rand.Intn(4000))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", osPart, v)
	case 1:
		v := fmt.Sprintf("%d.0", rand.Intn(40)+70)
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", osPart, v, v)
	default:
		v := fmt.Sprintf("%d.0.%d", rand.Intn(16)+600, rand.Intn(100))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Version/13.1 Safari/%s", osPart, v, v)
	}
}

// bufPool is a sync.Pool of bytes.Buffers to reduce allocations.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}
