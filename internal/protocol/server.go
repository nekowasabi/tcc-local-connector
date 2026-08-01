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

	"github.com/takets/tcc-local-connector/internal/tcc2"
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

type echoParams struct {
	Value json.RawMessage `json:"value"`
}

type sleepParams struct {
	Milliseconds int `json:"milliseconds"`
}

type Server struct {
	In           io.Reader
	Out          io.Writer
	Logger       *log.Logger
	MaxMessage   int
	DrainTimeout time.Duration
	ProbeTCC2    func(context.Context) (tcc2.ProbeResult, error)

	writeMu sync.Mutex
	taskMu  sync.Mutex
	tasks   map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func NewServer(in io.Reader, out io.Writer, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{
		In:           in,
		Out:          out,
		Logger:       logger,
		MaxMessage:   DefaultMaxMessage,
		DrainTimeout: DefaultDrainTimeout,
		ProbeTCC2: func(ctx context.Context) (tcc2.ProbeResult, error) {
			return tcc2.Probe(ctx, "tcc2")
		},
		tasks: make(map[string]context.CancelFunc),
	}
}

func (s *Server) Serve(ctx context.Context) error {
	if err := s.write(Event{
		Version: Version,
		Event:   "ready",
		Data: readyData{
			ProtocolVersion: Version,
			Capabilities: []Capability{
				{Name: "health"},
				{Name: "echo"},
				{Name: "sleep"},
				{Name: "cancel"},
				{Name: "tcc2_probe"},
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
	if request.Method == "cancel" {
		return s.cancel(request)
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
		}
	}()

	return nil
}

func (s *Server) handle(ctx context.Context, request Request) (any, *RPCError) {
	switch request.Method {
	case "health":
		return map[string]string{"status": "ok"}, nil
	case "echo":
		var params echoParams
		if len(request.Params) == 0 {
			return nil, &RPCError{Code: "invalid_params", Message: "params.value is required"}
		}
		if err := json.Unmarshal(request.Params, &params); err != nil || len(params.Value) == 0 {
			return nil, &RPCError{Code: "invalid_params", Message: "params.value is required"}
		}
		return map[string]json.RawMessage{"value": params.Value}, nil
	case "sleep":
		var params sleepParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, &RPCError{Code: "invalid_params", Message: "milliseconds must be an integer"}
		}
		if params.Milliseconds < 0 || params.Milliseconds > 30_000 {
			return nil, &RPCError{Code: "invalid_params", Message: "milliseconds must be between 0 and 30000"}
		}
		timer := time.NewTimer(time.Duration(params.Milliseconds) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, &RPCError{Code: "cancelled", Message: "request was cancelled"}
		case <-timer.C:
			return map[string]bool{"completed": true}, nil
		}
	case "tcc2_probe":
		if s.ProbeTCC2 == nil {
			return nil, &RPCError{Code: "unsupported", Message: "tcc2 probe is not configured"}
		}
		result, err := s.ProbeTCC2(ctx)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				return nil, &RPCError{Code: "cancelled", Message: "request was cancelled"}
			case errors.Is(err, context.DeadlineExceeded):
				return nil, &RPCError{Code: "timeout", Message: "tcc2 probe timed out"}
			default:
				return nil, &RPCError{Code: "tcc2_error", Message: err.Error()}
			}
		}
		return result, nil
	default:
		return nil, &RPCError{Code: "method_not_found", Message: "unknown method"}
	}
}

func (s *Server) cancel(request Request) error {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params.ID == "" {
		return s.writeError(request.ID, "invalid_params", "params.id is required")
	}
	if params.ID == request.ID {
		return s.writeError(request.ID, "duplicate_id", "cancel request id must differ from target id")
	}

	s.taskMu.Lock()
	_, requestIDExists := s.tasks[request.ID]
	cancel, ok := s.tasks[params.ID]
	s.taskMu.Unlock()
	if requestIDExists {
		return s.writeError(request.ID, "duplicate_id", "request id is already active")
	}
	if !ok {
		return s.writeError(request.ID, "not_found", "active request was not found")
	}

	cancel()
	return s.write(Response{
		Version: Version,
		ID:      request.ID,
		Result:  map[string]string{"cancelled_id": params.ID},
	})
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
