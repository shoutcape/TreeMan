package database

import (
	"errors"
	"fmt"
)

type cleanupPlan struct {
	store       *databaseStore
	worktreeID  string
	record      DatabaseRecord
	containerID string
}

type CleanupBatch struct {
	backend  Backend
	resolver ContainerResolver
	plans    []*cleanupPlan
}

func NewCleanupBatch() *CleanupBatch                { return newCleanupBatch(defaultBackend()) }
func newCleanupBatch(backend Backend) *CleanupBatch { return &CleanupBatch{backend: backend} }

// Prepare returns an opaque commit function. Call it only after the worktree
// and its SHA-guarded branch have both been deleted.
func (b *CleanupBatch) Prepare(worktreePath string) (func() error, error) {
	store, id, err := databaseStoreForWorktree(worktreePath)
	if err != nil {
		return nil, err
	}
	record, err := store.cleanupRecord(id)
	if err != nil || record == nil {
		return nil, err
	}
	if b.resolver == nil {
		b.resolver, err = b.backend.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("listing PostgreSQL containers: %w", err)
		}
	}
	target, err := b.resolver.ResolveID(record.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("finding recorded postgres container: %w", err)
	}
	plan := &cleanupPlan{store: store, worktreeID: id, record: *record, containerID: target.ID}
	committed := false
	return func() error {
		if committed {
			return fmt.Errorf("prepared database cleanup already committed")
		}
		current, err := plan.store.markPendingCleanup(plan.worktreeID, plan.record)
		if err != nil {
			return err
		}
		plan.record = *current
		b.plans = append(b.plans, plan)
		committed = true
		return nil
	}, nil
}

func (b *CleanupBatch) Flush() error {
	plans := b.plans
	b.plans = nil
	_, err := executeCleanupBatch(b.backend, plans)
	return err
}

func executeCleanupBatch(backend Backend, plans []*cleanupPlan) (int, error) {
	type group struct {
		container, user string
		plans           []*cleanupPlan
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
		if err := backend.Drop(group.container, group.user, names); err != nil {
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
