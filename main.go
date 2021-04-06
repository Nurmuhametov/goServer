package main

import "goServer/server"

func main() {
	var s = server.Init()
	s.Start()
}
