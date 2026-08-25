package database

type Backend interface {
	Snapshot() (ContainerResolver, error)
	Create(container, user, database string) error
	Drop(container, user string, databases []string) error
}

type dockerBackend struct{}

func (dockerBackend) Snapshot() (ContainerResolver, error) { return NewContainerResolver() }
func (dockerBackend) Create(container, user, database string) error {
	return CreateDatabase(container, user, database)
}
func (dockerBackend) Drop(container, user string, databases []string) error {
	return DropDatabases(container, user, databases)
}
func defaultBackend() Backend { return dockerBackend{} }
