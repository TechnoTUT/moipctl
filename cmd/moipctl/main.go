// Command moipctl manages several moip-toolkit nodes from one place.
//
//	moipctl serve [flags]    run the controller (REST API and web UI)
//	moipctl scout [flags]    search for nodes on this VLAN and report them to the controller
//	moipctl version          show the version
package main

import (
	"fmt"
	"log"
	"os"
)

// version is overridden at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `moipctl

Usage:
  moipctl serve [flags]    Start the controller
  moipctl scout [flags]    Search for nodes on this network and report them to a controller
  moipctl version          Show the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "scout":
		if err := runScout(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
