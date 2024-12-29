package main

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"os"
)

func handleHTTPS(w http.ResponseWriter, r *http.Request) {
	// Establish a connection to the target server.
	conn, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, "Unable to connect to target server", http.StatusBadGateway)
		return
	}
	defer conn.Close()

	// Hijack the connection to directly forward traffic between client and target.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Unable to hijack connection", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Notify the client that the connection was successfully established.
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Forward traffic between client and target server.
	go io.Copy(conn, clientConn)
	io.Copy(clientConn, conn)
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Create a new HTTP request to the target server.
	req, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Copy the headers from the incoming request to the new request.
	req.Header = r.Header

	// Send the request to the target server.
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, "Unable to reach target server", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy the response headers and status code to the client.
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Copy the response body to the client.
	io.Copy(w, resp.Body)
}

func main() {
	// Configure HTTPS settings.
	httpsServer := &http.Server{
		Addr: ":8443",
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{loadCertificate()},
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				handleHTTPS(w, r)
			} else {
				handleHTTP(w, r)
			}
		}),
	}

	// Start the HTTPS server.
	log.Println("Starting proxy server on :8443")
	log.Fatal(httpsServer.ListenAndServeTLS("", ""))
}

// loadCertificate loads a self-signed certificate for HTTPS connections.
func loadCertificate() tls.Certificate {
	certPEM, err := os.ReadFile("proxy.crt")
	if err != nil {
		log.Fatalf("Failed to read certificate: %v", err)
	}

	keyPEM, err := os.ReadFile("proxy.key")
	if err != nil {
		log.Fatalf("Failed to read private key: %v", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		log.Fatalf("Failed to load certificate: %v", err)
	}
	return cert
}
