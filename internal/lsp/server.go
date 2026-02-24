package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	in  *bufio.Reader
	out io.Writer
	mu  sync.Mutex

	rootPath string
	analyzer *Analyzer

	openFiles    sync.Map
	shuttingDown bool
	done         chan struct{}
}

func NewServer(in io.Reader, out io.Writer) *Server {
	return &Server{
		in:   bufio.NewReader(in),
		out:  out,
		done: make(chan struct{}),
	}
}

func (s *Server) Run() error {
	for {
		msg, err := s.readMessage()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.dispatch(msg)
	}
}

func (s *Server) readMessage() (*Message, error) {
	var contentLength int
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			fmt.Sscanf(line, "Content-Length: %d", &contentLength)
		}
	}
	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(s.in, body); err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Server) respond(id *json.RawMessage, result interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
	}
	if result == nil {
		resp.Result = json.RawMessage("null")
	} else {
		data, _ := json.Marshal(result)
		resp.Result = data
	}
	s.send(resp)
}

func (s *Server) respondError(id *json.RawMessage, code int, message string) {
	s.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &ResponseError{Code: code, Message: message},
	})
}

func (s *Server) notify(method string, params interface{}) {
	s.send(Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (s *Server) send(msg interface{}) {
	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal error: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	frame := make([]byte, 0, len(header)+len(body))
	frame = append(frame, header...)
	frame = append(frame, body...)
	s.out.Write(frame)
}

func (s *Server) dispatch(msg *Message) {
	switch msg.Method {
	case "initialize":
		s.handleInitialize(msg)
	case "initialized":
		s.handleInitialized()
	case "textDocument/didOpen":
		s.handleDidOpen(msg)
	case "textDocument/didSave":
		s.handleDidSave()
	case "textDocument/didClose":
		s.handleDidClose(msg)
	case "textDocument/hover":
		s.handleHover(msg)
	case "shutdown":
		close(s.done)
		s.shuttingDown = true
		s.respond(msg.ID, nil)
	case "exit":
		if s.shuttingDown {
			os.Exit(0)
		}
		os.Exit(1)
	default:
		if msg.ID != nil {
			s.respondError(msg.ID, -32601, "method not found: "+msg.Method)
		}
	}
}

func (s *Server) handleInitialize(msg *Message) {
	var params InitializeParams
	json.Unmarshal(msg.Params, &params)

	s.rootPath = uriToPath(params.RootURI)
	if s.rootPath == "" {
		s.rootPath = params.RootPath
	}

	s.respond(msg.ID, InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: TextDocumentSyncOptions{
				OpenClose: true,
				Change:    0,
				Save:      &SaveOptions{IncludeText: false},
			},
			HoverProvider: true,
		},
		ServerInfo: ServerInfo{Name: "carya-lsp", Version: "0.1.0"},
	})
}

func (s *Server) handleInitialized() {
	caryaPath := filepath.Join(s.rootPath, ".carya")
	analyzer, err := NewAnalyzer(s.rootPath, caryaPath)
	if err != nil {
		log.Printf("analyzer init: %v", err)
		return
	}
	s.analyzer = analyzer

	if err := s.analyzer.Refresh(); err != nil {
		log.Printf("initial refresh: %v", err)
	}
	s.publishAllDiagnostics()

	go s.backgroundRefresh()
}

func (s *Server) backgroundRefresh() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if s.analyzer == nil {
				continue
			}
			if err := s.analyzer.Refresh(); err != nil {
				log.Printf("refresh: %v", err)
				continue
			}
			s.publishAllDiagnostics()
		case <-s.done:
			return
		}
	}
}

func (s *Server) handleDidOpen(msg *Message) {
	var params DidOpenTextDocumentParams
	json.Unmarshal(msg.Params, &params)
	s.openFiles.Store(params.TextDocument.URI, true)
	s.publishDiagnostics(params.TextDocument.URI)
}

