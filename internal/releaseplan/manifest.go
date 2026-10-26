// Package releaseplan creates bounded offline release evidence. It never runs
// Docker, contacts a registry, reads deployment credentials or deploys services.
package releaseplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

const CatalogPath = "deploy/release/catalog.json"

var requiredChecks = [...]string{"go_test", "go_vet", "go_race", "package_build", "web_build", "web_test", "mobile_analyze", "mobile_test", "production_config"}

func RequiredChecks() []string { return append([]string(nil), requiredChecks[:]...) }

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var shaPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var repositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,255}$`)

type Component struct {
	Service    string `json:"service"`
	Package    string `json:"package,omitempty"`
	Dockerfile string `json:"dockerfile"`
	Context    string `json:"context"`
	ImageEnv   string `json:"image_env"`
}
type Catalog struct {
	Version int         `json:"version"`
	Include []Component `json:"include"`
}
type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type ImageReceipt struct {
	Service       string `json:"service"`
	SourceSHA     string `json:"source_sha"`
	CatalogSHA256 string `json:"catalog_sha256"`
	Repository    string `json:"repository"`
	Digest        string `json:"digest"`
	Published     bool   `json:"published"`
	BuilderImage  string `json:"builder_image"`
	RuntimeImage  string `json:"runtime_image"`
}
type CheckReceipt struct {
	Kind           string `json:"kind"`
	SourceSHA      string `json:"source_sha"`
	Passed         bool   `json:"passed"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}
type Manifest struct {
	Version            int               `json:"version"`
	SourceSHA          string            `json:"source_sha"`
	CatalogSHA256      string            `json:"catalog_sha256"`
	ImagesPublished    bool              `json:"images_published"`
	DeploymentAccepted bool              `json:"deployment_accepted"`
	Images             []ImageReceipt    `json:"images"`
	Checks             []CheckReceipt    `json:"checks"`
	Inputs             []FileDigest      `json:"inputs"`
	ImageEnvironment   map[string]string `json:"image_environment"`
}

func hash(body []byte) string { value := sha256.Sum256(body); return hex.EncodeToString(value[:]) }
func ValidSource(sha string) bool {
	return shaPattern.MatchString(sha) && sha != strings.Repeat("0", 40)
}
func validDigest(digest string) bool {
	return digestPattern.MatchString(digest) && digest != "sha256:"+strings.Repeat("0", 64)
}
func PinnedImage(value string) bool {
	parts := strings.Split(value, "@")
	return len(parts) == 2 && repositoryPattern.MatchString(parts[0]) && validDigest(parts[1])
}
func ValidateBases(builder, runtime string, published bool) error {
	for _, value := range []string{builder, runtime} {
		parts := strings.Split(value, "@")
		if len(parts) > 2 || !repositoryPattern.MatchString(parts[0]) || (len(parts) == 2 && !validDigest(parts[1])) {
			return errors.New("invalid release base image")
		}
		if published && !PinnedImage(value) {
			return errors.New("published image requires pinned builder and runtime")
		}
	}
	return nil
}

