package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Command struct {
	Action     string `json:"action"`      // "start"
	URL        string `json:"url"`         // Target URL
	Threads    int    `json:"threads"`     // Number of threads
	Timer      int    `json:"timer"`       // Duration in seconds
	CustomHost string `json:"custom_host"` // Optional custom Host header
}

type AgentStatus struct {
	Online   bool
	Status   string // "Ready", "Sending"
	LastPing time.Time
}

var agents = []string{
	"http://localhost:8081",
}

var mu sync.Mutex
var agentStatuses = make(map[string]AgentStatus)

func sendCommandToAgent(agentURL string, cmd Command) error {
	body, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	resp, err := http.Post(agentURL+"/control", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent responded with status: %d", resp.StatusCode)
	}

	return nil
}

func pingAgent(agentURL string) {
	resp, err := http.Get(agentURL + "/status")
	mu.Lock()
	defer mu.Unlock()
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()

	if err != nil || resp.StatusCode != http.StatusOK {
		agentStatuses[agentURL] = AgentStatus{Online: false}
		return
	}

	var status struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		agentStatuses[agentURL] = AgentStatus{Online: false}
		return
	}

	agentStatuses[agentURL] = AgentStatus{
		Online:   true,
		Status:   status.Status,
		LastPing: time.Now(),
	}
}

func monitorAgents() {
	for {
		for _, agent := range agents {
			pingAgent(agent)
		}
		time.Sleep(5 * time.Second)
	}
}

func addAgentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	// Retrieve the agent URL from form input
	newAgent := r.FormValue("url")
	if newAgent == "" {
		http.Error(w, "Agent URL is required", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Append the new agent URL to the list if it does not already exist
	for _, agent := range agents {
		if agent == newAgent {
			http.Error(w, "Agent already exists", http.StatusConflict)
			return
		}
	}

	agents = append(agents, newAgent)
	fmt.Printf("Added new agent: %s\n", newAgent)

	// Redirect back to the main page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func serveAgentStatuses(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agentStatuses)
}

func renderInterface(w http.ResponseWriter, r *http.Request) {
	// Path to the HTML file
	const filePath = "Interface/index.html"

	// Parse the HTML file
	tmpl, err := template.ParseFiles(filePath)
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		fmt.Printf("Template error: %v\n", err)
		return
	}

	// Lock to prevent concurrent issues
	mu.Lock()
	defer mu.Unlock()

	// Render the template
	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		fmt.Printf("Execution error: %v\n", err)
	}
}

func handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	url := r.FormValue("url")
	threads, _ := strconv.Atoi(r.FormValue("threads"))
	timer, _ := strconv.Atoi(r.FormValue("timer"))
	customHost := r.FormValue("custom_host")

	cmd := Command{
		Action:     "start",
		URL:        url,
		Threads:    threads,
		Timer:      timer,
		CustomHost: customHost,
	}

	for _, agent := range agents {
		go func(agent string) {
			err := sendCommandToAgent(agent, cmd)
			if err != nil {
				fmt.Printf("Failed to send command to %s: %v\n", agent, err)
			}
		}(agent)
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	// Serve static files from the "Interface" folder
	fs := http.FileServer(http.Dir("Interface"))
	http.Handle("/Interface/", http.StripPrefix("/Interface/", fs))

	// Start monitoring agents
	go monitorAgents()

	// Define route handlers
	http.HandleFunc("/", renderInterface)
	http.HandleFunc("/add-agent", addAgentHandler)
	http.HandleFunc("/command", handleCommand)
	http.HandleFunc("/agent-statuses", serveAgentStatuses)

	// Start the server
	fmt.Println("Server running on http://localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
