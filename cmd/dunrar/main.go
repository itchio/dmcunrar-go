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
	name := os.Args[1]

	archive, err := dmcunrar.OpenArchiveFromPath(name)
	must(err)

	log.Printf("File count: %d", archive.GetFileCount())

	for i := int64(0); i < archive.GetFileCount(); i++ {
		name, _ := archive.GetFilename(i)
		log.Printf("%d: %s", i, name)
	}
}

func must(err error) {
	if err != nil {
		panic(fmt.Sprintf("%+v", err))
	}
}
