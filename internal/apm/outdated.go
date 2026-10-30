package apm

import (
	"context"
	"encoding/json"
	"fmt"
)

type OutdatedRow struct {
	Package string
	Current string
	Latest  string
	Source  string
}

type OutdatedResult struct {
	Rows    []OutdatedRow
	Unknown int
}

type outdatedDocument struct {
	Dependencies *[]struct {
		Package string `json:"package"`
		Current string `json:"current"`
		Latest  string `json:"latest"`
		Status  string `json:"status"`
		Source  string `json:"source"`
	} `json:"dependencies"`
}

// Outdated checks the pinned APM global lockfile without changing it.
func (c *Client) Outdated(ctx context.Context) (OutdatedResult, error) {
	result, err := c.runEnv(ctx, []string{"NO_COLOR=1"}, "outdated", "-g", "--json", "--parallel-checks", "4")
	if err != nil {
		return OutdatedResult{}, err
	}
	return parseOutdated(result.Stdout)
}

func parseOutdated(stdout string) (OutdatedResult, error) {
	var doc outdatedDocument
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		return OutdatedResult{}, fmt.Errorf("parse apm outdated --json: %w", err)
	}
	if doc.Dependencies == nil {
		return OutdatedResult{}, fmt.Errorf("parse apm outdated --json: no dependencies field")
	}
	var parsed OutdatedResult
	for _, dep := range *doc.Dependencies {
		switch dep.Status {
		case "unknown":
			parsed.Unknown++
		case "outdated":
			parsed.Rows = append(parsed.Rows, OutdatedRow{Package: dep.Package, Current: dep.Current, Latest: dep.Latest, Source: dep.Source})
		}
	}
	return parsed, nil
}