func (s *Server) handleDidSave() {
	if s.analyzer == nil {
		return
	}
	go func() {
		if err := s.analyzer.Refresh(); err != nil {
			log.Printf("refresh on save: %v", err)
			return
		}
		s.publishAllDiagnostics()
	}()
}

func (s *Server) handleDidClose(msg *Message) {
	var params DidCloseTextDocumentParams
	json.Unmarshal(msg.Params, &params)
	s.openFiles.Delete(params.TextDocument.URI)
	s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         params.TextDocument.URI,
		Diagnostics: []Diagnostic{},
	})
}

func (s *Server) handleHover(msg *Message) {
	var params HoverParams
	json.Unmarshal(msg.Params, &params)

	if s.analyzer == nil {
		s.respond(msg.ID, nil)
		return
	}

	relPath := s.relativePathFromURI(params.TextDocument.URI)
	report := s.analyzer.ReportForFile(relPath)
	if report == nil || len(report.Teammates) == 0 {
		s.respond(msg.ID, nil)
		return
	}

	s.respond(msg.ID, Hover{
		Contents: MarkupContent{
			Kind:  "markdown",
			Value: formatHoverContent(report),
		},
	})
}

func formatHoverContent(report *FileReport) string {
	var b strings.Builder

	if len(report.Teammates) == 1 {
		td := report.Teammates[0]
		if td.IsConflict {
			fmt.Fprintf(&b, "**carya** — predicted conflict with `%s`\n\n", td.UserID)
		} else {
			fmt.Fprintf(&b, "**carya** — `%s` is also editing this file\n\n", td.UserID)
		}
		writePatch(&b, td.Patch, 50)
	} else {
		fmt.Fprintf(&b, "**carya** — %d teammates are also editing this file\n\n", len(report.Teammates))
		for i, td := range report.Teammates {
			if i > 0 {
				b.WriteString("\n---\n\n")
			}
			if td.IsConflict {
				fmt.Fprintf(&b, "**`%s`** _(conflict)_\n\n", td.UserID)
			} else {
				fmt.Fprintf(&b, "**`%s`**\n\n", td.UserID)
			}
			writePatch(&b, td.Patch, 30)
		}
	}

	return b.String()
}

func writePatch(b *strings.Builder, patch string, maxLines int) {
	if patch == "" {
		return
	}
	b.WriteString("```diff\n")
	lines := strings.Split(patch, "\n")
	if len(lines) > maxLines {
		b.WriteString(strings.Join(lines[:maxLines], "\n"))
		fmt.Fprintf(b, "\n... (%d more lines)\n", len(lines)-maxLines)
	} else {
		b.WriteString(patch)
		if !strings.HasSuffix(patch, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("```\n")
}

func (s *Server) publishAllDiagnostics() {
	s.openFiles.Range(func(key, _ interface{}) bool {
		s.publishDiagnostics(key.(string))
		return true
	})
}

func (s *Server) publishDiagnostics(uri string) {
	if s.analyzer == nil {
		return
	}

	relPath := s.relativePathFromURI(uri)
	report := s.analyzer.ReportForFile(relPath)

	var diagnostics []Diagnostic
	if report != nil {
		for _, td := range report.Teammates {
			severity := SeverityWarning
			msg := fmt.Sprintf("%s is also editing this file", td.UserID)
			if td.IsConflict {
				severity = SeverityError
				msg = fmt.Sprintf("predicted merge conflict with %s", td.UserID)
			}
			diagnostics = append(diagnostics, Diagnostic{
				Range: Range{
					Start: Position{Line: 0, Character: 0},
					End:   Position{Line: 0, Character: 0},
				},
				Severity: severity,
				Source:   "carya",
				Message:  msg,
			})
		}
	}

	s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	})
}

func (s *Server) relativePathFromURI(uri string) string {
	absPath := uriToPath(uri)
	rel, err := filepath.Rel(s.rootPath, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		parsed, err := url.Parse(uri)
		if err == nil {
			return parsed.Path
		}
	}
	return uri
}
