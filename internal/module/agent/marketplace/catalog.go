package marketplace

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	CatalogContractVersion   = "agent.extension_marketplace.v1"
	PublisherContractVersion = "agent.extension_publisher.v1"
	ManifestContractVersion  = "agent.extension_manifest.v1"
	ReleaseContractVersion   = "agent.extension_release.v1"

	KindSkill     = "skill"
	KindMCPServer = "mcp_server"

	PublisherVerified  = "verified"
	PublisherSuspended = "suspended"

	KeyActive  = "active"
	KeyRetired = "retired"
	KeyRevoked = "revoked"

	ReleasePublished = "published"
	ReleaseWithdrawn = "withdrawn"

	SignatureAlgorithmEd25519 = "ed25519"

	PermissionNetwork             = "network"
	PermissionReadUserData        = "user_data_read"
	PermissionWriteUserData       = "user_data_write"
	PermissionExternalWrite       = "external_write"
	PermissionCredentialReference = "credential_reference"

	DefaultPageSize = 20
	MaxPageSize     = 50

	maxCursorBytes             = 1024
	maxSearchRunes             = 120
	maxPublisherNameRunes      = 120
	maxDisplayNameRunes        = 160
	maxDescriptionRunes        = 1000
	maxCapabilityIDs           = 32
	maxRequestedPermissions    = 16
	maxPublisherSigningKeys    = 8
	maximumStableIdentifierLen = 128
)

var (
	ErrCatalogDisabled       = errors.New("agent extension marketplace is disabled")
	ErrInvalidQuery          = errors.New("invalid extension marketplace query")
	ErrInvalidCursor         = errors.New("invalid extension marketplace cursor")
	ErrInvalidPublisher      = errors.New("invalid extension marketplace publisher")
	ErrInvalidManifest       = errors.New("invalid extension marketplace manifest")
	ErrInvalidRelease        = errors.New("invalid extension marketplace release")
	ErrSignatureVerification = errors.New("extension marketplace signature verification failed")

	stableIDPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{1,126}[a-z0-9])?$`)
	keyIDPattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._:-]{0,126}[A-Za-z0-9])?$`)
	semverPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	hexDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// SigningKey stores public verification material only. Private signing keys
// are intentionally outside the service and repository trust boundary.
type SigningKey struct {
	KeyID           string
	Algorithm       string
	PublicKeyBase64 string
	Status          string
}

// Publisher is an immutable verified identity snapshot. Key rotation is
// represented by additional keys; revoked keys invalidate their releases.
type Publisher struct {
	ContractVersion string
	PublisherID     string
	DisplayName     string
	Verification    string
	SigningKeys     []SigningKey
	VerifiedAt      time.Time
}

// Manifest is the canonical, signed package declaration. Permissions are
// declarations for future installation review, never execution grants.
type Manifest struct {
	ContractVersion      string
	PackageID            string
	Kind                 string
	Version              string
	PublisherID          string
	DisplayName          string
	Description          string
	ArtifactDigestSHA256 string
	CapabilityIDs        []string
	RequestedPermissions []string
}

// SignedRelease binds an immutable manifest to a publisher key. PublishedAt
// and lifecycle status are registry facts and are not part of package bytes.
type SignedRelease struct {
	ContractVersion string
	ReleaseID       string
	Manifest        Manifest
	SignatureKeyID  string
	SignatureBase64 string
	Status          string
	PublishedAt     time.Time
}

type PublisherSummary struct {
	PublisherID  string
	DisplayName  string
	Verification string
}

// Listing is the credential-free, signature-verified public projection.
type Listing struct {
	ContractVersion      string
	ReleaseID            string
	PackageID            string
	Kind                 string
	Version              string
	DisplayName          string
	Description          string
	Publisher            PublisherSummary
	ArtifactDigestSHA256 string
	SignatureKeyID       string
	CapabilityIDs        []string
	RequestedPermissions []string
	PublishedAt          time.Time
	SignatureVerified    bool
}

