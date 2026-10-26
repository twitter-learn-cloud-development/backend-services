// Explicit read-only MySQL evidence. Never starts services or seeds datasets.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type planTable struct {
	Name          string  `json:"table"`
	Access        string  `json:"access"`
	Key           string  `json:"key,omitempty"`
	EstimatedRows float64 `json:"estimated_rows"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	return runMySQL(opts, out)
}

func summarizePlan(plan string) ([]planTable, error) {
	if len(plan) > 1<<20 {
		return nil, errors.New("EXPLAIN exceeds bounded input")
	}
	var root any
	if err := json.Unmarshal([]byte(plan), &root); err != nil {
		return nil, errors.New("invalid EXPLAIN JSON")
	}
	tables := []planTable{}
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if name, ok := node["table_name"].(string); ok {
				access, _ := node["access_type"].(string)
				key, _ := node["key"].(string)
				rows, _ := node["rows_examined_per_scan"].(float64)
				tables = append(tables, planTable{Name: name, Access: access, Key: key, EstimatedRows: rows})
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(root)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}
