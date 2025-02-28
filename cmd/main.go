package main

import (
	serv "configremotemeter/cmd/app"
)

func main() {
	app := &serv.Application{}
	app.Init()
	app.Run()
}

