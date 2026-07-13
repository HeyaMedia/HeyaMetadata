package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
	return newServer(version, checker, nil)
}

func NewWithRuntime(version string, runtime *platform.Runtime) *Server {
	return newServer(version, runtime, runtime)
}

func newServer(version string, checker ReadinessChecker, runtime *platform.Runtime) *Server {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Heya Metadata API", apiVersion)
	config.Info.Description = "Canonical, provenance-aware metadata for Heya media servers."
	config.Servers = []*huma.Server{{URL: "http://localhost:3030", Description: "Local development"}}
	config.OpenAPIPath = "/api/openapi"
	config.DocsPath = "/api/docs"
	config.DocsRenderer = huma.DocsRendererScalar

	api := humago.New(mux, config)
	registerHealth(api, version, checker)
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

	return &Server{handler: mux, api: api}
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
