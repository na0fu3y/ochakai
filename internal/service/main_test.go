package service

import (
	"os"
	"testing"

	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestMain lets this package's run say what did not run in it: without
// OCHAKAI_TEST_DATABASE_URL its integration tests skip, and testdb.Report
// prints how many rather than leaving a green run to imply none.
func TestMain(m *testing.M) { os.Exit(testdb.Report(m)) }
