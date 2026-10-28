package main

import (
	"io"
	"log"
	"net/http"
	"os"

	"autocut/internal/api"
	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/logger"
	"autocut/internal/pipeline"
)

func watchParent() {
	if os.Getenv("AUTOCUT_WATCH_STDIN") != "1" {
		return
	}
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		logger.Info("Desktop shell terputus, menghentikan engine.")
		os.Exit(0)
	}()
}

func main() {
	if err := logger.InitLogger("logs"); err != nil {
		log.Printf("Warning: Gagal inisialisasi file logger: %v", err)
	}
	defer logger.Close()

	watchParent()

	cfg := config.LoadConfig()

	logger.Info("==================================================")
	logger.Info("    TxShD Video Pipeline (AutoCut Workstation)    ")
	logger.Info("==================================================")
	logger.Info("[Config] Port: %s", cfg.Port)
	logger.Info("[Config] Storage: %s", cfg.StorageDir)
	logger.Info("[Config] NVENC GPU Acceleration: %v", cfg.UseNVENC)
	logger.Info("[Config] DeepSeek Key configured: %v", cfg.DeepSeekAPIKey != "")
	logger.Info("[Config] TypeSafe Key configured: %v", cfg.TypeSafeAPIKey != "")
	logger.Info("[Config] Gemini Key configured: %v", cfg.GeminiAPIKey != "")

	database, err := db.InitDB(cfg.StorageDir)
	if err != nil {
		logger.Error("Fatal: Failed to initialize SQLite database: %v", err)
		log.Fatalf("Fatal: Failed to initialize SQLite database: %v", err)
	}

	orchestrator := pipeline.NewOrchestrator(cfg, database)
	server := api.NewServer(cfg, database, orchestrator)

	addr := "127.0.0.1:" + cfg.Port
	logger.Info("🚀 AutoCut Server berjalan di http://%s", addr)
	logger.Info("Tekan Ctrl+C untuk menghentikan server.")
	logger.Info("==================================================")

	if err := http.ListenAndServe(addr, server); err != nil {
		logger.Error("Server stopped with error: %v", err)
		log.Fatalf("Server stopped: %v", err)
	}
}
