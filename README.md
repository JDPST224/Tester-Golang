go run control_server.go

go run agent.go


Commands

Start: 
curl -X POST -H "Content-Type: application/json" -d '{"action":"start", "url":"https://nodec.mediathektv.com", "threads":1800, "timer":60}' http://localhost:8080/command

Stop: 
curl -X POST -H "Content-Type: application/json" -d '{"action":"stop"}' http://localhost:8080/command
