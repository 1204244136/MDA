package main

import (
	"github.com/1204244136/MDA/agent/go-service/common/centerpriority"
	"github.com/1204244136/MDA/agent/go-service/pkg/resource"
	"github.com/1204244136/MDA/agent/go-service/taskersink/aspectratio"
	"github.com/1204244136/MDA/agent/go-service/taskersink/hdrcheck"
	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
	"github.com/1204244136/MDA/agent/go-service/taskersink/processcheck"
	"github.com/rs/zerolog/log"
)

func registerAll() {
	// Resource Sink
	resource.EnsureResourcePathSink()

	// Common Custom
	centerpriority.Register()

	// Pre-Check Custom
	aspectratio.Register()
	hdrcheck.Register()
	processcheck.Register()
	membership.Register()

	log.Info().
		Msg("All custom components and sinks registered successfully")
}
