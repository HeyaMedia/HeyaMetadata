// Package heyametadata is the generated Go client for Heya Metadata's public
// API. Run `make generate-api` after changing a public operation or schema.
package heyametadata

//go:generate go run ../../../cmd/heya-metadata openapi-spec --format yaml --version 3.0 --output ../../../api/openapi.yaml
//go:generate go tool oapi-codegen --config ../../../api/oapi-codegen.yaml ../../../api/openapi.yaml
