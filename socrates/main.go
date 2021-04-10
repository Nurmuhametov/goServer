package main

import (
	"fmt"
	"net"
	"os"
	"socrates/thinker"
	"strconv"
)

func main() {
	println(os.Args[0])
	conn, err := net.Dial("tcp4", os.Args[1])
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer conn.Close()
	defer conn.Write([]byte("DISCONNECT {\"QUIT\":\"\"}\n"))
	gamesToPlay, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	var player = thinker.InitThinker(os.Args[3], conn)
	for i := 0; i < gamesToPlay; i++ {
		player.PlayGame()
	}
}
