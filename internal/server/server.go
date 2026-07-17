package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/connectivity"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const apiVersion = "v2"

type Server struct {
	handler http.Handler
	api     huma.API
}

func New(version string) *Server {
	return NewWithReadiness(version, nil)
}

func NewWithReadiness(version string, checker ReadinessChecker) *Server {
	return newServer(version, checker, nil, nil)
}

func NewWithRuntime(version string, runtime *platform.Runtime) *Server {
	return newServer(version, runtime, runtime, nil)
}

// NewWithRuntimeContext configures production-only process services whose
// lifetime follows the serving command, including public egress-IP refreshes.
func NewWithRuntimeContext(ctx context.Context, version string, runtime *platform.Runtime) *Server {
	if runtime.Config.Connectivity.PublicIPEchoURL == "" {
		return newServer(version, runtime, runtime, nil)
	}
	publicIPs := connectivity.NewPublicIPCache(runtime.Config.Connectivity.PublicIPEchoURL, nil)
	if err := publicIPs.Refresh(ctx); err != nil {
		slog.WarnContext(ctx, "resolve connectivity service public egress IP", "error", err)
	} else {
		slog.InfoContext(ctx, "connectivity service public egress IP resolved")
	}
	go publicIPs.Run(ctx, time.Hour, func(err error) {
		slog.WarnContext(ctx, "refresh connectivity service public egress IP", "error", err)
	})
	return newServer(version, runtime, runtime, publicIPs)
}

func newServer(version string, checker ReadinessChecker, runtime *platform.Runtime, publicIPs connectivityPublicIPMatcher) *Server {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Heya Metadata API", apiVersion)
	config.Info.Description = "Canonical, provenance-aware metadata for Heya media servers."
	config.Servers = []*huma.Server{{URL: "http://localhost:3030", Description: "Local development"}}
	config.OpenAPIPath = "/api/openapi"
	config.DocsPath = "/api/docs"
	config.DocsRenderer = huma.DocsRendererScalar

	api := humago.New(mux, config)
	trustedProxies := []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	if runtime != nil {
		trustedProxies = runtime.Config.Connectivity.TrustedProxyCIDRs
	}
	clientIPs, err := connectivity.NewClientIPResolver(trustedProxies)
	if err != nil {
		panic(fmt.Sprintf("configure connectivity client IP resolver: %v", err))
	}
	registerHealth(api, version, checker)
	registerConnectivity(api, runtime, clientIPs, publicIPs)
	registerAuth(api, runtime)
	registerMovies(api, runtime)
	registerImages(api, runtime)
	registerDiscovery(api, runtime)
	registerEpisodic(api, runtime)
	registerPublications(api, runtime)
	registerReleases(api, runtime)
	registerArtists(api, runtime)
	registerLibrary(api, runtime)
	registerRelations(api, runtime)
	registerPersons(api, runtime)
	registerWorkflowEvents(api, runtime)
	registerAdmin(api, runtime)

	return &Server{handler: captureRequestDetails(cacheHeaders(mux)), api: api}
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) API() huma.API {
	return s.api
}

func OpenAPIDocument(version, format, specVersion string) ([]byte, error) {
	api := New(version).API().OpenAPI()
	format = strings.ToLower(format)
	specVersion = strings.ToLower(specVersion)

	switch format {
	case "json":
		if specVersion == "3.0" {
			return api.Downgrade()
		}
		if specVersion != "3.1" {
			return nil, fmt.Errorf("unknown OpenAPI version %q", specVersion)
		}
		return json.MarshalIndent(api, "", "  ")
	case "yaml":
		if specVersion == "3.0" {
			return api.DowngradeYAML()
		}
		if specVersion != "3.1" {
			return nil, fmt.Errorf("unknown OpenAPI version %q", specVersion)
		}
		return api.YAML()
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
}
