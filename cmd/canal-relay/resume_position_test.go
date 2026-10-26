package main

import (
	"testing"

	"github.com/go-mysql-org/go-mysql/mysql"
)

func TestResolveResumePosition(t *testing.T) {
	logs := []string{"mysql-bin.000021", "mysql-bin.000022"}
	tests := []struct {
		name      string
		saved     mysql.Position
		want      mysql.Position
		recovered bool
		wantError bool
	}{
		{name: "current", saved: mysql.Position{Name: logs[1], Pos: 123}, want: mysql.Position{Name: logs[1], Pos: 123}},
		{name: "purged", saved: mysql.Position{Name: "mysql-bin.000020", Pos: 999}, want: mysql.Position{Name: logs[0], Pos: 4}, recovered: true},
		{name: "missing", saved: mysql.Position{}, want: mysql.Position{}},
		{name: "unknown future", saved: mysql.Position{Name: "mysql-bin.000023", Pos: 4}, wantError: true},
		{name: "different server", saved: mysql.Position{Name: "other-bin.000020", Pos: 4}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, recovered, err := resolveResumePosition(tt.saved, logs)
			if (err != nil) != tt.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want || recovered != tt.recovered {
				t.Fatalf("got %v, recovered=%t; want %v, recovered=%t", got, recovered, tt.want, tt.recovered)
			}
		})
	}
	if _, _, err := resolveResumePosition(mysql.Position{}, nil); err == nil {
		t.Fatal("empty binary log list must fail closed")
	}
}
