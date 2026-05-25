package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// ServeStdio runs the MCP server over stdin/stdout.
func (s *Server) ServeStdio() error {
	return s.ServeStdioWithReaderWriter(os.Stdin, os.Stdout)
}

// ServeStdioWithReaderWriter runs the MCP server over arbitrary reader/writer.
// Uses newline-delimited JSON (one JSON-RPC message per line).
func (s *Server) ServeStdioWithReaderWriter(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := &Response{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &RPCError{Code: CodeParseError, Message: "parse error: " + err.Error()},
			}
			data, _ := json.Marshal(resp)
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n"))
			continue
		}

		resp := s.HandleRequest(&req)
		if resp == nil {
			continue
		}

		data, err := json.Marshal(resp)
		if err != nil {
			continue
		}
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n"))
	}

	return scanner.Err()
}
