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
	Action  string `json:"action"`  // "start"
	URL     string `json:"url"`     // Target URL
	Threads int    `json:"threads"` // Number of threads
	Timer   int    `json:"timer"`   // Duration in seconds
}

type AgentStatus struct {
	Online  bool
	Status  string // "Ready", "Sending"
	LastPing time.Time
}

var agents = []string{
	"http://localhost:8081",
	// Add more agents here
}

var mu sync.Mutex
var agentStatuses = make(map[string]AgentStatus)

// Send command to an agent
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

// Ping agent to update its status
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
		Online:  true,
		Status:  status.Status,
		LastPing: time.Now(),
	}
}

// Periodically update agent statuses
func monitorAgents() {
	for {
		for _, agent := range agents {
			pingAgent(agent)
		}
		time.Sleep(5 * time.Second)
	}
}

// Render web interface
func renderInterface(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
	<title>Control Server</title>
</head>
<body>
	<h1>Control Server</h1>
	<form method="POST" action="/command">
		<label>Target URL:</label><br>
		<input type="text" name="url" required><br><br>
		<label>Threads:</label><br>
		<input type="number" name="threads" required><br><br>
		<label>Timer (seconds):</label><br>
		<input type="number" name="timer" required><br><br>
		<button type="submit">Start Test</button>
	</form>
	<h2>Agent Status</h2>
	<table border="1">
		<tr>
			<th>Agent</th>
			<th>Status</th>
			<th>Online</th>
			<th>Last Ping</th>
		</tr>
		{{range $url, $status := .}}
		<tr>
			<td>{{$url}}</td>
			<td>{{$status.Status}}</td>
			<td>{{if $status.Online}}Online{{else}}Offline{{end}}</td>
			<td>{{$status.LastPing}}</td>
		</tr>
		{{end}}
	</table>
</body>
</html>
`
	mu.Lock()
	defer mu.Unlock()

	tmplParsed, _ := template.New("interface").Parse(tmpl)
	tmplParsed.Execute(w, agentStatuses)
}

// Handle commands from the web interface
func handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	url := r.FormValue("url")
	threads := r.FormValue("threads")
	timer := r.FormValue("timer")

	threadsInt, _ := strconv.Atoi(threads)
	timerInt, _ := strconv.Atoi(timer)

	cmd := Command{
		Action:  "start",
		URL:     url,
		Threads: threadsInt,
		Timer:   timerInt,
	}

	// Send the command to all agents
	for _, agent := range agents {
		go func(agent string) {
			err := sendCommandToAgent(agent, cmd)
			if err != nil {
				fmt.Printf("Failed to send command to %s: %v\n", agent, err)
			} else {
				fmt.Printf("Command sent to %s successfully\n", agent)
			}
		}(agent)
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	go monitorAgents()
	http.HandleFunc("/", renderInterface)
	http.HandleFunc("/command", handleCommand)
	fmt.Println("Control server is running on port 8080")
	http.ListenAndServe(":8080", nil)
}
