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

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

var (
	ip       string
	port     int
	path     string
	threads  int
	timer    int
	cookie   string
	basePath string

	// More diverse platform choices
	platformChoices = []string{"Macintosh", "Windows", "X11", "Linux", "iPhone", "Android"}

	// Expanded OS options
	macOSVersions    = []string{"68K", "PPC", "Intel Mac OS X 10_15_7", "Intel Mac OS X 11_2_3", "Intel Mac OS X 12_4"}
	windowsVersions  = []string{"Win3.11", "WinNT4.0", "Windows NT 5.1", "Windows NT 6.1", "Windows 7", "Windows 8", "Windows NT 10.0; Win64; x64"}
	linuxVersions    = []string{"Linux i686", "Linux x86_64", "Ubuntu; Linux x86_64", "Debian; Linux x86_64"}
	androidVersions  = []string{"Android 9; Pixel 3", "Android 10; Pixel 4 XL", "Android 11; Pixel 5"}
	iphoneVersions   = []string{"iPhone; CPU iPhone OS 14_4 like Mac OS X", "iPhone; CPU iPhone OS 15_1 like Mac OS X"}

	// Expanded browser choices
	browsers = []string{"chrome", "firefox", "edge", "safari", "opera", "brave"}

	// Added variation for Accept headers
	acceptHeaders = []string{
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"text/html,application/json;q=0.9,*/*;q=0.8",
		"application/xml;q=0.9,text/html;q=0.8",
		"*/*;q=0.8",
	}

	// Language headers for more diversity
	acceptLanguages = []string{
		"en-US,en;q=0.5",
		"en-GB,en;q=0.8",
		"fr-FR,fr;q=0.7,en;q=0.3",
		"de-DE,de;q=0.9,en;q=0.5",
		"es-ES,es;q=0.9,en;q=0.5",
	}
)

func init() {
	rand.Seed(time.Now().UnixNano())
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
	switch browser {
	case "chrome":
		webkit := strconv.Itoa(rand.Intn(599-500) + 500)
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s.0 (KHTML, like Gecko) Chrome/%s Safari/%s", os, webkit, version, webkit)
	case "firefox":
		version := fmt.Sprintf("%d.0", rand.Intn(99))
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", os, version, version)
	case "edge":
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edge/%s", os, version, version)
	case "safari":
		version := fmt.Sprintf("%d.0", rand.Intn(99))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Version/%s Safari/537.36", os, version)
	case "opera":
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 OPR/%s", os, version, version)
	case "brave":
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Brave/%s", os, version, version)
	}
	return ""
}

func getHeader() string {
	header := fmt.Sprintf("GET %s HTTP/1.1\r\n", path)
	header += fmt.Sprintf("Host: %s\r\n", ip)
	header += fmt.Sprintf("User-Agent: %s\r\n", getUserAgent())
	header += fmt.Sprintf("Accept: %s\r\n", acceptHeaders[rand.Intn(len(acceptHeaders))])
	header += "Accept-Encoding: gzip, deflate\r\n" // Can also randomize this for better variety
	header += fmt.Sprintf("Accept-Language: %s\r\n", acceptLanguages[rand.Intn(len(acceptLanguages))])
	if cookie != "" {
		header += fmt.Sprintf("Cookie: %s\r\n", cookie)
	}
	header += "Connection: keep-alive\r\n\r\n"
	return header
}

func worker(id int, wg *sync.WaitGroup, requestCount chan int) {
	defer wg.Done()

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	address := fmt.Sprintf("%s:%d", ip, port)

	for count := range requestCount {
		var conn net.Conn
		var err error

		if port == 443 {
			conn, err = tls.Dial("tcp", address, tlsConfig)
		} else {
			conn, err = net.Dial("tcp", address)
		}

		if err != nil {
			fmt.Printf("Worker %d: connection error: %v\n", id, err)
			continue
		}

		header := getHeader()
		for i := 0; i < count; i++ {
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
			_, err := conn.Write([]byte(header))
			if err != nil {
				fmt.Printf("Worker %d: write error: %v\n", id, err)
				break
			}
		}
		conn.Close()
	}
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: <URL> <THREADS> <TIMER>")
		return
	}

	// Parse URL and determine port and path
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

	fmt.Printf("Starting stress test on %s:%d%s with %d threads for %d seconds\n", ip, port, path, threads, timer)

	var wg sync.WaitGroup
	requestCount := make(chan int, threads)

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go worker(i, &wg, requestCount)
	}

	// Dispatch requests to workers
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
