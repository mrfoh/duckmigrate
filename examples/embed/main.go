package main

import (
	"context"
	"embed"
	"fmt"
	"log"

	"github.com/mrfoh/duckmigrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	m, err := duckmigrate.New(
		duckmigrate.WithDSN(":memory:"),
		duckmigrate.WithFS(migrations, "migrations"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()

	if err := m.Up(context.Background()); err != nil {
		log.Fatal(err)
	}

	var name string
	if err := m.DB().QueryRow(`SELECT name FROM widgets WHERE id = 1`).Scan(&name); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("migrations applied; widget #1 is %q\n", name)
}
