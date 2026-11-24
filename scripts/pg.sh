#!/usr/bin/env bash
set -euo pipefail

if ! command -v psql >/dev/null; then
  echo "psql is required to run just pg." >&2
  exit 1
fi

pg_config=.run/config.yaml
if [[ -f .run/env.json ]]; then
  pg_config=.run/generated/config.yaml
fi

{
  IFS= read -r -d '' pg_connection || exit 1
  if IFS= read -r -d '' pg_password; then
    export PGPASSWORD="$pg_password"
  fi
} < <(bun -e '
const configFile = process.argv[1];

function fail(message) {
  console.error(message);
  process.exit(1);
}

let databaseURL;
try {
  const config = Bun.YAML.parse(await Bun.file(configFile).text());
  const value = config?.database?.url;
  if (typeof value !== "string" || !value.trim()) {
    fail(`Set database.url in ${configFile} before running just pg.`);
  }
  databaseURL = value.trim();
} catch {
  fail(`Could not read ${configFile}.`);
}

let connection;
let password;
try {
  connection = new URL(databaseURL);
  if (connection.protocol !== "postgresql:" && connection.protocol !== "postgres:") {
    fail(`Set a PostgreSQL URL in ${configFile}.`);
  }
  password = connection.searchParams.get("password") ??
    (connection.password ? decodeURIComponent(connection.password) : undefined);
} catch {
  fail(`Invalid PostgreSQL URL in ${configFile}.`);
}
connection.password = "";
connection.searchParams.delete("password");

const searchPath = connection.searchParams.get("search_path");
connection.searchParams.delete("search_path");
const options = ["-csearch_path=public"];
const configuredOptions = connection.searchParams.get("options");
if (configuredOptions) options.push(configuredOptions);
if (searchPath !== null) options.push(`-csearch_path=${searchPath.replace(/[\\\s]/g, "\\$&")}`);
connection.searchParams.set("options", options.join(" "));
connection.search = connection.searchParams.toString().replaceAll("+", "%20");

const fields = [connection.href];
if (password !== undefined) fields.push(password);
process.stdout.write(fields.join("\0") + "\0");
' "$pg_config")

exec psql -X --dbname "$pg_connection" "$@"
