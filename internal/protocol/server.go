package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
	"github.com/takets/tcc-local-connector/internal/engine"
	"github.com/takets/tcc-local-connector/internal/logging"
)

const (
	Version             = 1
	DefaultMaxMessage   = 64 * 1024
	DefaultDrainTimeout = 5 * time.Second
)

var errMessageTooLarge = errors.New("message exceeds maximum size")

type Request struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	Version int       `json:"version"`
	ID      string    `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Event struct {
	Version int    `json:"version"`
	Event   string `json:"event"`
	Data    any    `json:"data,omitempty"`
}

type Capability struct {
	Name string `json:"name"`
}

type readyData struct {
	ProtocolVersion int          `json:"protocol_version"`
	Capabilities    []Capability `json:"capabilities"`
}

type Server struct {
	In               io.Reader
	Out              io.Writer
	Logger           *log.Logger
	StructuredLogger *logging.Logger
	MaxMessage       int
	DrainTimeout     time.Duration
	Engine           EngineAPI

	writeMu sync.Mutex
	taskMu  sync.Mutex
	tasks   map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func (s *Server) Emit(name string, data any) {
	_ = s.write(Event{Version: Version, Event: "event." + name, Data: data})
}

type EngineAPI interface {
	Snapshot() engine.Status
	Reload() engine.ReloadResult
	RunCycleNow(context.Context) (int64, error)
	Pause(time.Time) error
	Resume() error
	ReportActions(int64, []engine.ActionResult) (int, int)
	Paths() map[string]string
}

func NewServer(in io.Reader, out io.Writer, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{
		In:               in,
		Out:              out,
		Logger:           logger,
		StructuredLogger: logging.New(nil, constants.DefaultLogLevel),
		MaxMessage:       DefaultMaxMessage,
		DrainTimeout:     DefaultDrainTimeout,
		tasks:            make(map[string]context.CancelFunc),
	}
}

func (s *Server) Serve(ctx context.Context) error {
	if err := s.write(Event{
		Version: Version,
		Event:   "ready",
		Data: readyData{
			ProtocolVersion: Version,
			Capabilities: []Capability{
				{Name: "ready"},
				{Name: "status"}, {Name: "reload_config"}, {Name: "pause"}, {Name: "resume"}, {Name: "refresh_now"}, {Name: "config_paths"}, {Name: "report_actions"}, {Name: "event.plan"}, {Name: "event.state_changed"}, {Name: "event.notify"},
			},
		},
	}); err != nil {
		return err
	}

	type readResult struct {
		line []byte
		err  error
	}
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	results := make(chan readResult, 1)
	go func() {
		reader := bufio.NewReader(s.In)
		for {
			line, err := readLine(reader, s.maxMessage())
			select {
			case results <- readResult{line: line, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil && !errors.Is(err, errMessageTooLarge) {
				return
			}
		}
	}()

	for {
		var line []byte
		var err error
		select {
		case <-ctx.Done():
			if closer, ok := s.In.(io.Closer); ok {
				_ = closer.Close()
			}
			return s.shutdown()
		case result := <-results:
			line, err = result.line, result.err
		}

		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return s.shutdown()
			case errors.Is(err, errMessageTooLarge):
				if writeErr := s.writeError("", "message_too_large", err.Error()); writeErr != nil {
					return writeErr
				}
				continue
			default:
				return err
			}
		}

		if len(line) == 0 {
			continue
		}

		var request Request
		if err := json.Unmarshal(line, &request); err != nil {
			if writeErr := s.writeError("", "invalid_json", "request must be valid JSON"); writeErr != nil {
				return writeErr
			}
			continue
		}

		if err := s.dispatch(ctx, request); err != nil {
			return err
		}
	}
}

func (s *Server) dispatch(parent context.Context, request Request) error {
	select {
	case <-parent.Done():
		return s.shutdown()
	default:
	}

	if request.Version != Version {
		return s.writeError(request.ID, "unsupported_version", fmt.Sprintf("supported version is %d", Version))
	}
	if request.ID == "" {
		return s.writeError("", "invalid_request", "id is required")
	}
	if request.Method == "" {
		return s.writeError(request.ID, "invalid_request", "method is required")
	}
	ctx, cancel := context.WithCancel(parent)
	if !s.addTask(request.ID, cancel) {
		cancel()
		return s.writeError(request.ID, "duplicate_id", "request id is already active")
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer s.removeTask(request.ID)

		result, rpcErr := s.handle(ctx, request)
		response := Response{
			Version: Version,
			ID:      request.ID,
			Result:  result,
			Error:   rpcErr,
		}
		if err := s.write(response); err != nil {
			s.Logger.Printf("write response %q: %v", request.ID, err)
			s.StructuredLogger.Log(logging.Entry{Level: "error", Component: "protocol", Event: "write_response", ErrorCode: "write_error", Message: err.Error()})
		}
	}()

	return nil
}

func (s *Server) handle(ctx context.Context, request Request) (any, *RPCError) {
	s.StructuredLogger.Log(logging.Entry{Level: "info", Component: "protocol", Event: "rpc_request", Message: request.Method})
	switch request.Method {
	case "status":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		return statusResult(s.Engine.Snapshot()), nil
	case "reload_config":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		result := s.Engine.Reload()
		if !result.OK && len(result.Errors) == 1 {
			switch result.Errors[0].Code {
			case "config_not_found":
				return nil, &RPCError{Code: "config_not_found", Message: "configuration file not found"}
			case "insecure_permissions":
				return nil, &RPCError{Code: "insecure_permissions", Message: "configuration has insecure permissions"}
			case "io_error":
				return nil, &RPCError{Code: "io_error", Message: "cannot read configuration"}
			}
		}
		return reloadConfigResult(result), nil
	case "pause":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		var params pauseParams
		if json.Unmarshal(request.Params, &params) != nil || (params.DurationSeconds == 0 && params.Until == "") || (params.DurationSeconds != 0 && params.Until != "") {
			return nil, &RPCError{Code: "invalid_params", Message: "provide exactly one pause deadline"}
		}
		if params.DurationSeconds != 0 && (params.DurationSeconds < constants.PauseMinSeconds || params.DurationSeconds > constants.PauseMaxSeconds) {
			return nil, &RPCError{Code: "invalid_params", Message: "duration_seconds is outside the supported range"}
		}
		until := time.Now().Add(time.Duration(params.DurationSeconds) * time.Second)
		if params.Until != "" {
			var err error
			until, err = time.Parse(time.RFC3339, params.Until)
			if err != nil || !until.After(time.Now()) {
				return nil, &RPCError{Code: "invalid_params", Message: "until must be in the future"}
			}
		}
		if err := s.Engine.Pause(until); err != nil {
			return nil, &RPCError{Code: "internal_error", Message: "could not persist pause"}
		}
		return pauseResult{Until: until.Format(time.RFC3339)}, nil
	case "resume":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		if err := s.Engine.Resume(); err != nil {
			return nil, &RPCError{Code: "invalid_state", Message: "backend is not paused"}
		}
		return resumeResult{Resumed: true}, nil
	case "refresh_now":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		id, err := s.Engine.RunCycleNow(ctx)
		if err != nil {
			return nil, &RPCError{Code: err.Error(), Message: "cycle was not accepted"}
		}
		return refreshNowResult{Accepted: true, CycleID: id}, nil
	case "config_paths":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		return configPathsResult(s.Engine.Paths()), nil
	case "report_actions":
		if s.Engine == nil {
			return nil, &RPCError{Code: "unsupported", Message: "engine is not configured"}
		}
		var params reportActionsParams
		if json.Unmarshal(request.Params, &params) != nil {
			return nil, &RPCError{Code: "invalid_params", Message: "invalid action results"}
		}
		for _, result := range params.Results {
			if (result.Status == "accepted" || result.Status == "skipped") && result.Code != "" {
				return nil, &RPCError{Code: "invalid_params", Message: "invalid status/code pair"}
			}
			if result.Status != "accepted" && result.Status != "skipped" && result.Code == "" {
				return nil, &RPCError{Code: "invalid_params", Message: "invalid status/code pair"}
			}
		}
		accepted, ignored := s.Engine.ReportActions(params.CycleID, params.Results)
		return reportActionsResult{Accepted: accepted, Ignored: ignored}, nil
	default:
		return nil, &RPCError{Code: "method_not_found", Message: "unknown method"}
	}
}

func (s *Server) addTask(id string, cancel context.CancelFunc) bool {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	if _, exists := s.tasks[id]; exists {
		return false
	}
	s.tasks[id] = cancel
	return true
}

func (s *Server) removeTask(id string) {
	s.taskMu.Lock()
	delete(s.tasks, id)
	s.taskMu.Unlock()
}

func (s *Server) shutdown() error {
	s.taskMu.Lock()
	for _, cancel := range s.tasks {
		cancel()
	}
	s.taskMu.Unlock()

	drained := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(drained)
	}()

	timeout := s.DrainTimeout
	if timeout <= 0 {
		timeout = DefaultDrainTimeout
	}
	select {
	case <-drained:
		return nil
	case <-time.After(timeout):
		return errors.New("timed out waiting for active requests to stop")
	}
}

func (s *Server) writeError(id, code, message string) error {
	return s.write(Response{
		Version: Version,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	})
}

func (s *Server) write(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.Out.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *Server) maxMessage() int {
	if s.MaxMessage <= 0 {
		return DefaultMaxMessage
	}
	return s.MaxMessage
}

func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line bytes.Buffer
	tooLarge := false

	for {
		fragment, err := reader.ReadSlice('\n')
		if !tooLarge {
			if line.Len()+len(fragment) > limit+1 {
				tooLarge = true
			} else {
				_, _ = line.Write(fragment)
			}
		}

		switch {
		case err == nil:
			if tooLarge {
				return nil, errMessageTooLarge
			}
			payload := bytes.TrimSuffix(line.Bytes(), []byte{'\n'})
			payload = bytes.TrimSuffix(payload, []byte{'\r'})
			if len(payload) > limit {
				return nil, errMessageTooLarge
			}
			return append([]byte(nil), payload...), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if tooLarge {
				return nil, errMessageTooLarge
			}
			if line.Len() == 0 {
				return nil, io.EOF
			}
			if line.Len() > limit {
				return nil, errMessageTooLarge
			}
			return append([]byte(nil), line.Bytes()...), nil
		default:
			return nil, err
		}
	}
}
