package tcc2

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const (
	mcpProtocolVersion = "2025-06-18"
	maxResponseBytes   = 1024 * 1024
)

func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var output bytes.Buffer
	tooLarge := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !tooLarge {
			if output.Len()+len(fragment) > limit+1 {
				tooLarge = true
			} else {
				_, _ = output.Write(fragment)
			}
		}
		switch {
		case err == nil:
			if tooLarge {
				return nil, errors.New("MCP response exceeds maximum size")
			}
			return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if tooLarge {
				return nil, errors.New("MCP response exceeds maximum size")
			}
			if output.Len() == 0 {
				return nil, io.EOF
			}
			return output.Bytes(), nil
		default:
			return nil, err
		}
	}
}