// Relative paths and every existing ancestor must stay inside the supplied root.
// Real deployment .env/Secret content is intentionally not part of the input list.
func SafePath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, "\\:") {
		return "", errors.New("invalid release path")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("release path escapes root")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	current := absolute
	for _, segment := range strings.Split(filepath.ToSlash(clean), "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("release symlink rejected")
		}
	}
	return filepath.Join(absolute, clean), nil
}
func read(root, path string) ([]byte, error) {
	absolute, err := SafePath(root, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, errors.New("release input is not a bounded regular file")
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1024*1024 {
		return nil, errors.New("release input exceeds bound")
	}
	return body, nil
}
func strict(body []byte, target interface{}) error {
	// Reject duplicate JSON object keys before struct decoding, including nested objects.
	scanner := json.NewDecoder(bytes.NewReader(body))
	var scan func() error
	scan = func() error {
		token, err := scanner.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for scanner.More() {
				key, err := scanner.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate release JSON key")
				}
				seen[name] = true
				if err := scan(); err != nil {
					return err
				}
			}
		case '[':
			for scanner.More() {
				if err := scan(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected release JSON delimiter")
		}
		_, err = scanner.Token()
		return err
	}
	if err := scan(); err != nil {
		return err
	}
	if _, err := scanner.Token(); err != io.EOF {
		return errors.New("trailing release JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func Load(root string) (Catalog, string, error) {
	var catalog Catalog
	body, err := read(root, CatalogPath)
	if err != nil {
		return catalog, "", err
	}
	if err = strict(body, &catalog); err != nil {
		return catalog, "", err
	}
	if catalog.Version != 1 || len(catalog.Include) < 1 || len(catalog.Include) > 64 {
		return catalog, "", errors.New("invalid release catalog")
	}
	names, envs := map[string]bool{}, map[string]bool{}
	for _, item := range catalog.Include {
		expected := strings.ToUpper(strings.ReplaceAll(item.Service, "-", "_")) + "_IMAGE"
		if !namePattern.MatchString(item.Service) || names[item.Service] || item.ImageEnv != expected || envs[item.ImageEnv] {
			return catalog, "", errors.New("invalid or duplicate release component")
		}
		if item.Package != "" && item.Package != "./cmd/"+item.Service {
			return catalog, "", errors.New("release package must match its command directory")
		}
		expectedContext := "."
		if item.Service == "frontend" {
			expectedContext = "web"
		}
		if item.Context != expectedContext {
			return catalog, "", errors.New("invalid release build context")
		}
		if _, err := SafePath(root, item.Context); err != nil {
			return catalog, "", err
		}
		if _, err := read(root, item.Dockerfile); err != nil {
			return catalog, "", err
		}
		names[item.Service] = true
		envs[item.ImageEnv] = true
	}
	return catalog, hash(body), nil
}
func NewImage(root string, receipt ImageReceipt) (ImageReceipt, error) {
	catalog, digest, err := Load(root)
	if err != nil {
		return receipt, err
	}
	found := false
	for _, item := range catalog.Include {
		found = found || item.Service == receipt.Service
	}
	if !found || !ValidSource(receipt.SourceSHA) || !repositoryPattern.MatchString(receipt.Repository) || !strings.HasSuffix(receipt.Repository, "/"+receipt.Service) || !validDigest(receipt.Digest) {
		return receipt, errors.New("invalid release image identity")
	}
	// Published receipts require reviewed immutable base images. Candidate OCI
	// builds may use compatibility tags, and cannot become a published manifest.
	if err := ValidateBases(receipt.BuilderImage, receipt.RuntimeImage, receipt.Published); err != nil {
		return receipt, err
	}
	if receipt.CatalogSHA256 != "" && receipt.CatalogSHA256 != digest {
		return receipt, errors.New("release catalog mismatch")
	}
	receipt.CatalogSHA256 = digest
	return receipt, nil
}
func NewCheck(root, kind, source, evidence string) (CheckReceipt, error) {
	known := false
	for _, required := range requiredChecks {
		known = known || required == kind
	}
	if !known || !ValidSource(source) {
		return CheckReceipt{}, errors.New("invalid release check identity")
	}
	body, err := read(root, evidence)
	if err != nil {
		return CheckReceipt{}, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return CheckReceipt{}, errors.New("empty release evidence")
	}
	return CheckReceipt{kind, source, true, hash(body)}, nil
}
func exactFiles(root, directory string, names []string) error {
	path, err := SafePath(root, directory)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	expected := map[string]bool{}
	for _, name := range names {
		expected[name+".json"] = true
	}
	if len(entries) != len(expected) {
		return errors.New("incomplete or extra release receipts")
	}
	for _, entry := range entries {
		if !expected[entry.Name()] || entry.IsDir() {
			return errors.New("unexpected release receipt")
		}
	}
	return nil
}
func Assemble(root, source, images, checks string) (Manifest, error) {
	catalog, digest, err := Load(root)
	if err != nil {
		return Manifest{}, err
	}
	if !ValidSource(source) {
		return Manifest{}, errors.New("invalid source SHA")
	}
	names := []string{}
	for _, item := range catalog.Include {
		names = append(names, item.Service)
	}
	if err := exactFiles(root, images, names); err != nil {
		return Manifest{}, err
	}
	if err := exactFiles(root, checks, RequiredChecks()); err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{Version: 1, SourceSHA: source, CatalogSHA256: digest, ImagesPublished: true, ImageEnvironment: map[string]string{}}
	var firstPublished *bool
	for _, item := range catalog.Include {
		body, err := read(root, filepath.ToSlash(filepath.Join(images, item.Service+".json")))
		if err != nil {
			return Manifest{}, err
		}
		var receipt ImageReceipt
		if err := strict(body, &receipt); err != nil {
			return Manifest{}, err
		}
		if receipt.SourceSHA != source || receipt.CatalogSHA256 != digest || receipt.Service != item.Service {
			return Manifest{}, errors.New("release image source or catalog mismatch")
		}
		if _, err := NewImage(root, receipt); err != nil {
			return Manifest{}, err
		}
		if firstPublished == nil {
			value := receipt.Published
			firstPublished = &value
		} else if *firstPublished != receipt.Published {
			return Manifest{}, errors.New("mixed candidate and published images")
		}
		manifest.Images = append(manifest.Images, receipt)
		manifest.ImagesPublished = manifest.ImagesPublished && receipt.Published
		manifest.ImageEnvironment[item.ImageEnv] = receipt.Repository + "@" + receipt.Digest
	}
	for _, kind := range requiredChecks {
		body, err := read(root, filepath.ToSlash(filepath.Join(checks, kind+".json")))
		if err != nil {
			return Manifest{}, err
		}
		var receipt CheckReceipt
		if err := strict(body, &receipt); err != nil {
			return Manifest{}, err
		}
		if receipt.Kind != kind || receipt.SourceSHA != source || !receipt.Passed || !validDigest("sha256:"+receipt.EvidenceSHA256) {
			return Manifest{}, errors.New("invalid release check receipt")
		}
		manifest.Checks = append(manifest.Checks, receipt)
	}
	manifest.Inputs, err = snapshotInputs(root, catalog)
	if err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
func snapshotInputs(root string, catalog Catalog) ([]FileDigest, error) {
	inputs, err := inputPaths(root, catalog)
	if err != nil {
		return nil, err
	}
	sort.Strings(inputs)
	previous := ""
	result := []FileDigest{}
	for _, path := range inputs {
		if path == previous {
			continue
		}
		previous = path
		body, err := read(root, path)
		if err != nil {
			return nil, fmt.Errorf("release input %s unavailable", path)
		}
		result = append(result, FileDigest{path, hash(body)})
	}
	return result, nil
}

// Verify rechecks artifact identities and every current configuration/migration
// hash. Trust in unsigned CI receipts must be established separately.
func Verify(root, path string, requirePublished bool) (Manifest, error) {
	body, err := read(root, path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := strict(body, &manifest); err != nil {
		return Manifest{}, err
	}
	catalog, digest, err := Load(root)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.Version != 1 || !ValidSource(manifest.SourceSHA) || manifest.CatalogSHA256 != digest || manifest.DeploymentAccepted || len(manifest.Images) != len(catalog.Include) || len(manifest.Checks) != len(requiredChecks) {
		return Manifest{}, errors.New("invalid release manifest identity")
	}
	if requirePublished && !manifest.ImagesPublished {
		return Manifest{}, errors.New("candidate images are not published")
	}
	environment := map[string]string{}
	for index, item := range catalog.Include {
		receipt := manifest.Images[index]
		if receipt.Service != item.Service || receipt.SourceSHA != manifest.SourceSHA || receipt.CatalogSHA256 != digest || receipt.Published != manifest.ImagesPublished {
			return Manifest{}, errors.New("manifest image mismatch")
		}
		if _, err := NewImage(root, receipt); err != nil {
			return Manifest{}, err
		}
		environment[item.ImageEnv] = receipt.Repository + "@" + receipt.Digest
	}
	for index, kind := range requiredChecks {
		receipt := manifest.Checks[index]
		if receipt.Kind != kind || receipt.SourceSHA != manifest.SourceSHA || !receipt.Passed || !validDigest("sha256:"+receipt.EvidenceSHA256) {
			return Manifest{}, errors.New("manifest check mismatch")
		}
	}
	inputs, err := snapshotInputs(root, catalog)
	if err != nil {
		return Manifest{}, err
	}
	if !reflect.DeepEqual(inputs, manifest.Inputs) || !reflect.DeepEqual(environment, manifest.ImageEnvironment) {
		return Manifest{}, errors.New("release configuration or migration drift")
	}
	return manifest, nil
}
func inputPaths(root string, catalog Catalog) ([]string, error) {
	inputs := []string{CatalogPath, ".github/workflows/ci-cd.yaml", "Makefile", ".dockerignore", "go.mod", "go.sum", "web/package.json", "web/package-lock.json", "web/nginx.conf", "web/.dockerignore", "mobile/pubspec.yaml", "mobile/pubspec.lock", "mobile/analysis_options.yaml", "docker-compose.yaml", "docker-compose.production.yaml", "deploy/compose/schema-migrate.yaml", "deploy/compose/production.env.example", "deploy/acceptance/production-launch-checks.json", "scripts/validate-production-compose.ps1", "scripts/test-production-compose.ps1", "internal/infrastructure/schema/catalog.go"}
	for _, item := range catalog.Include {
		inputs = append(inputs, item.Dockerfile)
	}
	migrations, err := filepath.Glob(filepath.Join(root, "deploy/mysql/migrations/*.sql"))
	if err != nil || len(migrations) > 64 {
		return nil, errors.New("invalid release migrations")
	}
	for _, path := range migrations {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, filepath.ToSlash(relative))
	}
	return inputs, nil
}
func Write(root, path string, value interface{}) error {
	absolute, err := SafePath(root, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	// Exclusive output avoids silently replacing a previously reviewed release.
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}
