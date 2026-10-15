package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
	"github.com/1204244136/MDA/agent/go-service/pkg/pienv"
	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func main() {
	logFile, err := initLogger()
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize logger")
	}
	defer logFile.Close()
	// Shizuku 子进程没有桌面用户目录，配置随资源工作目录保留。
	if runtime.GOOS == "android" && os.Getenv("XDG_CONFIG_HOME") == "" {
		if err := os.Setenv("XDG_CONFIG_HOME", filepath.Join(getCwd(), "config")); err != nil {
			log.Fatal().Err(err).Msg("Failed to configure Android data directory")
		}
	}

	log.Info().
		Str("version", Version).
		Msg("MDA Agent Service")

	pienv.Init()
	i18n.Init()

	if len(os.Args) < 2 {
		log.Fatal().Msg("Usage: go-service <identifier>")
	}

	identifier := os.Args[len(os.Args)-1]
	log.Info().
		Str("identifier", identifier).
		Msg("Starting agent server")

	// Initialize MAA framework first (required before any other MAA calls)
	// Android 由宿主提供 nativeLibraryDir，桌面继续使用 maafw 子目录。
	libDir := frameworkLibDir()
	log.Info().
		Str("libDir", libDir).
		Msg("Initializing MAA framework")
	if err := maa.Init(
		maa.WithLibDir(libDir),
		maa.WithJSONEncoder(json.Marshal),
		maa.WithJSONDecoder(json.Unmarshal),
	); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize MAA framework")
	}
	defer maa.Release()
	log.Info().
		Msg("MAA framework initialized")

	// Initialize toolkit config option
	userPath := getCwd()
	if err := maa.ConfigInitOption(userPath, "{}"); err != nil {
		log.Warn().
			Str("userPath", userPath).
			Err(err).
			Msg("Failed to init toolkit config option")
	} else {
		log.Info().
			Str("userPath", userPath).
			Msg("Toolkit config option initialized")
	}

	// Set version for membership debug-mode detection
	membership.SetVersion(Version)
	membership.SetClientName(pienv.ClientName())

	// Register all custom components and sinks
	registerAll()

	// Start the agent server
	if err := maa.AgentServerStartUp(identifier); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to start agent server")
	}
	log.Info().
		Msg("Agent server started")

	// Wait for the server to finish
	maa.AgentServerJoin()

	// Shutdown
	maa.AgentServerShutDown()
	log.Info().
		Msg("Agent server shutdown")
}

func frameworkLibDir() string {
	if path := os.Getenv("MAAFW_BINARY_PATH"); path != "" {
		return path
	}
	return filepath.Join(getCwd(), "maafw")
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
