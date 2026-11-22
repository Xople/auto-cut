package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/models"
	"autocut/internal/pipeline"
)

func TestMCPProtocolAndAutoCutTools(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.LoadConfig()
	cfg.StorageDir = tmp

	database, err := db.InitDB(tmp)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer database.Close()

	orch := pipeline.NewOrchestrator(cfg, database)
	srv := NewServer(cfg, database, orch)

	// 1. Test initialize
	initResp := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`))
	if initResp == nil || initResp.Error != nil {
		t.Fatalf("initialize failed: %+v", initResp)
	}

	// 2. Test tools/list
	listResp := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if listResp == nil || listResp.Error != nil {
		t.Fatalf("tools/list failed: %+v", listResp)
	}
	tools := srv.ListTools()
	if len(tools) < 12 {
		t.Fatalf("expected at least 12 tools, got %d", len(tools))
	}

	// 3. Test autocut_create_project via CallTool
	res, err := srv.CallTool(context.Background(), "autocut_create_project", json.RawMessage(`{"name":"Among Us Short Test"}`))
	if err != nil {
		t.Fatalf("autocut_create_project failed: %v", err)
	}
	proj, ok := res.(*models.Project)
	if !ok {
		t.Fatalf("expected *models.Project, got %T", res)
	}
	if proj.Width != 1080 || proj.Height != 1920 || proj.FPS != 60 {
		t.Fatalf("expected 1080x1920 @ 60fps default, got %dx%d @ %.0ffps", proj.Width, proj.Height, proj.FPS)
	}
}
