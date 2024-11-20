package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Command struct {
	Action  string `json:"action"`  // "start" or "stop"
	URL     string `json:"url"`     // Target URL
	Threads int    `json:"threads"` // Number of threads
	Timer   int    `json:"timer"`   // Duration in seconds
}

var agents = []string{
	"http://agent1.example.com:8080",
	"http://agent2.example.com:8080",
	// Add more agents here
}

var mu sync.Mutex
var activeAgents = make(map[string]bool)

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

func handleCommand(w http.ResponseWriter, r *http.Request) {
	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "Invalid command format", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	// Send the command to all agents
	for _, agent := range agents {
		go func(agent string) {
			err := sendCommandToAgent(agent, cmd)
			if err != nil {
				fmt.Printf("Failed to send command to %s: %v\n", agent, err)
			} else {
				fmt.Printf("Command sent to %s successfully\n", agent)
				if cmd.Action == "start" {
					activeAgents[agent] = true
				} else if cmd.Action == "stop" {
					delete(activeAgents, agent)
				}
			}
		}(agent)
	}

	w.Write([]byte("Command dispatched to all agents"))
}

func main() {
	http.HandleFunc("/command", handleCommand)
	fmt.Println("Control server is running on port 8080")
	http.ListenAndServe(":8080", nil)
}
