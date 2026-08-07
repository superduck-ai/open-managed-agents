package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkbenchGeneratedTestCaseMapper -sql ./workbench_generated_test_case_mapper.xml -out ./workbench_generated_test_case_mapper.sqlmap.gen.go -dialect postgres

type workbenchGeneratedTestCaseRow struct {
	UUID       string `db:"uuid"`
	ValuesJSON string `db:"values_json"`
}

type WorkbenchGeneratedTestCaseMapper interface {
	Insert(ctx context.Context, organizationUUID, valuesJSON string) error
	DeleteOlderThanLimit(ctx context.Context, organizationUUID string, limit int) error
	ListForUpdate(ctx context.Context, organizationUUID string) ([]workbenchGeneratedTestCaseRow, error)
	DeleteByUUID(ctx context.Context, testCaseUUID string) error
	DeleteByOrganization(ctx context.Context, organizationUUID string) error
}
