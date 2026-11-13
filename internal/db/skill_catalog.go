package db

import "context"

func (d *DB) SkillCatalog(ctx context.Context, query SkillCatalogQuery) ([]CatalogSkill, SkillCatalogCounts, error) {
	mapper := NewSkillCatalogMapper(d.mapperDB)
	counts, err := mapper.Counts(ctx, query)
	if err != nil {
		return nil, counts, err
	}
	rows, err := mapper.List(ctx, query)
	return rows, counts, err
}
