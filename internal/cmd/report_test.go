package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type jsonReport struct {
	OK     bool        `json:"ok"`
	Checks []jsonCheck `json:"checks"`
}

type jsonCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func decodeReport(t *testing.T, output *bytes.Buffer) jsonReport {
	t.Helper()
	require.True(t, strings.HasSuffix(output.String(), "\n"), "missing trailing newline")
	require.NotContains(t, output.String(), "\x1b")
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var report jsonReport
	require.NoError(t, decoder.Decode(&report))
	var extra any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
	return report
}

func checkNames(checks []jsonCheck) []string {
	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, check.Name)
	}
	return names
}

func TestReportJSON(t *testing.T) {
	for _, tt := range []struct {
		name     string
		statuses []CheckStatus
		ok       bool
	}{
		{"empty", nil, true},
		{"pass", []CheckStatus{CheckPass}, true},
		{"info", []CheckStatus{CheckInfo}, true},
		{"warn", []CheckStatus{CheckWarn}, true},
		{"fail", []CheckStatus{CheckFail}, false},
		{"mixed", []CheckStatus{CheckPass, CheckInfo, CheckWarn, CheckFail}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var checks []Check
			for _, status := range tt.statuses {
				checks = append(checks, Check{Name: "example", Status: status, Message: "Description"})
			}
			var out bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&out)
			if err := writeJSONReport(cmd, newReport(checks)); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(out.String(), "\n") {
				t.Fatal("missing trailing newline")
			}
			decoder := json.NewDecoder(&out)
			var got struct {
				OK     bool                `json:"ok"`
				Checks []map[string]string `json:"checks"`
			}
			if err := decoder.Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.OK != tt.ok || got.Checks == nil || len(got.Checks) != len(checks) {
				t.Fatalf("unexpected report: %+v", got)
			}
			for i, check := range got.Checks {
				if len(check) != 4 || check["name"] != "example" || check["status"] != string(tt.statuses[i]) || check["message"] != "Description" {
					t.Fatalf("unexpected fields: %#v", check)
				}
				if hint, exists := check["hint"]; !exists || hint != "" {
					t.Fatalf("missing empty hint: %#v", check)
				}
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				t.Fatalf("second decode = %v, want EOF", err)
			}
		})
	}
	if CheckPass != "pass" || CheckInfo != "info" || CheckWarn != "warn" || CheckFail != "fail" {
		t.Fatal("status contract changed")
	}
}

type reportErrorWriter struct{ err error }

func (w reportErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestReportWriteError(t *testing.T) {
	want := errors.New("write failed")
	cmd := &cobra.Command{}
	cmd.SetOut(reportErrorWriter{want})
	if err := writeJSONReport(cmd, newReport(nil)); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
