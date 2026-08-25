package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/constants"
	"github.com/takets/tcc-local-connector/internal/engine"
	"github.com/takets/tcc-local-connector/internal/protocol"
	"github.com/takets/tcc-local-connector/internal/rules"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	options, err := parseOptions(args)
	if err != nil {
		return fmt.Errorf("%v\nusage: tcc-local-connector-backend <serve|validate|config-test|run-once|pause|resume|refresh-now|config-paths|status> [options]", err)
	}
	if options.command == "validate" || options.command == "config-test" {
		return testConfig(options.configPath, output)
	}
	if options.command == "run-once" {
		return runOnce(options.configPath, output, options.dryRun)
	}
	if options.command != "serve" {
		backend := engine.New(engine.Deps{ConfigPath: options.configPath})
		if result := backend.Reload(); !result.OK {
			return fmt.Errorf("configuration is invalid: %v", result.Errors)
		}
		switch options.command {
		case "pause":
			if err := backend.Pause(options.pauseUntil); err != nil {
				return err
			}
		case "resume":
			if err := backend.Resume(); err != nil {
				return err
			}
		case "refresh-now":
			if _, err := backend.RunCycleNow(context.Background()); err != nil {
				return err
			}
		case "config-paths":
			return json.NewEncoder(output).Encode(backend.Paths())
		case "status":
			return json.NewEncoder(output).Encode(backend.Snapshot())
		}
		return nil
	}
	return serve(options)
}

func serve(options options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := protocol.NewServer(
		os.Stdin,
		os.Stdout,
		log.New(os.Stderr, "tcc-local-connector-backend: ", log.LstdFlags),
	)
	logPath := engine.LogPath(options.configPath)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, constants.StateFileMode)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()
	if err := logFile.Chmod(constants.StateFileMode); err != nil {
		return fmt.Errorf("set log file permissions: %w", err)
	}
	backend := engine.New(engine.Deps{ConfigPath: options.configPath, LogWriter: io.MultiWriter(os.Stderr, logFile)})
	server.StructuredLogger = backend.Logger()
	backend.SetEventSink(server.Emit)
	server.Engine = backend
	go backend.Run(ctx)
	if err := server.Serve(ctx); err != nil {
		return err
	}
	return nil
}

type options struct {
	command    string
	configPath string
	pauseUntil time.Time
	dryRun     bool
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, errors.New("command is required")
	}
	command := args[0]
	valid := map[string]bool{"serve": true, "validate": true, "config-test": true, "run-once": true, "pause": true, "resume": true, "refresh-now": true, "config-paths": true, "status": true}
	if !valid[command] {
		return options{}, errors.New("unknown command")
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stdio := flags.Bool("stdio", false, "use standard input/output transport")
	configPath := flags.String("config", "", "configuration file path")
	duration := flags.Int("duration-seconds", 0, "pause duration in seconds")
	apply := flags.Bool("apply", false, "allow side effects for run-once")
	dryRun := flags.Bool("dry-run", false, "suppress side effects for run-once")
	if err := flags.Parse(args[1:]); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("unexpected positional argument")
	}
	if command == "serve" && !*stdio {
		return options{}, errors.New("--stdio is required")
	}
	if command == "pause" && (*duration < 1 || *duration > 86400) {
		return options{}, errors.New("--duration-seconds must be between 1 and 86400")
	}
	if command != "run-once" && (*apply || *dryRun) {
		return options{}, errors.New("--apply and --dry-run are only valid for run-once")
	}
	if *apply && *dryRun {
		return options{}, errors.New("--apply and --dry-run cannot be used together")
	}

	return options{command: command, configPath: *configPath, pauseUntil: time.Now().Add(time.Duration(*duration) * time.Second), dryRun: !*apply}, nil
}

func testConfig(path string, output io.Writer) error {
	if path == "" {
		var err error
		path, err = config.DefaultPath()
		if err != nil {
			return err
		}
	}
	_, validationErrors, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("configuration cannot be loaded: %w", err)
	}
	if len(validationErrors) > 0 {
		return fmt.Errorf("configuration is invalid: %v", validationErrors)
	}
	_, err = fmt.Fprintln(output, "configuration is valid")
	return err
}

func runOnce(path string, output io.Writer, dryRun bool) error {
	backend := engine.New(engine.Deps{ConfigPath: path})
	result := backend.LoadConfig()
	if !result.OK {
		return fmt.Errorf("configuration is invalid: %v", result.Errors)
	}
	if err := backend.SetDryRun(dryRun); err != nil {
		return err
	}
	var actions []rules.PlannedAction
	backend.SetEventSink(func(name string, data any) {
		if name == "plan" {
			if value, ok := data.(rules.Plan); ok {
				actions = append(actions, value.Actions...)
			}
		}
	})
	cycleID, err := backend.RunCycleNow(context.Background())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "{\"cycle_id\":%d,\"dry_run\":%t,\"state\":%q,\"actions\":%s}\n", cycleID, dryRun, backend.Snapshot().State, mustJSON(actions))
	return err
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("[]")
	}
	return encoded
}
