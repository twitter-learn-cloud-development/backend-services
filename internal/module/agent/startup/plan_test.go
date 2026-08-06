package startup

import "testing"

func TestParse_Defaults(t *testing.T) {
	plan, err := Parse("", "")
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if plan.Role() != ProcessRoleAll {
		t.Fatalf("role = %q, want %q", plan.Role(), ProcessRoleAll)
	}
	if !plan.StartsAPI() || !plan.StartsWorkers() {
		t.Fatalf("default plan should start both API and worker components: %+v", plan)
	}
	if got := plan.ActiveTrendingReporterOwner(true); got != TrendingReporterOwnerTemporal {
		t.Fatalf("active reporter = %q, want %q", got, TrendingReporterOwnerTemporal)
	}
}

func TestParse_Roles(t *testing.T) {
	tests := []struct {
		name         string
		role         string
		startsAPI    bool
		startsWorker bool
	}{
		{name: "api", role: "api", startsAPI: true},
		{name: "worker", role: "worker", startsWorker: true},
		{name: "all normalized", role: " ALL ", startsAPI: true, startsWorker: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := Parse(tt.role, "temporal")
			if err != nil {
				t.Fatalf("parse plan: %v", err)
			}
			if got := plan.StartsAPI(); got != tt.startsAPI {
				t.Fatalf("StartsAPI = %t, want %t", got, tt.startsAPI)
			}
			if got := plan.StartsWorkers(); got != tt.startsWorker {
				t.Fatalf("StartsWorkers = %t, want %t", got, tt.startsWorker)
			}
		})
	}
}

func TestParse_RejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		role  string
		owner string
	}{
		{name: "unknown role", role: "scheduler", owner: "temporal"},
		{name: "legacy local reporter", role: "all", owner: "local"},
		{name: "unknown reporter", role: "all", owner: "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.role, tt.owner); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
}

func TestPlan_ReporterOwnerMatrix(t *testing.T) {
	tests := []struct {
		name              string
		role              string
		owner             string
		temporalAvailable bool
		want              TrendingReporterOwner
	}{
		{name: "all with temporal", role: "all", owner: "temporal", temporalAvailable: true, want: TrendingReporterOwnerTemporal},
		{name: "worker with temporal", role: "worker", owner: "temporal", temporalAvailable: true, want: TrendingReporterOwnerTemporal},
		{name: "api never owns reporter", role: "api", owner: "temporal", temporalAvailable: true, want: TrendingReporterOwnerDisabled},
		{name: "temporal unavailable disables reporter", role: "worker", owner: "temporal", temporalAvailable: false, want: TrendingReporterOwnerDisabled},
		{name: "explicitly disabled", role: "all", owner: "disabled", temporalAvailable: true, want: TrendingReporterOwnerDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := Parse(tt.role, tt.owner)
			if err != nil {
				t.Fatalf("parse plan: %v", err)
			}
			if got := plan.ActiveTrendingReporterOwner(tt.temporalAvailable); got != tt.want {
				t.Fatalf("active reporter = %q, want %q", got, tt.want)
			}
		})
	}
}
