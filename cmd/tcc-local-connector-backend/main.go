package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/takets/tcc-local-connector/internal/engine"
	connectorlog "github.com/takets/tcc-local-connector/internal/logging"
	"github.com/takets/tcc-local-connector/internal/protocol"
	"github.com/takets/tcc-local-connector/internal/tcc2"
)

func main() {
	options, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\nusage: tcc-local-connector-backend serve --stdio [--tcc2-executable <path>]\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := protocol.NewServer(
		os.Stdin,
		os.Stdout,
		log.New(os.Stderr, "tcc-local-connector-backend: ", log.LstdFlags),
	)
	server.ProbeTCC2 = func(ctx context.Context) (tcc2.ProbeResult, error) {
		return tcc2.Probe(ctx, options.tcc2Executable)
	}
	structuredLogger := connectorlog.New(os.Stderr, "info")
	server.StructuredLogger = structuredLogger
	backend := engine.New(engine.Deps{ConfigPath: options.configPath, Logger: structuredLogger})
	backend.SetEventSink(server.Emit)
	server.Engine = backend
	go backend.Run(ctx)
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "tcc-local-connector-backend: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	tcc2Executable string
	configPath     string
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 || args[0] != "serve" {
		return options{}, errors.New("serve command is required")
	}

	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stdio := flags.Bool("stdio", false, "use standard input/output transport")
	tcc2Executable := flags.String("tcc2-executable", "tcc2", "absolute path or command name for tcc2")
	configPath := flags.String("config", "", "configuration file path")
	if err := flags.Parse(args[1:]); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("unexpected positional argument")
	}
	if !*stdio {
		return options{}, errors.New("--stdio is required")
	}
	if *tcc2Executable == "" {
		return options{}, errors.New("--tcc2-executable must not be empty")
	}

	return options{tcc2Executable: *tcc2Executable, configPath: *configPath}, nil
}
