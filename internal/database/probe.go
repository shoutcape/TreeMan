package database

import "fmt"

// ProbeResult reports whether a branch database can be created without
// changing the environment file, database ownership state, or container.
type ProbeResult struct {
	Skipped bool
}

type setupTarget struct {
	uri     string
	parsed  ParsedURI
	skipped bool
}

// Probe checks the configured database URI and its target container without
// creating a database or updating any files.
func Probe(worktreePath, envKey, configuredContainer string) (ProbeResult, error) {
	return probe(defaultBackend(), worktreePath, envKey, configuredContainer)
}

func probe(backend Backend, worktreePath, envKey, configuredContainer string) (ProbeResult, error) {
	target, err := loadSetupTarget(worktreePath, envKey)
	if err != nil || target.skipped {
		return ProbeResult{Skipped: target.skipped}, err
	}

	resolver, err := backend.Snapshot()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("listing PostgreSQL containers: %w", err)
	}
	if _, err := resolveTarget(resolver, target.parsed, configuredContainer); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{}, nil
}

func loadSetupTarget(worktreePath, envKey string) (setupTarget, error) {
	if envKey == "" {
		return setupTarget{skipped: true}, nil
	}
	uri, err := ReadEnvValue(worktreePath, envKey)
	if err != nil {
		return setupTarget{}, fmt.Errorf("reading %s: %w", envKey, err)
	}
	return parseSetupTarget(uri, envKey)
}

func parseSetupTarget(uri, envKey string) (setupTarget, error) {
	if uri == "" || !isPostgresURI(uri) {
		return setupTarget{skipped: true}, nil
	}
	parsed, err := ParseURI(uri)
	if err != nil {
		return setupTarget{}, fmt.Errorf("parsing %s: %w", envKey, err)
	}
	return setupTarget{uri: uri, parsed: parsed}, nil
}

func resolveTarget(resolver ContainerResolver, parsed ParsedURI, configuredContainer string) (ContainerTarget, error) {
	target, err := resolver.Resolve(parsed.Host, parsed.Port, configuredContainer)
	if err != nil {
		return ContainerTarget{}, fmt.Errorf("finding postgres container: %w", err)
	}
	return target, nil
}
