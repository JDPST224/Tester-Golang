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
	Online   bool
	Status   string // "Ready", "Sending"
	LastPing time.Time
}

var agents = []string{
	"http://localhost:8081",
	"http://localhost:8082",
	"http://localhost:8083",
	"http://localhost:8084",
	"http://localhost:8086",
	"http://localhost:8087",
	"http://localhost:804",
	"http://localhost:8045",
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
		Online:   true,
		Status:   status.Status,
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
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Control Server</title>
    <style>
        /* Ensure body and html allow proper alignment */
        body, html {
            font-family: 'Arial', sans-serif;
            margin: 0;
            padding: 0;
            background-color: #1a1a1a;
            color: #f4f4f9;
            height: 100%;
            display: flex;
            justify-content: center; /* Center horizontally */
            align-items: flex-start; /* Align to the top initially */
        }

        /* Main container for all content */
        .main-container {
            width: 90%;
            max-width: 800px;
            margin-top: 20px; /* Space from the top for better visibility */
            display: flex;
            flex-direction: column;
            align-items: center;
        }

        /* Ensure each section has proper spacing */
        .container {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            width: 100%;
            background-color: #262626;
            padding: 20px;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.4);
            margin-bottom: 20px; /* Add margin for spacing */
        }

        /* Agent Status table styles */
        .agent-status {
            width: 100%;
            max-height: 300px; /* Limit height to allow scrolling */
            overflow-y: auto; /* Enable vertical scrolling for the table */
        }

        form{
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            width: 100%;
        }

        /* Form spacing tweaks */
        input[type="text"], input[type="number"], button {
            width: 90%; /* Make inputs and button full width */
            padding: 10px;
            margin: 10px 0; /* Adjust margin to avoid extra space */
            border: 1px solid #555;
            border-radius: 5px;
            background-color: #1a1a1a;
            color: #f4f4f9;
        }

        /* Button style */
        button {
            width: 80%; /* Ensure button is full width */
            padding: 10px;
            margin: 10px 0;
            border: 1px solid #555;
            border-radius: 5px;
            background-color: #007bff;
            color: #fff;
            font-weight: bold;
            cursor: pointer;
            transition: background-color 0.3s ease;
        }

        button:hover {
            background-color: #0056b3;
        }

        /* Heading Styles */
        h1, h2 {
            color: #f4f4f9;
        }

        /* Table styles */
        table {
            width: 100%;
            color: #f4f4f9;
            text-align: left;
            border-collapse: collapse;
        }

        table th, table td {
            padding: 8px;
            border: 1px solid #555;
        }

        table th {
            background-color: #333;
        }
    </style>
</head>
<body>
    <div class="main-container">
        <div class="container">
            <h1>Control Server</h1>
            <form method="POST" action="/command">
                <label for="url">Target URL:</label>
                <input type="text" name="url" id="url" placeholder="Enter the target URL" required>
                
                <label for="threads">Threads:</label>
                <input type="number" name="threads" id="threads" placeholder="Number of threads" required>
                
                <label for="timer">Timer (seconds):</label>
                <input type="number" name="timer" id="timer" placeholder="Duration in seconds" required>
                
                <button type="submit">Start Test</button>
            </form>
        </div>

        <div class="container">
            <h2>Agent Status</h2>
            <div class="agent-status">
                <table>
                    <thead>
                        <tr>
                            <th>Agent</th>
                            <th>Status</th>
                            <th>Online</th>
                            <th>Last Ping</th>
                        </tr>
                    </thead>
                    <tbody>
                        <!-- Data dynamically filled here by the backend -->
                        {{range $url, $status := .}}
                        <tr>
                            <td>{{$url}}</td>
                            <td>{{$status.Status}}</td>
                            <td>{{if $status.Online}}Online{{else}}Offline{{end}}</td>
                            <td>{{$status.LastPing}}</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
        </div>
    </div>
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
