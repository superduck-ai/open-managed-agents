package openobserve

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

//go:embed queries.json
var queriesJSON []byte

type queryPack struct {
	Backend string      `json:"backend"`
	Queries []packQuery `json:"queries"`
}

type packQuery struct {
	QueryRef   string     `json:"query_ref"`
	StreamType streamType `json:"stream_type"`
	SQL        string     `json:"sql"`
}

var packedQueries map[string]packQuery

func init() {
	var pack queryPack
	if err := json.Unmarshal(queriesJSON, &pack); err != nil {
		panic("observability/openobserve: parse queries.json: " + err.Error())
	}
	packedQueries = make(map[string]packQuery, len(pack.Queries))
	for _, query := range pack.Queries {
		if query.QueryRef == "" {
			panic("observability/openobserve: queries.json query_ref is empty")
		}
		if query.StreamType != streamTraces && query.StreamType != streamMetrics && query.StreamType != streamLogs {
			panic("observability/openobserve: invalid stream_type for " + query.QueryRef)
		}
		if _, exists := packedQueries[query.QueryRef]; exists {
			panic("observability/openobserve: duplicate query_ref " + query.QueryRef)
		}
		packedQueries[query.QueryRef] = query
	}
	core := map[string]struct{}{}
	for _, ref := range observability.QueryRefs() {
		core[ref] = struct{}{}
	}
	if len(core) != len(packedQueries) {
		panic(fmt.Sprintf("observability/openobserve: query_ref count mismatch core=%d pack=%d", len(core), len(packedQueries)))
	}
	for ref := range packedQueries {
		if _, ok := core[ref]; !ok {
			panic("observability/openobserve: pack query_ref not declared in panels.json: " + ref)
		}
	}
	for ref := range core {
		if _, ok := packedQueries[ref]; !ok {
			panic("observability/openobserve: panels.json query_ref missing from pack: " + ref)
		}
	}
}

func lookupQuery(queryRef string) (packQuery, bool) {
	query, ok := packedQueries[queryRef]
	return query, ok
}
