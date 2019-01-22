package main

import (
	"fmt"
	"log"
	"os"

	"github.com/itchio/dmcunrar-go/dmcunrar"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s FILE.rar", os.Args[0])
	}
	file := os.Args[1]

	err := dmcunrar.Demo(file)
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}
}
