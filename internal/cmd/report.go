package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckInfo CheckStatus = "info"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

type Check struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
	Hint    string      `json:"hint"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

func newReport(checks []Check) Report {
	if checks == nil {
		checks = []Check{}
	}
	report := Report{OK: true, Checks: checks}
	for _, check := range checks {
		if check.Status == CheckFail {
			report.OK = false
		}
	}
	return report
}

func writeJSONReport(cmd *cobra.Command, report Report) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
}
