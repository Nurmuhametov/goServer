package main

import "server"

func main() {
	var s = server.Init()
	s.Start()
}
