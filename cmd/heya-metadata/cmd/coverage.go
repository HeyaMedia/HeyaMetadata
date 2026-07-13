package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/golden"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

type goldenVerifier func(context.Context, *platform.Runtime) (golden.Report, error)

func newCoverageCommand() *cobra.Command {
	command := &cobra.Command{Use: "coverage", Short: "Verify semantic golden-entity coverage", Args: cobra.NoArgs}
	verifiers := []struct {
		name        string
		description string
		verify      goldenVerifier
	}{
		{"movie", "movie", golden.VerifyMovie},
		{"tv", "TV", golden.VerifyTV},
		{"books", "books", golden.VerifyBooks},
		{"music", "music", golden.VerifyMusic},
		{"people", "people", golden.VerifyPeople},
	}
	for _, verifier := range verifiers {
		command.AddCommand(newCoverageVerifyCommand("verify-"+verifier.name, "Verify the "+verifier.description+" catalog through the real v2 router", verifier.verify))
	}
	command.AddCommand(&cobra.Command{Use: "verify-all", Short: "Verify every semantic catalog through the real v2 router", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		reports := make([]golden.Report, 0, len(verifiers))
		var reportErrors []error
		for _, verifier := range verifiers {
			report, verifyErr := verifier.verify(cmd.Context(), runtime)
			if verifyErr != nil {
				return verifyErr
			}
			reports = append(reports, report)
			if reportErr := report.Error(); reportErr != nil {
				reportErrors = append(reportErrors, reportErr)
			}
		}
		if ui.JSONMode {
			if outputErr := ui.OutputJSON(reports); outputErr != nil {
				return outputErr
			}
		} else {
			for _, report := range reports {
				outputCoverageReport(report)
			}
		}
		return errors.Join(reportErrors...)
	}})
	return command
}

func newCoverageVerifyCommand(use, short string, verify goldenVerifier) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		report, err := verify(cmd.Context(), runtime)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			if outputErr := ui.OutputJSON(report); outputErr != nil {
				return outputErr
			}
		} else {
			outputCoverageReport(report)
		}
		return report.Error()
	}}
}

func outputCoverageReport(report golden.Report) {
	for _, check := range report.Checks {
		if check.Passed {
			ui.Success("%s · %s", check.EntryID, check.Reference)
		} else {
			ui.Error("%s · %s: %s", check.EntryID, check.Reference, check.Error)
		}
	}
	ui.Info("Coverage · "+report.Domain, fmt.Sprintf("%d passed, %d failed", report.Passed, report.Failed))
}
