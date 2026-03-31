package database

import (
	"fmt"
	"net/url"
	"strings"
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

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return ParsedURI{}, fmt.Errorf("unsupported scheme %q: only postgres:// and postgresql:// are supported", parsed.Scheme)
	}

	// Extract database name from path (strip leading slash)
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return ParsedURI{}, fmt.Errorf("no database name in URI path")
	}

	// Reconstruct BaseURI manually to preserve original percent-encoding.
	// We rebuild from the raw URI string by finding the path start.
	baseURI := reconstructBaseURI(uri, parsed.Scheme)

	return ParsedURI{
		Database: dbName,
		BaseURI:  baseURI,
		Query:    parsed.RawQuery,
		Port:     parsed.Port(),
	}, nil
}

// reconstructBaseURI extracts the base URI (scheme + userinfo + host) from
// the raw URI string, preserving original percent-encoding.
func reconstructBaseURI(rawURI, scheme string) string {
	// Find the authority start (after "scheme://")
	schemePrefix := scheme + "://"
	afterScheme := rawURI[len(schemePrefix):]

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

// BranchDBName derives a branch-specific database name from the original
// database name and a git branch name. The format is "<original>__<branch_slug>"
// where the branch slug has slashes, hyphens, and dots replaced with underscores.
// The result is truncated to 63 characters (PostgreSQL's max identifier length).
func BranchDBName(originalDB, branch string) string {
	slug := sanitizeBranchSlug(branch)
	name := originalDB + "__" + slug

	if len(name) > 63 {
		name = name[:63]
	}

	return name
}

// sanitizeBranchSlug replaces slashes, hyphens, and dots with underscores.
func sanitizeBranchSlug(branch string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"-", "_",
		".", "_",
	)
	return replacer.Replace(branch)
}

// ReplaceDatabase returns a new URI with the database name replaced,
// preserving query parameters and original percent-encoding.
func ReplaceDatabase(uri, newDB string) (string, error) {
	parsed, err := ParseURI(uri)
	if err != nil {
		return "", err
	}

	result := parsed.BaseURI + "/" + newDB
	if parsed.Query != "" {
		result += "?" + parsed.Query
	}

	return result, nil
}
