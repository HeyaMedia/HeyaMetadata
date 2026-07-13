package integrity

import "testing"

func TestDefinitionsHaveUniqueCodesAndValidQueries(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, definition := range definitions() {
		if definition.Code == "" || definition.Domain == "" || definition.Summary == "" {
			t.Fatalf("incomplete definition: %+v", definition.Check)
		}
		if seen[definition.Code] {
			t.Fatalf("duplicate check code %q", definition.Code)
		}
		seen[definition.Code] = true
		if definition.countSQL == "" || definition.sampleSQL == "" {
			t.Fatalf("check %q has no query", definition.Code)
		}
		switch definition.Severity {
		case SeverityError, SeverityWarning, SeverityInfo:
		default:
			t.Fatalf("check %q has invalid severity %q", definition.Code, definition.Severity)
		}
	}
}

func TestReportFinalizationAndStrictFailure(t *testing.T) {
	t.Parallel()
	report := Report{Checks: []Check{
		{Code: "clean", Domain: "core", Severity: SeverityError},
		{Code: "warning", Domain: "music", Severity: SeverityWarning, Count: 2},
		{Code: "information", Domain: "written", Severity: SeverityInfo, Count: 5},
	}}
	report.finalize()
	if report.Passed != 1 || report.Errors != 0 || report.Warnings != 1 || report.Info != 1 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if err := report.Error(false); err != nil {
		t.Fatalf("non-strict report failed: %v", err)
	}
	if err := report.Error(true); err == nil {
		t.Fatal("strict report should fail on warnings")
	}
}

func TestReportAlwaysFailsOnStructuralErrors(t *testing.T) {
	t.Parallel()
	report := Report{Checks: []Check{{Code: "broken", Domain: "core", Severity: SeverityError, Count: 1}}}
	report.finalize()
	if err := report.Error(false); err == nil {
		t.Fatal("report should fail on structural errors")
	}
}
