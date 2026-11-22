package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TechnoTUT/moipctl/internal/api"
	"github.com/TechnoTUT/moipctl/internal/discovery"
	"github.com/TechnoTUT/moipctl/internal/registry"
	"github.com/TechnoTUT/moipctl/internal/stream"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	host := fs.String("host", "0.0.0.0", "listen address")
	port := fs.Int("port", 8080, "listen port")
	mdns := fs.Bool("mdns", true, "discover nodes over mDNS (_moip._tcp)")
	var static []string
	fs.Func("node", "add a node by host:port (repeatable); never expires", func(v string) error {
		if _, _, err := net.SplitHostPort(v); err != nil {
			return fmt.Errorf("want host:port: %w", err)
		}
		static = append(static, v)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return err
	}

	reg := registry.New()
	for _, addr := range static {
		reg.Seen("static:"+addr, addr, addr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	run := func(f func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); f(ctx) }()
	}
	run(stream.New(reg).Run)
	if *mdns {
		run(discovery.New(reg, discovery.ZeroconfBrowser{}, discovery.Options{}).Run)
	}

	srv := &http.Server{
		Addr:              net.JoinHostPort(*host, strconv.Itoa(*port)),
		Handler:           (&api.Server{Reg: reg}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("moipctl %s listening on %s (mdns=%v, static nodes=%s)", version, srv.Addr, *mdns, strings.Join(static, ","))
	err := srv.ListenAndServe()
	stop()
	wg.Wait()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
