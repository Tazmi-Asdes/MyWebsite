package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("dev")
		return
	}

	fmt.Fprintln(os.Stderr, "用法: adminctl version")
	os.Exit(2)
}