type Query struct {
	Kind        string
	PublisherID string
	Search      string
	AfterCursor string
	PageSize    int
}

type CursorPosition struct {
	PublishedAt time.Time
	ReleaseID   string
}

// ListRequest is normalized once by the domain layer and consumed by stores.
type ListRequest struct {
	Kind        string
	PublisherID string
	Search      string
	After       *CursorPosition
	PageSize    int
	filterHash  string
}

type Page struct {
	ContractVersion string
	Releases        []Listing
	NextCursor      string
	HasMore         bool
}

// CatalogStore is read-only from the product service. Trusted publication and
// publisher administration use a separate future control-plane boundary.
type CatalogStore interface {
	ListPublished(context.Context, ListRequest) ([]SignedRelease, bool, error)
	GetPublishers(context.Context, []string) (map[string]Publisher, error)
}

type canonicalManifest struct {
	ContractVersion      string   `json:"contract_version"`
	PackageID            string   `json:"package_id"`
	Kind                 string   `json:"kind"`
	Version              string   `json:"version"`
	PublisherID          string   `json:"publisher_id"`
	DisplayName          string   `json:"display_name"`
	Description          string   `json:"description,omitempty"`
	ArtifactDigestSHA256 string   `json:"artifact_digest_sha256"`
	CapabilityIDs        []string `json:"capability_ids"`
	RequestedPermissions []string `json:"requested_permissions"`
}

type catalogCursor struct {
	Version     int    `json:"v"`
	FilterHash  string `json:"filter_sha256"`
	PublishedAt string `json:"published_at"`
	ReleaseID   string `json:"release_id"`
}

func NormalizePublisher(publisher Publisher) (Publisher, error) {
	publisher.ContractVersion = strings.TrimSpace(publisher.ContractVersion)
	publisher.PublisherID = strings.ToLower(strings.TrimSpace(publisher.PublisherID))
	publisher.DisplayName = strings.TrimSpace(publisher.DisplayName)
	publisher.Verification = strings.ToLower(strings.TrimSpace(publisher.Verification))
	publisher.VerifiedAt = publisher.VerifiedAt.UTC()
	if publisher.ContractVersion != PublisherContractVersion || !validStableID(publisher.PublisherID) {
		return Publisher{}, fmt.Errorf("%w: identity is incomplete", ErrInvalidPublisher)
	}
	if publisher.DisplayName == "" || utf8.RuneCountInString(publisher.DisplayName) > maxPublisherNameRunes {
		return Publisher{}, fmt.Errorf("%w: display name is invalid", ErrInvalidPublisher)
	}
	if publisher.Verification != PublisherVerified && publisher.Verification != PublisherSuspended {
		return Publisher{}, fmt.Errorf("%w: unsupported verification state", ErrInvalidPublisher)
	}
	if publisher.VerifiedAt.IsZero() || len(publisher.SigningKeys) == 0 || len(publisher.SigningKeys) > maxPublisherSigningKeys {
		return Publisher{}, fmt.Errorf("%w: verification evidence is incomplete", ErrInvalidPublisher)
	}
	keys := make([]SigningKey, 0, len(publisher.SigningKeys))
	seen := make(map[string]struct{}, len(publisher.SigningKeys))
	for _, key := range publisher.SigningKeys {
		key, err := NormalizeSigningKey(key)
		if err != nil {
			return Publisher{}, err
		}
		if _, exists := seen[key.KeyID]; exists {
			return Publisher{}, fmt.Errorf("%w: duplicate signing key", ErrInvalidPublisher)
		}
		seen[key.KeyID] = struct{}{}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].KeyID < keys[j].KeyID })
	publisher.SigningKeys = keys
	return publisher, nil
}

