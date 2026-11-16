package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SkillCatalogMapper -sql ./skill_catalog_mapper.xml -out ./skill_catalog_mapper.sqlmap.gen.go -dialect postgres

type CatalogSkill struct {
	ExternalID    string    `db:"external_id"`
	DisplayTitle  string    `db:"display_title"`
	IconKey       string    `db:"icon_key"`
	Description   string    `db:"description"`
	LatestVersion string    `db:"latest_version"`
	Source        string    `db:"source"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}
type SkillCatalogQuery struct {
	WorkspaceUUID string
	Query         string
	Source        string
	Sort          string
	Limit         int
	Offset        int
}
type SkillCatalogCounts struct {
	All     int `db:"all_count"`
	Builtin int `db:"builtin_count"`
	Custom  int `db:"custom_count"`
}
type SkillCatalogMapper interface {
	List(ctx context.Context, query SkillCatalogQuery) ([]CatalogSkill, error)
	Counts(ctx context.Context, query SkillCatalogQuery) (SkillCatalogCounts, error)
}
