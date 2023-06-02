package main

import (
	"context"

	"distributed-backup/internal"
	"distributed-backup/pkg/log"
)

func main() {
	log.SetupLogger()

	app := internal.NewApp()

	if err := app.Setup(); err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if err := app.Run(ctx, cancel); err != nil {
		log.Fatal(err)
	}
}
