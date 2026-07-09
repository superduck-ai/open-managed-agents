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
