package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TechnoTUT/moipctl/internal/discovery"
	"github.com/TechnoTUT/moipctl/internal/scout"
)

// runScout runs the scout: the only network it needs is the VLAN it searches
// (for mDNS) and a route to the controller (to report).
func runScout(args []string) error {
	fs := flag.NewFlagSet("scout", flag.ExitOnError)
	controller := fs.String("controller", os.Getenv("MOIPCTL_CONTROLLER"), "base URL of the controller's scout listener, e.g. http://moipctl:8081 ($MOIPCTL_CONTROLLER)")
	source := fs.String("source", os.Getenv("MOIPCTL_SOURCE"), "name of this scout, e.g. the VLAN: av or ent ($MOIPCTL_SOURCE)")
	interval := fs.Duration("interval", 15*time.Second, "time between searches")
	window := fs.Duration("window", 6*time.Second, "how long one search listens for answers; it sends three queries within it")
	health := fs.String("health", ":8082", "address for GET /healthz (empty: disabled)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *controller == "" || *source == "" {
		return errors.New("scout: --controller and --source are required")
	}
	if !strings.HasPrefix(*controller, "http://") && !strings.HasPrefix(*controller, "https://") {
		return fmt.Errorf("scout: --controller %q must start with http:// or https://", *controller)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *health != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
		srv := &http.Server{Addr: *health, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("health listener: %v", err)
			}
		}()
		go func() { <-ctx.Done(); srv.Close() }()
	}

	log.Printf("moipctl %s scout %q reporting to %s every %s", version, *source, *controller, *interval)
	(&scout.Agent{
		Browser:    discovery.ZeroconfBrowser{},
		Controller: strings.TrimRight(*controller, "/"),
		Source:     *source,
		Interval:   *interval,
		Window:     *window,
	}).Run(ctx)
	return nil
}
