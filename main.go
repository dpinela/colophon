package main

import (
	"fmt"
	"os"

	"github.com/dpinela/hkmod/internal/modlinks"
)

func main() {
	manifests, err := modlinks.Get()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
		return
	}
	for _, m := range manifests {
		fmt.Printf("%#v\n", m)
	}
}
