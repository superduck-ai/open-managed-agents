### Development Environment (5 criteria)

**Repository Scope:**
- **devcontainer** (Level 2, Repo): Dev container configured – .devcontainer/devcontainer.json with Node.js & TS extensions (TS) or Python image with pip/poetry (Py)
- **env_template** (Level 1, Repo): Environment template – .env.example file exists or environment variables are documented in README/AGENTS.md. Without knowing required env vars, agents cannot run the application locally. Absolute blocker.
- **local_services_setup** (Level 2, Repo) [Skippable]: Local services setup – docker-compose.yml for local dependencies (Postgres, Redis, etc.) or clear documentation on how to run them. Agents need these services to run integration tests and develop features. Skip for apps without external service dependencies.
- **devcontainer_runnable** (Level 3, Repo) [Skippable]: Devcontainer runnable – The devcontainer can be built and run successfully using the devcontainer CLI or VS Code. Validates that the containerized development environment actually works, not just that config files exist. Skip if devcontainer CLI is not installed.

**Application Scope:**
- **database_schema** (Level 2, App) [Skippable]: Database schema – Schema definition files exist for databases (Prisma schema, TypeORM entities, SQLAlchemy models, raw SQL schemas). Agents need to understand the data model to make correct changes. Skip for apps without databases.
