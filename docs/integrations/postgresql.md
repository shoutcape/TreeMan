# Branch Databases

TreeMan can make one PostgreSQL database for each new worktree. This feature is optional.

## Configure the Database Key

Put this text in `.treeman.toml`.

```toml
[database]
env_key = "DATABASE_URI"
container = "project-postgres-1" # optional
```

Put a PostgreSQL URI in a root-level environment file.

```text
DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp
```

TreeMan accepts `postgres://` and `postgresql://` URIs. It skips other URI types. `container` is optional, but required for remote database hosts, unexposed ports, and ambiguous local Docker environments.

## Create a Branch Database

TreeMan copies environment files before database setup. It reads the configured key from the copied `.env` file.

It makes a readable, collision-resistant database name:

```text
<base-prefix>__<branch-prefix>_<repository-and-branch-hash>
```

TreeMan normalizes readable prefixes and reserves a stable hash of the repository and full branch name. It keeps names within PostgreSQL's 63-byte identifier limit without splitting UTF-8 characters.

TreeMan takes one snapshot of running Docker containers. It uses the configured `container` exactly when set. Otherwise, it requires exactly one PostgreSQL-image container publishing the URI port on `localhost`, `127.0.0.1`, or `::1`. It refuses ambiguous or remote targets instead of guessing.

TreeMan runs `psql` through `docker exec` against the `postgres` maintenance database. Docker access, a PostgreSQL client inside the container, and a user permitted to create and drop databases are required.

If a setup must be retried, its original host, effective port, user, and configured container must remain unchanged. Changing only the source database name is allowed: TreeMan replaces it with the owned branch database.

## Verify a Branch Database

`treeman setup` reads the ownership record and acts on its state:

| Ownership state | Action |
| --- | --- |
| No record | TreeMan creates the branch database |
| Setup pending | TreeMan retries the exact recorded target |
| Active | TreeMan verifies the database and uses it again |
| Pending cleanup | TreeMan refuses and keeps the cleanup state |

For an active record, TreeMan checks the branch, the worktree path, the host,
the port, the user, and the configured container. It resolves the exact
recorded container ID. It never selects a replacement container by name or by
port. It then asks the container whether the database exists.

TreeMan does not create the database again, drop it, rewrite the environment
file, or change the ownership state.

- A missing owned database is an error. Restore the database, or delete the
  worktree to release the record.
- An environment file that names a different database is an error. TreeMan
  keeps the file.

`treeman setup` is not a database reset.

## Delete a Branch Database

TreeMan records a database ownership record in Git's common directory before setup completes. The record pins the exact Docker container ID selected during setup. It reads that record before removing the worktree, marks it pending only after both worktree and branch deletion succeed, then drops the recorded database. If that exact container is no longer running, cleanup fails safely rather than selecting a replacement by name or port.

TreeMan never authorizes deletion from the current `.env` value. Older branch databases without an ownership record are preserved with a warning and require manual cleanup.

## Failure Rules

Database actions are warning-only. If environment rewriting fails, TreeMan immediately attempts to drop the newly created database. If that fails, it retains a pending ownership record for recovery.

After Git deletion, failed drops remain pending and a later `treeman clean` retries them. Use `docker exec` and `psql` only when recovering a legacy database without ownership metadata.

> [!warning]
> TreeMan safely escapes branch database names before using them as PostgreSQL identifiers.
