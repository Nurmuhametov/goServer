package main

import "goServer/server"

func main() {
	var s = server.Init()
	println("Everything is OK")
	s.Start()
}
