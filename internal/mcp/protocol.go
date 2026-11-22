package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/pipeline"
	"autocut/internal/project"
	"autocut/internal/render"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolHandler func(ctx context.Context, args json.RawMessage) (any, error)

type registeredTool struct {
	def     ToolDefinition
	handler ToolHandler
}

type Server struct {
	cfg          *config.Config
	db           *db.DB
	projects     *project.Service
	orchestrator *pipeline.Orchestrator
	renderer     *render.Renderer
	brollDir     string
	tools        []registeredTool
	toolMap      map[string]registeredTool
}

func NewServer(cfg *config.Config, database *db.DB, orch *pipeline.Orchestrator) *Server {
	brollDir := os.Getenv("BROLL_ENGINE_DIR")
	if strings.TrimSpace(brollDir) == "" {
		brollDir = `D:\Dev\.Projects\BRollEngine`
	}
	projSvc := project.NewService(cfg, database)
	s := &Server{
		cfg:          cfg,
		db:           database,
		projects:     projSvc,
		orchestrator: orch,
		renderer:     render.NewRenderer(projSvc.FFmpeg(), projSvc.Fonts()),
		brollDir:     brollDir,
		toolMap:      map[string]registeredTool{},
	}
	s.registerBRollTools()
	s.registerAutoCutTools()
	return s
}

func (s *Server) RegisterTool(def ToolDefinition, handler ToolHandler) {
	rt := registeredTool{def: def, handler: handler}
	s.tools = append(s.tools, rt)
	s.toolMap[def.Name] = rt
}

func (s *Server) ListTools() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(s.tools))
	for _, t := range s.tools {
		defs = append(defs, t.def)
	}
	return defs
}

func (s *Server) CallTool(ctx context.Context, name string, args json.RawMessage) (any, error) {
	tool, ok := s.toolMap[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return tool.handler(ctx, args)
}

func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if resp := s.HandleMessage(ctx, bytes.TrimSpace(line)); resp != nil {
				data, _ := json.Marshal(resp)
				_, _ = out.Write(append(data, '\n'))
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (s *Server) HandleMessage(ctx context.Context, raw []byte) *JSONRPCResponse {
	var req JSONRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &JSONRPCError{Code: -32700, Message: "parse error: " + err.Error()},
		}
	}

	// Notifications have no ID and expect no response.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		if req.Method == "notifications/initialized" || strings.HasPrefix(req.Method, "notifications/") {
			return nil
		}
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := params.ProtocolVersion
		if version == "" {
			version = "2024-11-05"
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": version,
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
				},
				"serverInfo": map[string]any{
					"name":    "autocut-mcp",
					"version": "1.0.0",
				},
			},
		}

	case "ping":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	case "tools/list":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": s.ListTools(),
			},
		}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32602, Message: "invalid arguments: " + err.Error()},
			}
		}
		res, err := s.CallTool(ctx, params.Name, params.Arguments)
		if err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"isError": true,
					"content": []map[string]any{
						{
							"type": "text",
							"text": fmt.Sprintf("Error executing %s: %v", params.Name, err),
						},
					},
				},
			}
		}
		formatted, _ := json.MarshalIndent(res, "", "  ")
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": string(formatted),
					},
				},
			},
		}

	default:
		if len(req.ID) == 0 {
			return nil
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

// notifyLiveDesktop notifies a running AutoCut Desktop sidecar (if session.json exists)
// so that the desktop UI reloads the modified project in real time.
func (s *Server) notifyLiveDesktop(projectID, reason string) {
	sessionPath := filepath.Join(s.cfg.StorageDir, "session.json")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return
	}
	var session struct {
		BaseURL string `json:"base_url"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(data, &session); err != nil || session.BaseURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"reason": reason})
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/projects/%s/notify", strings.TrimRight(session.BaseURL, "/"), projectID), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if session.Token != "" {
		req.Header.Set("X-Autocut-Token", session.Token)
	}
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Do(req)
	if err == nil && resp != nil {
		resp.Body.Close()
	}
}