// NormalizeSigningKey validates public verification material without ever
// accepting private signing material into the marketplace trust boundary.
func NormalizeSigningKey(key SigningKey) (SigningKey, error) {
	key.KeyID = strings.TrimSpace(key.KeyID)
	key.Algorithm = strings.ToLower(strings.TrimSpace(key.Algorithm))
	key.PublicKeyBase64 = strings.TrimSpace(key.PublicKeyBase64)
	key.Status = strings.ToLower(strings.TrimSpace(key.Status))
	if !keyIDPattern.MatchString(key.KeyID) || key.Algorithm != SignatureAlgorithmEd25519 {
		return SigningKey{}, fmt.Errorf("%w: signing key identity is invalid", ErrInvalidPublisher)
	}
	if key.Status != KeyActive && key.Status != KeyRetired && key.Status != KeyRevoked {
		return SigningKey{}, fmt.Errorf("%w: signing key status is invalid", ErrInvalidPublisher)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(key.PublicKeyBase64)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return SigningKey{}, fmt.Errorf("%w: signing public key is invalid", ErrInvalidPublisher)
	}
	return key, nil
}

func NormalizeManifest(manifest Manifest) (Manifest, error) {
	manifest.ContractVersion = strings.TrimSpace(manifest.ContractVersion)
	manifest.PackageID = strings.ToLower(strings.TrimSpace(manifest.PackageID))
	manifest.Kind = strings.ToLower(strings.TrimSpace(manifest.Kind))
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.PublisherID = strings.ToLower(strings.TrimSpace(manifest.PublisherID))
	manifest.DisplayName = strings.TrimSpace(manifest.DisplayName)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.ArtifactDigestSHA256 = strings.ToLower(strings.TrimSpace(manifest.ArtifactDigestSHA256))
	if manifest.ContractVersion != ManifestContractVersion || !validStableID(manifest.PackageID) ||
		!validStableID(manifest.PublisherID) {
		return Manifest{}, fmt.Errorf("%w: identity is incomplete", ErrInvalidManifest)
	}
	if manifest.Kind != KindSkill && manifest.Kind != KindMCPServer {
		return Manifest{}, fmt.Errorf("%w: unsupported package kind", ErrInvalidManifest)
	}
	if !semverPattern.MatchString(manifest.Version) {
		return Manifest{}, fmt.Errorf("%w: version must be SemVer", ErrInvalidManifest)
	}
	if manifest.DisplayName == "" || utf8.RuneCountInString(manifest.DisplayName) > maxDisplayNameRunes ||
		utf8.RuneCountInString(manifest.Description) > maxDescriptionRunes {
		return Manifest{}, fmt.Errorf("%w: package text is invalid", ErrInvalidManifest)
	}
	if !hexDigestPattern.MatchString(manifest.ArtifactDigestSHA256) {
		return Manifest{}, fmt.Errorf("%w: artifact digest is invalid", ErrInvalidManifest)
	}
	capabilities, err := normalizedStableValues(manifest.CapabilityIDs, maxCapabilityIDs)
	if err != nil || len(capabilities) == 0 {
		return Manifest{}, fmt.Errorf("%w: capability declarations are invalid", ErrInvalidManifest)
	}
	permissions, err := normalizedPermissions(manifest.RequestedPermissions)
	if err != nil {
		return Manifest{}, err
	}
	manifest.CapabilityIDs = capabilities
	manifest.RequestedPermissions = permissions
	return manifest, nil
}

func CanonicalManifestPayload(manifest Manifest) ([]byte, error) {
	normalized, err := NormalizeManifest(manifest)
	if err != nil {
		return nil, err
	}
	return json.Marshal(canonicalManifest{
		ContractVersion: normalized.ContractVersion, PackageID: normalized.PackageID,
		Kind: normalized.Kind, Version: normalized.Version, PublisherID: normalized.PublisherID,
		DisplayName: normalized.DisplayName, Description: normalized.Description,
		ArtifactDigestSHA256: normalized.ArtifactDigestSHA256,
		CapabilityIDs:        normalized.CapabilityIDs, RequestedPermissions: normalized.RequestedPermissions,
	})
}

