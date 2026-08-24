package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParsedURI holds the components of a parsed postgres connection URI.
type ParsedURI struct {
	// Database is the database name extracted from the URI path.
	Database string
	// BaseURI is the URI without the database path or query string,
	// preserving original casing and percent-encoding.
	BaseURI string
	// Query is the raw query string (without the leading '?'), if any.
	Query string
	// Port is the port number from the URI host, if present.
	Port string
	// Host is the hostname from the URI.
	Host string
	// User is the PostgreSQL user from the URI, defaulting to postgres.
	User string
}

// ParseURI extracts the database name, base URI, and query string from a
// postgres connection URI. It supports the "postgres" and "postgresql" schemes.
func ParseURI(uri string) (ParsedURI, error) {
	if uri == "" {
		return ParsedURI{}, fmt.Errorf("empty URI")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return ParsedURI{}, fmt.Errorf("invalid URI: %w", err)
	}

	if !strings.EqualFold(parsed.Scheme, "postgres") && !strings.EqualFold(parsed.Scheme, "postgresql") {
		return ParsedURI{}, fmt.Errorf("unsupported scheme %q: only postgres:// and postgresql:// are supported", parsed.Scheme)
	}

	// Extract database name from path (strip leading slash)
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" || strings.Contains(dbName, "/") {
		return ParsedURI{}, fmt.Errorf("no database name in URI path")
	}

	// Reconstruct BaseURI manually to preserve original percent-encoding.
	// We rebuild from the raw URI string by finding the path start.
	baseURI := reconstructBaseURI(uri, parsed.Scheme)

	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	user := "postgres"
	if parsed.User != nil && parsed.User.Username() != "" {
		user = parsed.User.Username()
	}

	return ParsedURI{
		Database: dbName,
		BaseURI:  baseURI,
		Query:    parsed.RawQuery,
		Port:     port,
		Host:     parsed.Hostname(),
		User:     user,
	}, nil
}

// reconstructBaseURI extracts the base URI (scheme + userinfo + host) from
// the raw URI string, preserving original percent-encoding.
func reconstructBaseURI(rawURI, scheme string) string {
	// Find the authority start (after "scheme://") while retaining the exact
	// original scheme casing and percent-encoding.
	schemeEnd := strings.Index(rawURI, "://")
	if schemeEnd < 0 {
		return rawURI
	}
	schemePrefix := rawURI[:schemeEnd+3]
	afterScheme := rawURI[schemeEnd+3:]

	// Find the path start (first '/' after the authority).
	// The authority is everything before the first unescaped '/'.
	slashIdx := strings.Index(afterScheme, "/")
	if slashIdx < 0 {
		// No path - return everything
		// Strip query string if present
		qIdx := strings.Index(afterScheme, "?")
		if qIdx >= 0 {
			return schemePrefix + afterScheme[:qIdx]
		}
		return rawURI
	}

	return schemePrefix + afterScheme[:slashIdx]
}

// BranchDBName derives a branch-specific database name without repository
// scoping. New callers should use BranchDBNameForRepository.
func BranchDBName(originalDB, branch string) string {
	return BranchDBNameForRepository(originalDB, branch, "")
}

// BranchDBNameForRepository derives a collision-resistant PostgreSQL
// identifier. The readable prefixes are capped safely and the suffix is a
// stable hash of the repository, source database, and full branch name.
func BranchDBNameForRepository(originalDB, branch, repositoryID string) string {
	const maxIdentifierBytes = 63
	const hashLength = 12

	base := sanitizeDatabasePart(originalDB)
	if base == "" {
		base = "database"
	}
	slug := sanitizeDatabasePart(branch)
	if slug == "" {
		slug = "branch"
	}
	sum := sha256.Sum256([]byte(repositoryID + "\x00" + originalDB + "\x00" + branch))
	hash := hex.EncodeToString(sum[:])[:hashLength]

	// Reserve separators and hash, then split the remaining readable budget.
	readableBudget := maxIdentifierBytes - len("__") - len("_") - hashLength
	baseBudget := min(24, readableBudget/2)
	base = truncateUTF8(base, baseBudget)
	slug = truncateUTF8(slug, readableBudget-len(base))
	if slug == "" {
		slug = "branch"
		slug = truncateUTF8(slug, readableBudget-len(base))
	}
	return base + "__" + slug + "_" + hash
}

// truncateUTF8 keeps PostgreSQL's byte limit without corrupting a rune.
func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := 0
	for end < len(value) {
		_, size := utf8.DecodeRuneInString(value[end:])
		if end+size > maxBytes {
			break
		}
		end += size
	}
	return value[:end]
}

func sanitizeDatabasePart(value string) string {
	var result strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			result.WriteRune(r)
			lastUnderscore = r == '_'
			continue
		}
		if !lastUnderscore {
			result.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(result.String(), "_")
}

// ReplaceDatabase returns a new URI with the database name replaced,
// preserving query parameters and original percent-encoding.
func ReplaceDatabase(uri, newDB string) (string, error) {
	parsed, err := ParseURI(uri)
	if err != nil {
		return "", err
	}

	result := parsed.BaseURI + "/" + url.PathEscape(newDB)
	if parsed.Query != "" {
		result += "?" + parsed.Query
	}

	return result, nil
}
