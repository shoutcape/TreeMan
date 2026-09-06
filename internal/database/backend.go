package database

type Backend interface {
	Snapshot() (ContainerResolver, error)
	Create(container, user, database string) error
	Drop(container, user string, databases []string) error
	// Exists reports whether a database is present. Reuse is verified with
	// it, so a rerun can report a missing owned database instead of
	// recreating one the user may have dropped on purpose.
	Exists(container, user, database string) (bool, error)
}

type dockerBackend struct{}

func (dockerBackend) Snapshot() (ContainerResolver, error) { return NewContainerResolver() }
func (dockerBackend) Create(container, user, database string) error {
	return CreateDatabase(container, user, database)
}
func (dockerBackend) Drop(container, user string, databases []string) error {
	return DropDatabases(container, user, databases)
}
func (dockerBackend) Exists(container, user, database string) (bool, error) {
	return DatabaseExists(container, user, database)
}
func defaultBackend() Backend { return dockerBackend{} }
