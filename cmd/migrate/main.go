package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/pedroegerland/wager-service/internal/adapter/postgres"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	m, err := postgres.NewMigrator(url)
	if err != nil {
		fail(err)
	}
	defer m.Close()

	switch os.Args[1] {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	case "steps":
		if len(os.Args) < 3 {
			usage()
		}
		n, perr := strconv.Atoi(os.Args[2])
		if perr != nil {
			usage()
		}
		err = m.Steps(n)
	case "version":
		v, dirty, verr := m.Version()
		if verr != nil {
			fail(verr)
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)
		return
	default:
		usage()
	}
	if err != nil {
		fail(err)
	}
	fmt.Println("ok")
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate up|down|steps N|version")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
