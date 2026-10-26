package releaseplan

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sourceFixture = "1234567890123456789012345678901234567890"

func fixtureFile(t *testing.T, root, path string, body []byte) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0755))
	require.NoError(t, os.WriteFile(absolute, body, 0600))
}
func manifestFixture(t *testing.T, published bool, omitted ...string) string {
	t.Helper()
	root := t.TempDir()
	catalog := Catalog{Version: 1, Include: []Component{{"gateway", "./cmd/gateway", "deploy/docker/gateway.dockerfile", ".", "GATEWAY_IMAGE"}}}
	body, err := json.Marshal(catalog)
	require.NoError(t, err)
	fixtureFile(t, root, CatalogPath, body)
	paths, err := inputPaths(root, catalog)
	require.NoError(t, err)
	for _, path := range paths {
		if path != CatalogPath {
			fixtureFile(t, root, path, []byte("bounded source input"))
		}
	}
	fixtureFile(t, root, "deploy/mysql/migrations/fixture.sql", []byte("review-only"))
	builder, runtime := "golang:1.25.5-alpine", "alpine:latest"
	if published {
		builder = "golang@sha256:" + strings.Repeat("b", 64)
		runtime = "alpine@sha256:" + strings.Repeat("c", 64)
	}
	receipt, err := NewImage(root, ImageReceipt{Service: "gateway", SourceSHA: sourceFixture, Repository: "registry.invalid/project/gateway", Digest: "sha256:" + strings.Repeat("a", 64), Published: published, BuilderImage: builder, RuntimeImage: runtime})
	require.NoError(t, err)
	require.NoError(t, Write(root, "images/gateway.json", receipt))
	fixtureFile(t, root, "evidence.log", []byte("fixture command completed; not actual CI evidence"))
	for _, kind := range RequiredChecks() {
		if len(omitted) > 0 && kind == omitted[0] {
			continue
		}
		receipt, err := NewCheck(root, kind, sourceFixture, "evidence.log")
		require.NoError(t, err)
		require.NoError(t, Write(root, "checks/"+kind+".json", receipt))
	}
	return root
}
func TestManifestExactInputsDeterministicCandidateAndPublished(t *testing.T) {
	for _, published := range []bool{false, true} {
		root := manifestFixture(t, published)
		one, err := Assemble(root, sourceFixture, "images", "checks")
		require.NoError(t, err)
		two, err := Assemble(root, sourceFixture, "images", "checks")
		require.NoError(t, err)
		require.Equal(t, one, two)
		require.Equal(t, published, one.ImagesPublished)
		require.False(t, one.DeploymentAccepted)
		require.NoError(t, Write(root, "verify/manifest.json", one))
		verified, err := Verify(root, "verify/manifest.json", false)
		require.NoError(t, err)
		require.Equal(t, one, verified)
		_, err = Verify(root, "verify/manifest.json", true)
		if published {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.Len(t, one.Checks, len(RequiredChecks()))
		require.Contains(t, one.ImageEnvironment["GATEWAY_IMAGE"], "@sha256:")
		before := one.Inputs
		fixtureFile(t, root, "deploy/mysql/migrations/fixture.sql", []byte("changed reviewed SQL"))
		changed, err := Assemble(root, sourceFixture, "images", "checks")
		require.NoError(t, err)
		require.NotEqual(t, before, changed.Inputs)
		_, err = Verify(root, "verify/manifest.json", false)
		require.ErrorContains(t, err, "drift")
		require.NoError(t, Write(root, "out/manifest.json", one))
		require.Error(t, Write(root, "out/manifest.json", two))
	}
}

func TestManifestRejectsAlteredEnvironmentAndClaimedDeployment(t *testing.T) {
	for _, deployment := range []bool{false, true} {
		root := manifestFixture(t, true)
		manifest, err := Assemble(root, sourceFixture, "images", "checks")
		require.NoError(t, err)
		if deployment {
			manifest.DeploymentAccepted = true
		} else {
			manifest.ImageEnvironment["GATEWAY_IMAGE"] = "registry.invalid/other@sha256:" + strings.Repeat("a", 64)
		}
		require.NoError(t, Write(root, "bad.json", manifest))
		_, err = Verify(root, "bad.json", true)
		require.Error(t, err)
	}
}
func TestManifestRejectsIncompleteExtraSourceCatalogAndBadEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"missing", func(t *testing.T, root string) {}},
		{"extra", func(t *testing.T, root string) { fixtureFile(t, root, "images/other.json", []byte("{}")) }},
		{"source", func(t *testing.T, root string) {
			path := filepath.Join(root, "images/gateway.json")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			fixtureFile(t, root, "images/gateway.json", []byte(strings.ReplaceAll(string(body), sourceFixture, strings.Repeat("f", 40))))
		}},
		{"catalog", func(t *testing.T, root string) {
			body, err := os.ReadFile(filepath.Join(root, CatalogPath))
			require.NoError(t, err)
			fixtureFile(t, root, CatalogPath, append(body, '\n'))
		}},
		{"failed", func(t *testing.T, root string) {
			body, err := os.ReadFile(filepath.Join(root, "checks/go_test.json"))
			require.NoError(t, err)
			fixtureFile(t, root, "checks/go_test.json", []byte(strings.ReplaceAll(string(body), `"passed": true`, `"passed": false`)))
		}},
		{"duplicate", func(t *testing.T, root string) {
			body, err := os.ReadFile(filepath.Join(root, "checks/go_test.json"))
			require.NoError(t, err)
			fixtureFile(t, root, "checks/go_test.json", []byte(strings.Replace(string(body), "{", `{"passed": true,`, 1)))
		}},
		{"unknown", func(t *testing.T, root string) {
			body, err := os.ReadFile(filepath.Join(root, "checks/go_test.json"))
			require.NoError(t, err)
			fixtureFile(t, root, "checks/go_test.json", []byte(strings.Replace(string(body), "{", `{"credential": "private",`, 1)))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			omitted := []string{}
			if test.name == "missing" {
				omitted = append(omitted, "go_test")
			}
			root := manifestFixture(t, false, omitted...)
			test.mutate(t, root)
			_, err := Assemble(root, sourceFixture, "images", "checks")
			require.Error(t, err)
		})
	}
}
func TestManifestBoundedPathsDigestsAndCheckEvidence(t *testing.T) {
	root := manifestFixture(t, false)
	for _, path := range []string{"../outside", "/absolute", `C:\outside`, "..\\bad"} {
		require.Error(t, Write(root, path, map[string]bool{}))
	}
	for _, digest := range []string{"latest", "sha256:" + strings.Repeat("0", 64), "sha256:short"} {
		_, err := NewImage(root, ImageReceipt{Service: "gateway", SourceSHA: sourceFixture, Repository: "registry.invalid/project/gateway", Digest: digest, BuilderImage: "golang:1.25.5-alpine", RuntimeImage: "alpine:latest"})
		require.Error(t, err)
	}
	require.Error(t, ValidateBases("golang:latest", "alpine:latest", true))
	require.Error(t, ValidateBases("golang\nmalicious", "alpine:latest", false))
	_, err := NewCheck(root, "fake", sourceFixture, "evidence.log")
	require.Error(t, err)
	fixtureFile(t, root, "empty.log", nil)
	_, err = NewCheck(root, "go_test", sourceFixture, "empty.log")
	require.Error(t, err)
	fixtureFile(t, root, "large.log", []byte(strings.Repeat("x", 1024*1024+1)))
	_, err = NewCheck(root, "go_test", sourceFixture, "large.log")
	require.Error(t, err)
	require.False(t, ValidSource(strings.Repeat("0", 40)))
}
func TestCatalogCoversComposeBuildsPackageEntrypointsAndWorkflowGates(t *testing.T) {
	root := "../.."
	catalog, _, err := Load(root)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(root, "docker-compose.yaml"))
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Build interface{} `yaml:"build"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(body, &compose))
	remaining := map[string]bool{}
	for name, item := range compose.Services {
		if item.Build != nil {
			remaining[name] = true
		}
	}
	for _, item := range catalog.Include {
		delete(remaining, item.Service)
		body, err := read(root, item.Dockerfile)
		require.NoError(t, err)
		require.NotContains(t, string(body), "/main.go")
		require.Contains(t, string(body), "ARG BUILDER_IMAGE")
		require.Contains(t, string(body), "ARG RUNTIME_IMAGE")
		if item.Package != "" {
			require.Contains(t, string(body), item.Package)
			require.Contains(t, string(body), "-trimpath")
		}
	}
	require.Empty(t, remaining)
	body, err = read(root, ".github/workflows/ci-cd.yaml")
	require.NoError(t, err)
	var workflow struct {
		Permissions map[string]string      `yaml:"permissions"`
		Jobs        map[string]interface{} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(body, &workflow))
	require.Equal(t, "read", workflow.Permissions["contents"])
	for _, job := range []string{"plan", "go", "web", "mobile", "production-config", "build-images", "release-manifest"} {
		require.Contains(t, workflow.Jobs, job)
	}
	require.NotContains(t, string(body), "git push")
	require.NotContains(t, string(body), "sudo wget")
	require.NotContains(t, string(body), "paths-ignore")
	require.Contains(t, string(body), "go-version-file: go.mod")
	require.Contains(t, string(body), "type=oci")
	require.Contains(t, string(body), "inputs.publish")
	makefile, err := read(root, "Makefile")
	require.NoError(t, err)
	require.NotContains(t, string(makefile), "/main.go")
	require.Contains(t, string(makefile), "./cmd/...")
}