// SignManifest is intended for trusted offline tooling and tests. The private
// key is consumed transiently and is never represented by repository records.
func SignManifest(manifest Manifest, keyID string, privateKey ed25519.PrivateKey, publishedAt time.Time) (SignedRelease, error) {
	keyID = strings.TrimSpace(keyID)
	if !keyIDPattern.MatchString(keyID) || len(privateKey) != ed25519.PrivateKeySize || publishedAt.IsZero() {
		return SignedRelease{}, fmt.Errorf("%w: signing input is incomplete", ErrInvalidRelease)
	}
	normalized, err := NormalizeManifest(manifest)
	if err != nil {
		return SignedRelease{}, err
	}
	payload, err := CanonicalManifestPayload(normalized)
	if err != nil {
		return SignedRelease{}, err
	}
	return SignedRelease{
		ContractVersion: ReleaseContractVersion,
		ReleaseID:       StableReleaseID(normalized.PublisherID, normalized.PackageID, normalized.Version),
		Manifest:        normalized,
		SignatureKeyID:  keyID,
		SignatureBase64: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)),
		Status:          ReleasePublished,
		PublishedAt:     publishedAt.UTC(),
	}, nil
}

func VerifyRelease(publisher Publisher, release SignedRelease) (Listing, error) {
	normalizedPublisher, err := NormalizePublisher(publisher)
	if err != nil {
		return Listing{}, err
	}
	if normalizedPublisher.Verification != PublisherVerified {
		return Listing{}, fmt.Errorf("%w: publisher is not active", ErrSignatureVerification)
	}
	normalizedManifest, err := NormalizeManifest(release.Manifest)
	if err != nil {
		return Listing{}, err
	}
	release.ContractVersion = strings.TrimSpace(release.ContractVersion)
	release.ReleaseID = strings.TrimSpace(release.ReleaseID)
	release.SignatureKeyID = strings.TrimSpace(release.SignatureKeyID)
	release.SignatureBase64 = strings.TrimSpace(release.SignatureBase64)
	release.Status = strings.ToLower(strings.TrimSpace(release.Status))
	release.PublishedAt = release.PublishedAt.UTC()
	if release.ContractVersion != ReleaseContractVersion || release.Status != ReleasePublished || release.PublishedAt.IsZero() ||
		release.ReleaseID != StableReleaseID(normalizedManifest.PublisherID, normalizedManifest.PackageID, normalizedManifest.Version) {
		return Listing{}, fmt.Errorf("%w: release identity is invalid", ErrInvalidRelease)
	}
	if normalizedManifest.PublisherID != normalizedPublisher.PublisherID {
		return Listing{}, fmt.Errorf("%w: publisher identity mismatch", ErrSignatureVerification)
	}
	key, found := publisherSigningKey(normalizedPublisher.SigningKeys, release.SignatureKeyID)
	if !found || key.Status == KeyRevoked {
		return Listing{}, fmt.Errorf("%w: signing key is unavailable", ErrSignatureVerification)
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(key.PublicKeyBase64)
	if err != nil {
		return Listing{}, fmt.Errorf("%w: public key cannot be decoded", ErrSignatureVerification)
	}
	signature, err := base64.RawStdEncoding.DecodeString(release.SignatureBase64)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Listing{}, fmt.Errorf("%w: signature cannot be decoded", ErrSignatureVerification)
	}
	payload, err := CanonicalManifestPayload(normalizedManifest)
	if err != nil {
		return Listing{}, err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return Listing{}, ErrSignatureVerification
	}
	return Listing{
		ContractVersion: CatalogContractVersion,
		ReleaseID:       release.ReleaseID, PackageID: normalizedManifest.PackageID,
		Kind: normalizedManifest.Kind, Version: normalizedManifest.Version,
		DisplayName: normalizedManifest.DisplayName, Description: normalizedManifest.Description,
		Publisher: PublisherSummary{
			PublisherID: normalizedPublisher.PublisherID, DisplayName: normalizedPublisher.DisplayName,
			Verification: normalizedPublisher.Verification,
		},
		ArtifactDigestSHA256: normalizedManifest.ArtifactDigestSHA256,
		SignatureKeyID:       release.SignatureKeyID,
		CapabilityIDs:        append([]string(nil), normalizedManifest.CapabilityIDs...),
		RequestedPermissions: append([]string(nil), normalizedManifest.RequestedPermissions...),
		PublishedAt:          release.PublishedAt, SignatureVerified: true,
	}, nil
}

