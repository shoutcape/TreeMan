package database

func setupBranchDB(backend Backend, worktreePath, branch, envKey, configuredContainer string) (SetupResult, error) {
	return setupDatabase(backend, SetupOptions{
		WorktreePath:        worktreePath,
		Branch:              branch,
		EnvKey:              envKey,
		ConfiguredContainer: configuredContainer,
	})
}
