package websearch

import "testing"

func TestBraveProviderFactorySharesDeploymentAdmission(t *testing.T) {
	factory, err := NewBraveProviderFactory(BraveProviderFactoryConfig{MaxConcurrent: 2})
	if err != nil {
		t.Fatalf("NewBraveProviderFactory() error = %v", err)
	}
	first, err := factory.New(DefaultBraveBaseURL, "first-secret")
	if err != nil {
		t.Fatalf("factory.New(first) error = %v", err)
	}
	second, err := factory.New(DefaultBraveBaseURL, "second-secret")
	if err != nil {
		t.Fatalf("factory.New(second) error = %v", err)
	}
	firstBrave, firstOK := first.(*BraveProvider)
	secondBrave, secondOK := second.(*BraveProvider)
	if !firstOK || !secondOK {
		t.Fatalf("factory providers = %T, %T", first, second)
	}
	if firstBrave.admission != secondBrave.admission || firstBrave.admission != factory.admission {
		t.Fatal("tenant providers do not share the deployment admission gate")
	}
	if cap(firstBrave.admission) != 2 {
		t.Fatalf("shared admission capacity = %d, want 2", cap(firstBrave.admission))
	}
}
