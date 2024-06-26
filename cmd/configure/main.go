package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/initializ-buildpacks/nginx/cmd/configure/internal"
)

func main() {
	log.SetFlags(0)

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	err = internal.Run(
		os.Getenv("EXECD_CONF"),
		filepath.Join(wd, "modules"),
		strings.Replace(filepath.Dir(os.Args[0]), "exec.d", "modules", 1),
	)

	if err != nil {
		log.Fatal(err)
	}
}