// VerifyNewRelease is stricter than public read verification: retired keys
// keep historical releases valid but cannot sign a newly published version.
func VerifyNewRelease(publisher Publisher, release SignedRelease) (Listing, error) {
	listing, err := VerifyRelease(publisher, release)
	if err != nil {
		return Listing{}, err
	}
	normalizedPublisher, err := NormalizePublisher(publisher)
	if err != nil {
		return Listing{}, err
	}
	key, found := publisherSigningKey(normalizedPublisher.SigningKeys, release.SignatureKeyID)
	if !found || key.Status != KeyActive {
		return Listing{}, fmt.Errorf("%w: new releases require an active signing key", ErrSignatureVerification)
	}
	return listing, nil
}

func PrepareList(query Query, configuredPageSize int) (ListRequest, error) {
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	query.PublisherID = strings.ToLower(strings.TrimSpace(query.PublisherID))
	query.Search = strings.ToLower(strings.TrimSpace(query.Search))
	query.AfterCursor = strings.TrimSpace(query.AfterCursor)
	if query.Kind != "" && query.Kind != KindSkill && query.Kind != KindMCPServer {
		return ListRequest{}, fmt.Errorf("%w: unsupported kind", ErrInvalidQuery)
	}
	if query.PublisherID != "" && !validStableID(query.PublisherID) {
		return ListRequest{}, fmt.Errorf("%w: publisher_id is invalid", ErrInvalidQuery)
	}
	if utf8.RuneCountInString(query.Search) > maxSearchRunes || hasControlCharacters(query.Search) {
		return ListRequest{}, fmt.Errorf("%w: search is invalid", ErrInvalidQuery)
	}
	if len(query.AfterCursor) > maxCursorBytes {
		return ListRequest{}, fmt.Errorf("%w: cursor is too large", ErrInvalidCursor)
	}
	if configuredPageSize < 1 || configuredPageSize > MaxPageSize {
		configuredPageSize = DefaultPageSize
	}
	if query.PageSize == 0 {
		query.PageSize = configuredPageSize
	}
	if query.PageSize < 1 || query.PageSize > configuredPageSize || query.PageSize > MaxPageSize {
		return ListRequest{}, fmt.Errorf("%w: page_size must be within 1..%d", ErrInvalidQuery, configuredPageSize)
	}
	request := ListRequest{
		Kind: query.Kind, PublisherID: query.PublisherID, Search: query.Search,
		PageSize: query.PageSize, filterHash: marketplaceFilterHash(query.Kind, query.PublisherID, query.Search),
	}
	if query.AfterCursor != "" {
		cursor, err := decodeCursor(query.AfterCursor)
		if err != nil {
			return ListRequest{}, err
		}
		if cursor.FilterHash != request.filterHash {
			return ListRequest{}, fmt.Errorf("%w: cursor filters do not match request", ErrInvalidCursor)
		}
		publishedAt, err := time.Parse(time.RFC3339Nano, cursor.PublishedAt)
		if err != nil || publishedAt.IsZero() || strings.TrimSpace(cursor.ReleaseID) == "" {
			return ListRequest{}, fmt.Errorf("%w: malformed cursor", ErrInvalidCursor)
		}
		request.After = &CursorPosition{PublishedAt: publishedAt.UTC(), ReleaseID: cursor.ReleaseID}
	}
	return request, nil
}

