package pdfview

import (
	"os"
	"path/filepath"
	"testing"
)

// repoTestdata locates a PDF fixture regardless of where the test runs.
// The canonical home is examples/pdfview/testdata so the example can
// //go:embed it (embed disallows `..` paths). Tests usually run with
// WD = the package dir; the relative search below covers both that
// case and a run from the repo root.
func repoTestdata(t *testing.T, name string) string {
	t.Helper()
	for _, rel := range []string{
		"../examples/pdfview/testdata/" + name,
		"examples/pdfview/testdata/" + name,
	} {
		if _, err := os.Stat(rel); err == nil {
			abs, err := filepath.Abs(rel)
			if err == nil {
				return abs
			}
			return rel
		}
	}
	t.Skipf("testdata fixture %q not found relative to %s", name, mustGetwd(t))
	return ""
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}
