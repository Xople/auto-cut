package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Port                  string  `json:"port"`
	FFmpegPath            string  `json:"ffmpeg_path"`
	DeepSeekAPIKey        string  `json:"deepseek_api_key"`
	TypeSafeAPIKey        string  `json:"typesafe_api_key"`
	GeminiAPIKey          string  `json:"gemini_api_key"`
	WhisperCLIPath        string  `json:"whisper_cli_path"`
	WhisperModelPath      string  `json:"whisper_model_path"`
	StorageDir            string  `json:"storage_dir"`
	UseNVENC              bool    `json:"use_nvenc"`
	MinSilenceDuration    float64 `json:"min_silence_duration"`
	SilenceNoiseThreshold string  `json:"silence_noise_threshold"`
	SafetyPaddingMs       float64 `json:"safety_padding_ms"`
}

func LoadConfig() *Config {
	cfg := &Config{
		Port:                  "8080",
		FFmpegPath:            "ffmpeg",
		WhisperCLIPath:        filepath.Join("bin", "whisper", "whisper-cli.exe"),
		WhisperModelPath:      filepath.Join("bin", "whisper", "models", "ggml-small.bin"),
		StorageDir:            "data",
		UseNVENC:              true,
		MinSilenceDuration:    0.4,
		SilenceNoiseThreshold: "-30dB",
		SafetyPaddingMs:       60.0,
	}

	// Optional config.json, overridden by environment below.
	if data, err := os.ReadFile("config.json"); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	// Environment wins so the desktop shell can point the sidecar at its app data dir.
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("FFMPEG_PATH"); v != "" {
		cfg.FFmpegPath = v
	}
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		cfg.DeepSeekAPIKey = v
	}
	if v := os.Getenv("TYPESAFE_API_KEY"); v != "" {
		cfg.TypeSafeAPIKey = v
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.GeminiAPIKey = v
	}
	if v := os.Getenv("WHISPER_CLI_PATH"); v != "" {
		cfg.WhisperCLIPath = v
	}
	if v := os.Getenv("WHISPER_MODEL_PATH"); v != "" {
		cfg.WhisperModelPath = v
	}
	if v := os.Getenv("STORAGE_DIR"); v != "" {
		cfg.StorageDir = v
	}
	if v := os.Getenv("USE_NVENC"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.UseNVENC = b
		}
	}
	if v := os.Getenv("MIN_SILENCE_DURATION"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.MinSilenceDuration = f
		}
	}
	if v := os.Getenv("SILENCE_NOISE_THRESHOLD"); v != "" {
		cfg.SilenceNoiseThreshold = v
	}
	if v := os.Getenv("SAFETY_PADDING_MS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SafetyPaddingMs = f
		}
	}

	return cfg
}

func (c *Config) SaveConfig() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("config.json", data, 0644)
}
