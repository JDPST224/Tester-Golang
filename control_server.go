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
	"https://humble-muskrat-reliably.ngrok-free.app",
	"https://tidy-notably-vervet.ngrok-free.app",
	"https://moved-miserably-oryx.ngrok-free.app",
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

func serveAgentStatuses(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agentStatuses)
}

func renderInterface(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Control Server</title>
    <style>
        body, html {
            font-family: 'Arial', sans-serif;
            background-color: #1a1a1a;
            color: #f4f4f9;
            display: flex;
            justify-content: center;
            align-items: flex-start;
            margin: 0;
        }
        .main-container {
            width: 90%;
            max-width: 800px;
            margin-top: 20px;
        }
        .container {
            background-color: #262626;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.4);
        }
        input, button {
            padding: 10px;
            margin: 10px 0;
            border: 1px solid #555;
            border-radius: 5px;
            width: 90%;
            color: #f4f4f9;
            background-color: #1a1a1a;
        }
        button {
            background-color: #007bff;
            color: #fff;
            cursor: pointer;
        }
        table {
            width: 100%;
            margin-top: 10px;
        }
        th, td {
            padding: 8px;
            text-align: left;
            border: 1px solid #555;
        }
    </style>
    <script>
        async function fetchAgentStatuses() {
            const response = await fetch('/agent-statuses');
            const data = await response.json();
            const tbody = document.querySelector('tbody');
            tbody.innerHTML = '';
            for (const [agent, info] of Object.entries(data)) {
                const row = '<tr>' +
                            '<td>' + agent + '</td>' +
                            '<td>' + (info.Online ? 'Online' : 'Offline') + '</td>' +
                            '<td>' + info.Status + '</td>' +
                            '<td>' + new Date(info.LastPing).toLocaleString() + '</td>' +
                            '</tr>';
                tbody.innerHTML += row;
            }
        }
        setInterval(fetchAgentStatuses, 5000);
        fetchAgentStatuses();
    </script>
</head>
<body>
    <div class="main-container">
        <div class="container">
            <h1>Control Server</h1>
            <form method="POST" action="/command">
                <input type="text" name="url" placeholder="Target URL" required>
                <input type="number" name="threads" placeholder="Threads" required>
                <input type="number" name="timer" placeholder="Timer (seconds)" required>
                <input type="text" name="custom_host" placeholder="Custom Host (optional)">
                <button type="submit">Start</button>
            </form>
        </div>
        <div class="container">
            <h2>Agent Status</h2>
            <table>
                <thead>
                    <tr>
                        <th>Agent</th>
                        <th>Online</th>
                        <th>Status</th>
                        <th>Last Ping</th>
                    </tr>
                </thead>
                <tbody></tbody>
            </table>
        </div>
    </div>
</body>
</html>
`
	mu.Lock()
	defer mu.Unlock()

	tmplParsed, _ := template.New("interface").Parse(tmpl)
	tmplParsed.Execute(w, nil)
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
	go monitorAgents()
	http.HandleFunc("/", renderInterface)
	http.HandleFunc("/command", handleCommand)
	http.HandleFunc("/agent-statuses", serveAgentStatuses)

	fmt.Println("Server running on http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
