package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func Banner(version string) string {
	title := "HEYA METADATA"
	tagline := fmt.Sprintf("v%s — metadata, properly sourced", version)
	if !ColorEnabled {
		return title + "\n" + tagline + "\n"
	}

	box := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSecondary).
		Padding(0, 2).
		Render(title)

	return box + "\n" + StyleDim.Italic(true).Render(tagline) + "\n"
}

func HelpBanner(version string) string {
	commands := []string{
		fmt.Sprintf("  %s  %s", Primary("serve"), "Start the HTTP API"),
		fmt.Sprintf("  %s  %s", Primary("worker"), "Start durable background workers"),
		fmt.Sprintf("  %s  %s", Primary("dev-proxy"), "Run the local development proxy"),
		fmt.Sprintf("  %s  %s", Primary("migrate"), "Manage database schemas"),
		fmt.Sprintf("  %s  %s", Primary("smoke"), "Verify the platform pipeline"),
		fmt.Sprintf("  %s  %s", Primary("movie"), "Ingest and inspect canonical movies"),
		fmt.Sprintf("  %s  %s", Primary("artist"), "Ingest, update, and inspect canonical artists"),
		fmt.Sprintf("  %s  %s", Primary("release-group"), "Manage canonical music release groups"),
		fmt.Sprintf("  %s  %s", Primary("release"), "Manage canonical issued music releases"),
		fmt.Sprintf("  %s  %s", Primary("recording"), "Manage canonical music recordings"),
		fmt.Sprintf("  %s  %s", Primary("musical-work"), "Manage canonical composed musical works"),
		fmt.Sprintf("  %s  %s", Primary("person"), "Manage canonical people"),
		fmt.Sprintf("  %s  %s", Primary("book"), "Manage canonical books"),
		fmt.Sprintf("  %s  %s", Primary("discover"), "Discover and rank upstream candidates"),
		fmt.Sprintf("  %s  %s", Primary("provider"), "Collect provider source evidence"),
		fmt.Sprintf("  %s  %s", Primary("coverage"), "Verify semantic catalog coverage"),
		fmt.Sprintf("  %s  %s", Primary("retention"), "Expire short-lived provider blobs"),
		fmt.Sprintf("  %s  %s", Primary("version"), "Show build information"),
		fmt.Sprintf("  %s  %s", Primary("openapi-spec"), "Render the OpenAPI document"),
	}

	return Banner(version) + "\n" + Bold("Commands:") + "\n" +
		strings.Join(commands, "\n") + "\n\n" +
		Dim("Run 'heya-metadata <command> --help' for command details.") + "\n"
}
