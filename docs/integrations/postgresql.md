# Branch Databases

TreeMan can make one PostgreSQL database for each new worktree. This feature is optional.

## Configure the Database Key

Put this text in `.treeman.toml`.

```toml
[database]
env_key = "DATABASE_URI"
```

Put a PostgreSQL URI in a root-level environment file.

```text
DATABASE_URI=postgres://postgres:postgres@127.0.0.1:5432/myapp
```

TreeMan accepts `postgres://` and `postgresql://` URIs. It skips other URI types.

## Create a Branch Database

TreeMan copies environment files before database setup. It reads the configured key from the copied `.env` file.

It makes this database name:

```text
<base-database>__<branch-slug>
```

For database names, TreeMan changes `/`, `-`, and `.` to `_`. PostgreSQL names have a 63-character limit. TreeMan truncates longer names.

Two long branch names can produce the same truncated database name.

TreeMan finds a running PostgreSQL container in this order:

1. A container that publishes the URI port.
2. A container from the `postgres` image.
3. A container with `postgres` in its image name.

TreeMan runs `psql` through `docker exec`. Docker access and a PostgreSQL client inside the container are required.

## Delete a Branch Database

TreeMan reads and prepares the worktree database target before it removes the worktree. It drops the database only after it removes both the worktree and branch, and only when its name contains `__`.

This check helps protect the base database. It does not prove that the database belongs to the worktree.

## Failure Rules

Database actions are warning-only. TreeMan can create a database and then fail to rewrite the environment file. This leaves an orphan database.

Use `docker exec` and `psql` to remove an orphan database after you verify its name.

> [!warning]
> TreeMan safely escapes branch database names before using them as PostgreSQL identifiers.
