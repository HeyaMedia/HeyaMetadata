package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const apiVersion = "v2"

type Server struct {
	handler http.Handler
	api     huma.API
}

func New(version string) *Server {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("Heya Metadata API", apiVersion)
	config.Info.Description = "Canonical, provenance-aware metadata for Heya media servers."
	config.Servers = []*huma.Server{{URL: "http://localhost:3030", Description: "Local development"}}
	config.OpenAPIPath = "/api/openapi"
	config.DocsPath = "/api/docs"

	api := humago.New(mux, config)
	registerHealth(api, version)

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
