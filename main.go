package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/mix3/phantasma/apis"
	"github.com/mix3/phantasma/apps"
	"github.com/mix3/phantasma/options"
)

var opts options.Options

var parser = flags.NewParser(&opts, flags.Default)

func init() {
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type != flags.ErrHelp {
			parser.ParseArgs([]string{"--help"})
		}
		os.Exit(1)
	}
}

func main() {
	api, err := apis.New(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer api.Close()

	app, err := apps.New(api, opts)
	if err != nil {
		log.Fatal(err)
	}

	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	log.Println("[main] starting...")
	log.Println("[main] running on", addr, "...")

	log.Fatal(http.ListenAndServe(addr, app))
}
