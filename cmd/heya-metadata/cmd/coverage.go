package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/golden"
	"github.com/HeyaMedia/HeyaMetadata/internal/integrity"
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
	command.AddCommand(newCoverageAuditCommand())
	return command
}

func newCoverageAuditCommand() *cobra.Command {
	var strict bool
	var sampleLimit int
	command := &cobra.Command{Use: "audit", Short: "Audit the canonical graph for structural and reconciliation anomalies", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		report, err := integrity.Audit(cmd.Context(), runtime, integrity.Options{SampleLimit: sampleLimit})
		if err != nil {
			return err
		}
		if ui.JSONMode {
			if err := ui.OutputJSON(report); err != nil {
				return err
			}
		} else {
			outputIntegrityReport(report)
		}
		return report.Error(strict)
	}}
	command.Flags().BoolVar(&strict, "strict", false, "Fail when warning checks have findings as well as structural errors")
	command.Flags().IntVar(&sampleLimit, "sample-limit", 5, "Maximum representative samples per finding (0-100)")
	return command
}

func outputIntegrityReport(report integrity.Report) {
	for _, check := range report.Checks {
		if check.Count == 0 {
			ui.Success("%s · clean", check.Code)
			continue
		}
		switch check.Severity {
		case integrity.SeverityError:
			ui.Error("%s · %d · %s", check.Code, check.Count, check.Summary)
		case integrity.SeverityWarning:
			ui.Warn("%s · %d · %s", check.Code, check.Count, check.Summary)
		default:
			ui.Info(check.Code, fmt.Sprintf("%d · %s", check.Count, check.Summary))
		}
		for _, sample := range check.Samples {
			label := sample.Label
			if sample.Kind != "" {
				label = sample.Kind + " · " + label
			}
			if sample.Reference != "" {
				label += " · " + sample.Reference
			}
			if sample.Detail != "" {
				label += " · " + sample.Detail
			}
			ui.Info("  sample", label)
		}
		if check.Remediation != "" {
			ui.Info("  next", check.Remediation)
		}
	}
	ui.Info("Integrity", fmt.Sprintf("%d entities · %d clean · %d errors · %d warnings · %d informational", report.Entities, report.Passed, report.Errors, report.Warnings, report.Info))
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
