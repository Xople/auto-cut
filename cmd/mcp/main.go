package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/logger"
	"autocut/internal/mcp"
	"autocut/internal/pipeline"
)

func main() {
	// Keep stdout strictly clean for MCP JSON-RPC / CLI JSON output
	logger.SetConsoleOutput(nil)
	listTools := flag.Bool("list-tools", false, "Print registered MCP tools as JSON and exit")
	callTool := flag.String("call", "", "Directly execute an MCP tool by name and print JSON result")
	callArgs := flag.String("args", "{}", "JSON arguments for --call")
	argsFile := flag.String("args-file", "", "Path to JSON file containing arguments for --call")
	storageDir := flag.String("storage", "", "Override storage directory (defaults to AutoCut Desktop %APPDATA%\\com.txshd.autocut)")
	flag.Parse()

	cfg := config.LoadConfig()
	if *storageDir != "" {
		cfg.StorageDir = *storageDir
	}

	database, err := db.InitDB(cfg.StorageDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "autocut-mcp: failed to initialize SQLite database at %s: %v\n", cfg.StorageDir, err)
		os.Exit(1)
	}
	defer database.Close()

	orch := pipeline.NewOrchestrator(cfg, database)
	srv := mcp.NewServer(cfg, database, orch)

	if *listTools {
		data, _ := json.MarshalIndent(srv.ListTools(), "", "  ")
		fmt.Println(string(data))
		return
	}

	if *callTool != "" {
		rawArgs := []byte(*callArgs)
		if *argsFile != "" {
			fileBytes, err := os.ReadFile(*argsFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "autocut-mcp: failed reading --args-file %s: %v\n", *argsFile, err)
				os.Exit(1)
			}
			rawArgs = bytes.TrimPrefix(fileBytes, []byte{0xEF, 0xBB, 0xBF})
		}
		res, err := srv.CallTool(context.Background(), *callTool, rawArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "autocut-mcp: tool %s failed: %v\n", *callTool, err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(data))
		return
	}

	if err := srv.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "autocut-mcp: stdio server error: %v\n", err)
		os.Exit(1)
	}
}
