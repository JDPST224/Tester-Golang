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
openssl genrsa -out proxy.key 2048
openssl req -new -x509 -key proxy.key -out proxy.crt -days 365
```
