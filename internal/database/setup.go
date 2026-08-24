package database

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/git"
)

// Function variables let focused tests replace Docker-dependent operations.
var (
	newContainerResolverFn = NewContainerResolver
	createDatabaseFn       = CreateDatabase
	dropDatabasesFn        = DropDatabases
)

// SetupResult holds the outcome of SetupBranchDB.
type SetupResult struct {
	DBName  string
	Skipped bool
}

// SetupBranchDB creates and records a branch-specific database, then rewrites
// the copied .env.
func SetupBranchDB(worktreePath, branch, envKey, configuredContainer string) (SetupResult, error) {
	if envKey == "" {
		return SetupResult{Skipped: true}, nil
	}
	uri, err := ReadDatabaseURI(worktreePath, envKey)
	if err != nil {
		return SetupResult{}, fmt.Errorf("reading %s: %w", envKey, err)
	}
	if uri == "" || !isPostgresURI(uri) {
		return SetupResult{Skipped: true}, nil
	}
	parsed, err := ParseURI(uri)
	if err != nil {
		return SetupResult{}, fmt.Errorf("parsing %s: %w", envKey, err)
	}

	store, worktreeID, err := databaseStoreForWorktree(worktreePath)
	if err != nil {
		return SetupResult{}, err
	}
	record, err := store.setupRecord(worktreeID, branch)
	if err != nil {
		return SetupResult{}, err
	}
	if record != nil && (parsed.Host != record.Host || parsed.Port != record.Port || parsed.User != record.User || configuredContainer != record.Container) {
		return SetupResult{}, fmt.Errorf("database setup target changed since the pending setup; restore host, port, user, and container configuration before retrying")
	}
	resolver, err := newContainerResolverFn()
	if err != nil {
		return SetupResult{}, fmt.Errorf("listing PostgreSQL containers: %w", err)
	}
	if record == nil {
		target, err := resolver.Resolve(parsed.Host, parsed.Port, configuredContainer)
		if err != nil {
			return SetupResult{}, fmt.Errorf("finding postgres container: %w", err)
		}
		candidate := &DatabaseRecord{WorktreeID: worktreeID, WorktreePath: worktreePath, Branch: branch, Database: BranchDBNameForRepository(parsed.Database, branch, store.repoID), Container: configuredContainer, ContainerID: target.ID, Host: parsed.Host, Port: parsed.Port, User: parsed.User, Status: databaseStatusSetupPending}
		record, err = store.beginSetup(candidate)
		if err != nil {
			return SetupResult{}, err
		}
	}
	target, err := resolver.ResolveID(record.ContainerID)
	if err != nil {
		return SetupResult{}, fmt.Errorf("finding recorded postgres container: %w", err)
	}
	// Creation is idempotent, so every retry can safely re-establish the
	// recorded database before changing the worktree environment.
	if err := createDatabaseFn(target.ID, record.User, record.Database); err != nil {
		return SetupResult{}, fmt.Errorf("creating database %q: %w", record.Database, err)
	}
	newURI, err := ReplaceDatabase(uri, record.Database)
	if err != nil {
		return SetupResult{}, fmt.Errorf("building new URI: %w", err)
	}
	if err := RewriteDatabaseURI(worktreePath, envKey, newURI); err != nil {
		if dropErr := dropDatabasesFn(target.ID, record.User, []string{record.Database}); dropErr != nil {
			return SetupResult{}, fmt.Errorf("rewriting .env: %w; rolling back database %q: %v", err, record.Database, dropErr)
		}
		return SetupResult{}, fmt.Errorf("rewriting .env: %w", err)
	}
	// If this persistence step fails, setup_pending remains the durable source
	// of truth. Retrying setup safely reuses the already-created database and
	// rewritten URI before attempting activation again.
	if err := store.activateSetup(worktreeID, *record); err != nil {
		return SetupResult{}, err
	}
	return SetupResult{DBName: record.Database}, nil
}

// cleanupPlan is an owned database target. It never derives deletion authority
// from a mutable environment file.
type cleanupPlan struct {
	store       *databaseStore
	worktreeID  string
	record      DatabaseRecord
	containerID string
}

// CleanupTicket captures an owned database target before its worktree is
// removed. It is valid only for the CleanupSession that created it.
type CleanupTicket struct {
	session *CleanupSession
	plan    *cleanupPlan
}

// CleanupSession batches cleanup for several worktrees against one Docker
// container snapshot. Call Prepare before Git deletion, MarkDeleted after it,
// then Flush after the batch.
type CleanupSession struct {
	resolver ContainerResolver
	plans    []*cleanupPlan
}

func NewCleanupSession() *CleanupSession {
	return &CleanupSession{}
}

