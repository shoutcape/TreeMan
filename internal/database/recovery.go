package database

import (
	"errors"
	"fmt"
	"github.com/shoutcape/treeman/internal/git"
	"os"
)

func (b *CleanupBatch) RetryPending(mainRoot string) ([]string, error) {
	store, err := newDatabaseStore(mainRoot)
	if err != nil {
		return nil, err
	}
	records, err := store.records()
	if err != nil {
		return nil, err
	}
	var pending []DatabaseRecord
	var errs []error
	for _, record := range records {
		if record.Status == databaseStatusPendingCleanup {
			pending = append(pending, record)
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
		return nil, errors.Join(errs...)
	}
	if b.resolver == nil {
		b.resolver, err = b.backend.Snapshot()
		if err != nil {
			return nil, errors.Join(append(errs, fmt.Errorf("listing PostgreSQL containers: %w", err))...)
		}
	}
	var plans []*cleanupPlan
	for _, record := range pending {
		target, err := b.resolver.ResolveID(record.ContainerID)
		if err != nil {
			errs = append(errs, fmt.Errorf("finding recorded postgres container for pending database %q: %w", record.Database, err))
			continue
		}
		plans = append(plans, &cleanupPlan{store: store, worktreeID: record.WorktreeID, record: record, containerID: target.ID})
	}
	removed, cleanupErr := executeCleanupBatch(b.backend, plans)
	return removed, errors.Join(append(errs, cleanupErr)...)
}

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