func BuildPage(request ListRequest, releases []Listing, hasMore bool) (Page, error) {
	if request.PageSize < 1 || request.PageSize > MaxPageSize || len(releases) > request.PageSize {
		return Page{}, fmt.Errorf("%w: store returned an invalid page", ErrInvalidRelease)
	}
	items := make([]Listing, len(releases))
	for index, release := range releases {
		if release.ContractVersion != CatalogContractVersion || !release.SignatureVerified ||
			release.ReleaseID == "" || release.PublishedAt.IsZero() {
			return Page{}, fmt.Errorf("%w: listing is not verified", ErrInvalidRelease)
		}
		items[index] = cloneListing(release)
		if index > 0 && listingComesAfter(items[index], items[index-1]) {
			return Page{}, fmt.Errorf("%w: store page order is unstable", ErrInvalidRelease)
		}
	}
	if hasMore && len(items) == 0 {
		return Page{}, fmt.Errorf("%w: empty page cannot have more results", ErrInvalidRelease)
	}
	nextCursor := ""
	var err error
	if hasMore {
		last := items[len(items)-1]
		nextCursor, err = encodeCursor(catalogCursor{
			Version: 1, FilterHash: request.filterHash,
			PublishedAt: last.PublishedAt.UTC().Format(time.RFC3339Nano), ReleaseID: last.ReleaseID,
		})
		if err != nil {
			return Page{}, err
		}
	}
	return Page{
		ContractVersion: CatalogContractVersion,
		Releases:        items, NextCursor: nextCursor, HasMore: hasMore,
	}, nil
}

func StableReleaseID(publisherID, packageID, version string) string {
	payload := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(publisherID)),
		strings.ToLower(strings.TrimSpace(packageID)), strings.TrimSpace(version),
	}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return "release_" + hex.EncodeToString(digest[:16])
}

func publisherSigningKey(keys []SigningKey, keyID string) (SigningKey, bool) {
	for _, key := range keys {
		if key.KeyID == keyID {
			return key, true
		}
	}
	return SigningKey{}, false
}

func normalizedStableValues(values []string, maximum int) ([]string, error) {
	if len(values) > maximum {
		return nil, errors.New("too many values")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !validStableID(value) {
			return nil, errors.New("invalid stable value")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizedPermissions(values []string) ([]string, error) {
	if len(values) > maxRequestedPermissions {
		return nil, fmt.Errorf("%w: too many permission declarations", ErrInvalidManifest)
	}
	allowed := map[string]struct{}{
		PermissionNetwork: {}, PermissionReadUserData: {}, PermissionWriteUserData: {},
		PermissionExternalWrite: {}, PermissionCredentialReference: {},
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("%w: unsupported permission %q", ErrInvalidManifest, value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func validStableID(value string) bool {
	return len(value) <= maximumStableIdentifierLen && stableIDPattern.MatchString(value)
}

func marketplaceFilterHash(kind, publisherID, search string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{kind, publisherID, search}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func encodeCursor(cursor catalogCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode marketplace cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(raw string) (catalogCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return catalogCursor{}, fmt.Errorf("%w: malformed cursor", ErrInvalidCursor)
	}
	var cursor catalogCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 ||
		!hexDigestPattern.MatchString(cursor.FilterHash) || strings.TrimSpace(cursor.ReleaseID) == "" {
		return catalogCursor{}, fmt.Errorf("%w: malformed cursor", ErrInvalidCursor)
	}
	return cursor, nil
}

func cloneListing(listing Listing) Listing {
	clone := listing
	clone.CapabilityIDs = append([]string(nil), listing.CapabilityIDs...)
	clone.RequestedPermissions = append([]string(nil), listing.RequestedPermissions...)
	return clone
}

func listingComesAfter(current, previous Listing) bool {
	if current.PublishedAt.Before(previous.PublishedAt) {
		return false
	}
	if current.PublishedAt.After(previous.PublishedAt) {
		return true
	}
	return current.ReleaseID < previous.ReleaseID
}

func hasControlCharacters(value string) bool {
	for _, char := range value {
		if char < 0x20 && char != '\t' {
			return true
		}
	}
	return false
}
