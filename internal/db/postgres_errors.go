package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const postgresUniqueViolationCode = "23505"

func postgresError(err error) (*pgconn.PgError, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil, false
	}
	return pgErr, true
}

func isUniqueViolation(err error) bool {
	pgErr, ok := postgresError(err)
	return ok && pgErr.Code == postgresUniqueViolationCode
}

func isUniqueViolationOnConstraint(err error, constraintName string) bool {
	pgErr, ok := postgresError(err)
	return ok &&
		pgErr.Code == postgresUniqueViolationCode &&
		pgErr.ConstraintName == constraintName
}

const (
	postgresUndefinedTableCode  = "42P01"
	postgresUndefinedColumnCode = "42703"
)

// isUndefinedRelationError reports whether err is a PostgreSQL error
// indicating that a table or column referenced by a console query is missing
// from the current schema. Console handlers treat this as "no data yet" (for
// reads) or a no-op (for writes like RemoveOrgUser) so a drifted dev database
// degrades gracefully instead of 500-ing the whole console workspace context.
func isUndefinedRelationError(err error) bool {
	pgErr, ok := postgresError(err)
	if !ok {
		return false
	}
	return pgErr.Code == postgresUndefinedTableCode || pgErr.Code == postgresUndefinedColumnCode
}
