package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ModelPriceCatalogMapper -sql ./model_price_catalog_mapper.xml -out ./model_price_catalog_mapper.sqlmap.gen.go -dialect postgres

type modelPriceCatalogRow struct {
	Model string `db:"model"`
	Price []byte `db:"price"`
}
type modelPriceCatalogWrite struct {
	Source   string
	Model    string
	Price    []byte
	Original []byte
}
type ModelPriceCatalogMapper interface {
	Find(ctx context.Context, source string, models []string) ([]modelPriceCatalogRow, error)
	Save(ctx context.Context, p modelPriceCatalogWrite) error
	DeleteSource(ctx context.Context, source string) error
}
