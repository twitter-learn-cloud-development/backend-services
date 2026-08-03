package profile

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestAtomicResolverReplacesWholeCatalog(t *testing.T) {
	stable, err := NewCatalog([]AgentProfile{testAgentProfile("assist.draft", "v1")}, nil)
	if err != nil {
		t.Fatalf("NewCatalog(stable) error = %v", err)
	}
	candidate, err := NewCatalog([]AgentProfile{testAgentProfile("assist.draft", "v2")}, nil)
	if err != nil {
		t.Fatalf("NewCatalog(candidate) error = %v", err)
	}
	resolver, err := NewAtomicResolver(stable)
	if err != nil {
		t.Fatalf("NewAtomicResolver() error = %v", err)
	}

	selected, err := resolver.Resolve(context.Background(), "assist.draft", SelectionSubject{UserID: 42})
	if err != nil || selected.Version != "v1" {
		t.Fatalf("Resolve(stable) = %+v, %v", selected, err)
	}
	if err := resolver.Replace(candidate); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	selected, err = resolver.Resolve(context.Background(), "assist.draft", SelectionSubject{UserID: 42})
	if err != nil || selected.Version != "v2" {
		t.Fatalf("Resolve(candidate) = %+v, %v", selected, err)
	}
}

func TestAtomicResolverConcurrentResolveAndReplace(t *testing.T) {
	v1, err := NewCatalog([]AgentProfile{testAgentProfile("assist.draft", "v1")}, nil)
	if err != nil {
		t.Fatalf("NewCatalog(v1) error = %v", err)
	}
	v2, err := NewCatalog([]AgentProfile{testAgentProfile("assist.draft", "v2")}, nil)
	if err != nil {
		t.Fatalf("NewCatalog(v2) error = %v", err)
	}
	resolver, err := NewAtomicResolver(v1)
	if err != nil {
		t.Fatalf("NewAtomicResolver() error = %v", err)
	}

	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 500; iteration++ {
				selected, resolveErr := resolver.Resolve(context.Background(), "assist.draft", SelectionSubject{UserID: 42})
				if resolveErr != nil {
					t.Errorf("Resolve() error = %v", resolveErr)
					return
				}
				if selected.Version != "v1" && selected.Version != "v2" {
					t.Errorf("Resolve() version = %q", selected.Version)
					return
				}
			}
		}()
	}
	for iteration := 0; iteration < 500; iteration++ {
		if err := resolver.Replace(v2); err != nil {
			t.Fatalf("Replace(v2) error = %v", err)
		}
		if err := resolver.Replace(v1); err != nil {
			t.Fatalf("Replace(v1) error = %v", err)
		}
	}
	wait.Wait()
}

func TestAtomicResolverProfileSetNeverMixesCatalogSnapshots(t *testing.T) {
	buildCatalog := func(marker string) *Catalog {
		profiles := []AgentProfile{
			testAgentProfile("research.parent", "v1"),
			testAgentProfile("researcher", "v1"),
			testAgentProfile("drafter", "v1"),
		}
		for index := range profiles {
			profiles[index].Prompt.SystemPrompt += ":" + marker
		}
		catalog, err := NewCatalog(profiles, nil)
		if err != nil {
			t.Fatalf("NewCatalog(%s) error = %v", marker, err)
		}
		return catalog
	}
	one := buildCatalog("snapshot-one")
	two := buildCatalog("snapshot-two")
	resolver, err := NewAtomicResolver(one)
	if err != nil {
		t.Fatalf("NewAtomicResolver() error = %v", err)
	}

	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 500; iteration++ {
				selected, resolveErr := resolver.ResolveProfileSet(
					context.Background(),
					"research.parent",
					[]string{"researcher", "drafter"},
					SelectionSubject{UserID: 42},
				)
				if resolveErr != nil {
					t.Errorf("ResolveProfileSet() error = %v", resolveErr)
					return
				}
				marker := "snapshot-one"
				parent, _ := selected.Profile("research.parent")
				if strings.Contains(parent.Prompt.SystemPrompt, "snapshot-two") {
					marker = "snapshot-two"
				}
				for _, profileID := range []string{"researcher", "drafter"} {
					member, _ := selected.Profile(profileID)
					if !strings.Contains(member.Prompt.SystemPrompt, marker) {
						t.Errorf("profile set mixed snapshots: parent=%q member=%q", parent.Prompt.SystemPrompt, member.Prompt.SystemPrompt)
						return
					}
				}
			}
		}()
	}
	for iteration := 0; iteration < 500; iteration++ {
		if err := resolver.Replace(two); err != nil {
			t.Fatalf("Replace(two) error = %v", err)
		}
		if err := resolver.Replace(one); err != nil {
			t.Fatalf("Replace(one) error = %v", err)
		}
	}
	wait.Wait()
}
