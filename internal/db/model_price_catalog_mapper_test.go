package db

import (
	"github.com/superduck-ai/yourbatis"
	"testing"
)

func TestModelPriceCatalogMapper(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{statement: modelPriceCatalogMapperFindStatement, bound: buildModelPriceCatalogMapperFind(yourbatis.DialectPostgres, "openrouter", []string{"priced"}), wantID: "ModelPriceCatalogMapper.Find", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"source", "model", "model"}, wantSQLFragments: []string{"source=$1"}})
	assertMapperBuilderContract(t, mapperBuilderContract{statement: modelPriceCatalogMapperSaveStatement, bound: buildModelPriceCatalogMapperSave(yourbatis.DialectPostgres, modelPriceCatalogWrite{}), wantID: "ModelPriceCatalogMapper.Save", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"p.Source", "p.Model", "p.Price", "p.Original"}, wantSQLFragments: []string{"CAST($3 AS jsonb)"}})
}
