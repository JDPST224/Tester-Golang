ulimit -n 999999
go build l7.go
go run agent.go

go run control_server.go


