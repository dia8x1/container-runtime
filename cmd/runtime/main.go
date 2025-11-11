package main

import (
	"container-runtime/internal/cli"
	"container-runtime/internal/container"
	"os"
)

func main() {

	if len(os.Args) > 1 && os.Args[1] == "child" {
		container.ChildMain()
	} else {
		cli.Execute()
	}
}