// Prepare loads an authorized cleanup target from durable metadata.
func (s *CleanupSession) Prepare(worktreePath string) (*CleanupTicket, error) {
	store, worktreeID, err := databaseStoreForWorktree(worktreePath)
	if err != nil {
		return nil, err
	}
	record, err := store.cleanupRecord(worktreeID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	if s.resolver == nil {
		s.resolver, err = newContainerResolverFn()
		if err != nil {
			return nil, fmt.Errorf("listing PostgreSQL containers: %w", err)
		}
	}
	target, err := s.resolver.ResolveID(record.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("finding recorded postgres container: %w", err)
	}
	return &CleanupTicket{session: s, plan: &cleanupPlan{store: store, worktreeID: worktreeID, record: *record, containerID: target.ID}}, nil
}

// MarkDeleted makes a successfully deleted worktree's database retryable
// before the drop begins.
func (s *CleanupSession) MarkDeleted(ticket *CleanupTicket) error {
	if ticket == nil || ticket.plan == nil {
		return nil
	}
	if ticket.session != s {
		return fmt.Errorf("database cleanup ticket belongs to another session")
	}
	plan := ticket.plan
	record, err := plan.store.markPendingCleanup(plan.worktreeID, plan.record)
	if err != nil {
		return err
	}
	plan.record = *record
	s.plans = append(s.plans, plan)
	return nil
}

// Flush drops queued pending databases and removes their records only after
// Docker reports success. Failed plans remain pending for a later retry.
func (s *CleanupSession) Flush() error {
	plans := s.plans
	s.plans = nil
	_, err := executeCleanupBatch(plans)
	return err
}

// executeCleanupBatch groups already-pending databases by container and user.
func executeCleanupBatch(plans []*cleanupPlan) (int, error) {
	type group struct {
		container string
		user      string
		plans     []*cleanupPlan
	}
	groups := map[string]*group{}
	var errs []error
	removed := 0
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		record, err := plan.store.pendingCleanupRecord(plan.worktreeID, plan.record)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		plan.record = *record
		key := plan.containerID + "\x00" + plan.record.User
		if groups[key] == nil {
			groups[key] = &group{container: plan.containerID, user: plan.record.User}
		}
		groups[key].plans = append(groups[key].plans, plan)
	}
	for _, group := range groups {
		names := make([]string, 0, len(group.plans))
		for _, plan := range group.plans {
			names = append(names, plan.record.Database)
		}
		if err := dropDatabasesFn(group.container, group.user, names); err != nil {
			errs = append(errs, fmt.Errorf("dropping databases in container %q: %w", group.container, err))
			continue
		}
		for _, plan := range group.plans {
			if err := plan.store.removePendingCleanup(plan.worktreeID, plan.record); err != nil {
				errs = append(errs, err)
				continue
			}
			removed++
		}
	}
	return removed, errors.Join(errs...)
}

// RetryPending drops owned databases after a prior Git deletion. Orphaned
// setup and active records are first made retryable under the state lock.
func (s *CleanupSession) RetryPending(mainRoot string) (int, error) {
	store, err := newDatabaseStore(mainRoot)
	if err != nil {
		return 0, err
	}
	records, err := store.records()
	if err != nil {
		return 0, err
	}
	pending := make([]DatabaseRecord, 0, len(records))
	var errs []error
	for _, record := range records {
		if record.Status == databaseStatusPendingCleanup {
			pending = append(pending, record)
			continue
		}
		if record.Status != databaseStatusActive && record.Status != databaseStatusSetupPending {
			continue
		}
		missing, err := worktreeMissing(record.WorktreePath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !missing {
			continue
		}
		branchMissing, err := git.BranchMissing(mainRoot, record.Branch)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !branchMissing {
			continue
		}
		current, err := store.markPendingCleanup(record.WorktreeID, record)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pending = append(pending, *current)
	}
	if len(pending) == 0 {
		return 0, errors.Join(errs...)
	}
	if s.resolver == nil {
		s.resolver, err = newContainerResolverFn()
		if err != nil {
			return 0, errors.Join(append(errs, fmt.Errorf("listing PostgreSQL containers: %w", err))...)
		}
	}
	plans := make([]*cleanupPlan, 0, len(pending))
	for _, record := range pending {
		target, err := s.resolver.ResolveID(record.ContainerID)
		if err != nil {
			errs = append(errs, fmt.Errorf("finding recorded postgres container for pending database %q: %w", record.Database, err))
			continue
		}
		plans = append(plans, &cleanupPlan{
			store:       store,
			worktreeID:  record.WorktreeID,
			record:      record,
			containerID: target.ID,
		})
	}
	removed, cleanupErr := executeCleanupBatch(plans)
	return removed, errors.Join(append(errs, cleanupErr)...)
}

// LegacyBranchDatabase reports an old-style branch database name without
// granting deletion authority. Callers use it to give a manual-cleanup warning.
func LegacyBranchDatabase(worktreePath, envKey string) (string, error) {
	if envKey == "" {
		return "", nil
	}
	uri, err := ReadDatabaseURI(worktreePath, envKey)
	if err != nil || uri == "" || !isPostgresURI(uri) {
		return "", err
	}
	parsed, err := ParseURI(uri)
	if err != nil {
		return "", err
	}
	if strings.Contains(parsed.Database, "__") {
		return parsed.Database, nil
	}
	return "", nil
}

func isPostgresURI(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}

func databaseStoreForWorktree(worktreePath string) (*databaseStore, string, error) {
	store, err := newDatabaseStore(worktreePath)
	if err != nil {
		return nil, "", fmt.Errorf("opening database ownership state: %w", err)
	}
	worktreeID, err := git.WorktreeID(worktreePath)
	if err != nil {
		return nil, "", fmt.Errorf("identifying linked worktree: %w", err)
	}
	return store, worktreeID, nil
}

// worktreeMissing only authorizes orphan cleanup when the path is explicitly
// absent. Permission and I/O failures must not be mistaken for deletion.
func worktreeMissing(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, fmt.Errorf("checking worktree path %q: %w", path, err)
}
