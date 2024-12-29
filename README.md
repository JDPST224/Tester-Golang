## Start Agent
```bash
ulimit -n 999999
go build l7.go
go run agent.go
```
## Start Control Server
```bash
go run control_server.go
```
## Proxy Generate Certs
```bash
openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes
```
