# Brave Search API 与 Exa Search API 调研

> 调研范围：仅使用 Brave Search API 与 Exa 官方文档，核对搜索请求模式、搜索模式与参数、分页、内容获取、结果字段和鉴权方式，并评估当前 internal/websearch 抽象。
>
> 调研日期：2026-07-22。
> 实现状态：provider-neutral 类型已落地到 `internal/websearch/types.go`，并已接入 Tavily 与 Brave。Brave 当前支持 Web Search 的通用请求参数、结果映射和页式分页；Exa 的 provider-specific 参数和内容抓取尚未实现。

## 结论

原有抽象足以表达“输入查询、返回若干标题/链接/正文摘要”的最小 provider-neutral 搜索能力，但不足以无损覆盖 Brave 与 Exa 的主要能力。现已保留 Provider 接口并扩展 SearchRequest、SearchResponse、SearchOptions 和 Result；这些类型覆盖通用交集和未来扩展位，但不试图把两家 provider 的高级协议全部塞进基础接口。

具体判断如下：

- Provider.Search(context.Context, SearchRequest) (SearchResponse, error) 的调用形状适合两家 API：两者都能被适配成一次非流式搜索调用，并返回统一结果列表及响应元数据。
- 当前 SearchOptions 已覆盖 MaxResults、IncludeDomains、ExcludeDomains，以及 Brave 的语言、地区、新鲜度、安全搜索、结果模块、Goggles 和分页等选项；Exa 的 provider-specific 模式和内容选项仍由后续适配器决定。
- 当前 Result 已将摘要与正文分成 Snippet/Text，并支持 PublishedDate、PageAge、ID、Author、Favicon、Highlights、Summary 和 ExtraSnippets 等字段；不能被 provider-neutral 模型表达的 provider-specific 结果类型仍应在适配器中降级或另设能力接口。
- SearchResponse 现在可以表达分页是否还有结果：Brave 返回 query.more_results_available，Exa Search 官方 /search 文档没有公开页码、offset 或 next-cursor 字段；Exa 的 numResults 是单次请求数量，不等同于可遍历分页。
- Brave Web Search 返回的是 description 和可选 extra_snippets，不提供该搜索 endpoint 的通用正文抓取协议；Brave 适配器将 description 映射为 Snippet。Exa 可以在 /search 内嵌 contents，也可以调用独立的 /contents 获取文本、highlights、summary、子页面和链接，后续适配器可分别映射到 Text、Highlights 和 Summary。

因此，当前抽象适合 gateway 的非流式 web search 结果渲染，也已保留 Brave 的分页和字段语义。Exa 后续接入时，应把 provider-specific 的搜索模式、内容请求和综合输出限制在 Exa 适配器或独立能力接口中，不要继续扩大基础 Provider 合同。

## 当前抽象

provider.go 声明：

```go
type Provider interface {
    Search(context.Context, SearchRequest) (SearchResponse, error)
}
```

provider-neutral 的 SearchOptions、Result 和 SearchResponse 定义在 internal/websearch/types.go；Tavily 和 Brave 只负责 provider-specific payload 与字段映射：

```go
type SearchOptions struct {
    MaxResults     int
    IncludeDomains []string
    ExcludeDomains []string
    SearchMode     string
    Country        string
    SearchLanguage string
    UILanguage     string
    Freshness      string
    PageToken      string
    Content        ContentOptions
}

type Result struct {
    ID            string
    Title         string
    URL           string
    Snippet       string
    Text          string
    Highlights    []string
    Summary       string
    Author        string
    Favicon       string
    PublishedDate string
    ExtraSnippets []string
    PageAge       string
}

type SearchResponse struct {
    Results       []Result
    HasMore       bool
    NextPageToken string
    RequestID     string
}
```

NewProvider 当前注册 Tavily 和 Brave；Brave 使用独立的 BraveWebSearchConfig，并将 credential 保留在 OMA 服务端 provider client 中。

## 请求模式与鉴权对比

| 维度 | Brave Search API | Exa Search API | 对抽象的影响 |
| --- | --- | --- | --- |
| Web search endpoint | GET https://api.search.brave.com/res/v1/web/search；官方 API Reference 同时列出 POST 变体 | POST https://api.exa.ai/search | Provider 可统一为一次 Search 调用；Brave 的 GET/POST 选择属于 provider 实现配置 |
| 参数承载 | GET 使用 query parameters；POST 使用 JSON request body；官方文档给出的字段语义一致 | JSON request body，Content-Type: application/json | SearchOptions 不应暴露 HTTP 传输细节 |
| API key | 必须放在 X-Subscription-Token header；官方明确要求不要放在 URL 或源码中 | 官方 Search API 示例使用 x-api-key header；Exa HTTP 文档也列出部分兼容 endpoint 可使用 Authorization: Bearer，但 Search API 的直接示例使用 x-api-key | credential 应留在 provider/client，不进入 SearchOptions |
| 默认搜索形态 | Web Search 面向人类消费的网页结果；无 Exa 那样的 type 搜索模式枚举 | type 可选 auto、fast、instant、deep-lite、deep、deep-reasoning，默认 auto | 需要 provider-neutral 的搜索模式，或保留 provider-specific 扩展 |
| 流式 | Web Search API 以普通 JSON 搜索响应为主；官方 Web Search API Reference 的 POST 变体不是 Exa 的 synthesized-output SSE 模式 | stream: true 可请求 text/event-stream，官方说明主要用于带 outputSchema 的 synthesized output | 当前 Provider 是非流式结果接口，不覆盖流式综合答案；若当前业务只需要结果列表，可明确不纳入该接口 |

来源：

- [Brave Authentication](https://api-dashboard.search.brave.com/documentation/guides/authentication)
- [Brave Web Search 文档](https://api-dashboard.search.brave.com/documentation/services/web-search)
- [Brave Web Search API Reference（GET）](https://api-dashboard.search.brave.com/api-reference/web/search/get)
- [Brave Web Search API Reference（POST）](https://api-dashboard.search.brave.com/api-reference/web/search/post)
- [Exa Search API Reference](https://exa.ai/docs/reference/search)
- [Exa Search API Reference for coding agents](https://exa.ai/docs/reference/search-api-guide-for-coding-agents)
- [Exa HTTP requests](https://exa.ai/docs/.mintlify/skills/build-with-exa/references/http-requests)

## 搜索模式与参数

### Brave

Brave Web Search 没有类似 Exa type 的官方搜索模式参数。搜索策略主要由 q 中的自然语言和搜索操作符控制；官方文档举例包括精确短语、负项、site: 和 filetype:。主要请求参数包括：

| 参数 | 官方语义 | 当前抽象覆盖情况 |
| --- | --- | --- |
| q | 必填查询词；API Reference 说明最多 400 字符、50 个单词 | 由 query string 覆盖 |
| country | 两字符国家代码，指定结果来源国家 | SearchOptions.Country |
| search_lang | 搜索结果语言 | SearchOptions.SearchLanguage |
| ui_lang | 响应中面向用户的 UI 语言 | SearchOptions.UILanguage |
| count | 每页 Web results 数量，最大 20；实际返回数可能更少 | MaxResults 可部分映射 |
| offset | 以“页”为单位的 0-based offset；官方 API Reference 说明与 count 配合分页，指南还建议检查 query.more_results_available | SearchOptions.PageToken |
| safesearch | off、moderate、strict | SearchOptions.SafeSearch |
| spellcheck | 是否对 query 做拼写检查；修改后的 query 出现在响应的 altered 字段 | SearchOptions.Spellcheck |
| freshness | 按页面年龄筛选：pd、pw、pm、py 或自定义日期范围 | SearchOptions.Freshness 或日期范围 |
| extra_snippets | 为每个 Web result 增加最多 5 个额外片段 | Result.ExtraSnippets 保留数组 |
| result_filter | 选择返回的结果模块/类型 | SearchOptions.ResultFilter |
| goggles | 使用 URL 或内联定义进行自定义重排/过滤，可组合多个 | SearchOptions.Goggles |

Brave 官方文档还说明：

- Web result 的核心字段包括 title、url、description；常见附加字段包括 profile、meta_url、page_age、page_fetched、language、family_friendly，具体结果类型还可能带 thumbnail、作者/来源等结构。
- extra_snippets=true 时，result 增加 extra_snippets: string[]。
- query.more_results_available 用于判断是否存在下一页；不应只根据当前结果数量盲目递增 offset。
- Brave 文档把 Local POIs、Local Descriptions、Rich Search 作为其他 endpoint/后续请求，不是 Web Search 的通用网页正文获取接口。

### Exa

Exa Search 使用自然语言/语义 query，并通过 type 选择速度与深度。官方 Search API Reference 定义的主要参数如下：

| 参数 | 官方语义 | 当前抽象覆盖情况 |
| --- | --- | --- |
| query | 必填查询字符串，支持较长的语义描述 | 由 query string 覆盖 |
| type | instant、fast、auto、deep-lite、deep、deep-reasoning | 不覆盖 |
| numResults | 返回数量，公开最大 100；限制随 search type 变化 | MaxResults 可部分映射 |
| category | company、people、publication、news、personal site、financial report 等 | 不覆盖 |
| userLocation | 两字符 ISO 国家代码 | 不覆盖 |
| includeDomains / excludeDomains | 允许域名/路径/通配子域名；官方 Search API Reference 最大 1200 项 | 可直接映射 |
| startPublishedDate / endPublishedDate | ISO 8601 发布日期范围 | 不覆盖 |
| startCrawlDate / endCrawlDate | crawl 日期范围，但当前官方 schema 标记为 deprecated | 不覆盖；不建议新抽象加入 |
| moderation | 过滤不安全内容 | 不覆盖 |
| additionalQueries | 仅 deep-search 类型使用的额外 query 变体，官方 schema 最大 10 项 | 不覆盖 |
| systemPrompt | 指导综合输出或 deep-search 规划 | 不覆盖 |
| outputSchema | 要求综合输出匹配 JSON schema；响应增加 output | 不覆盖 |
| stream | 请求 SSE；官方说明主要用于带 outputSchema 的 synthesized output | 不覆盖 |

Exa 官方文档说明，旧的 context 参数已 deprecated，应优先使用 contents.highlights 或 contents.text。因此 provider-neutral 抽象不应把 context 作为新的核心字段。

来源：

- [Brave Web Search 文档](https://api-dashboard.search.brave.com/documentation/services/web-search)
- [Brave Web Search API Reference（GET）](https://api-dashboard.search.brave.com/api-reference/web/search/get)
- [Brave Web Search API Reference（POST）](https://api-dashboard.search.brave.com/api-reference/web/search/post)
- [Exa Search API Reference](https://exa.ai/docs/reference/search)
- [Exa Search API Reference for coding agents](https://exa.ai/docs/reference/search-api-guide-for-coding-agents)

## 分页

### Brave：显式页号式分页

Brave 使用 count 与 offset：count 是每页数量，offset 是要跳过的页数，0 表示第一页。官方指南给出 count=20&offset=1 的第二页示例，并要求优先读取响应 query.more_results_available 决定是否继续。

统一接口已支持 Brave 的真实分页：

- 请求侧的 PageToken 与 page size；
- 响应侧的 HasMore 和 NextPageToken。

### Exa：单次数量控制，未提供 Search 结果分页合同

Exa Search 官方 SearchRequest 提供 numResults（1–100），但官方 Search API schema 和示例没有定义 offset、page number、next cursor 或 hasMore。Search response 的主要顶层字段是 requestId、results，以及可选的综合输出/成本等字段。

因此不应把 Exa 的 numResults 误当成通用分页参数，也不应在 provider-neutral 层假设两家都支持可遍历分页。若产品需要“更多 Exa 结果”，应先确认 Exa 当前官方合同是否新增分页能力，再设计 opaque cursor；仅凭当前官方 Search 文档不能安全推导出分页实现。

## 内容获取

### Brave：搜索摘要，不是通用正文抓取

Brave Web Search result 提供 description，并可用 extra_snippets=true 获取 extra_snippets: string[]。官方 Web Search 文档把它描述为给网页结果增加上下文片段；未在 Web Search API 文档中定义一个按 result URL 获取通用正文的配套 endpoint。

因此 Brave 适配器把 description 映射到 Snippet，把 extra_snippets 保留为数组；不把它标记为完整 page content，也不把数组压缩成单个字符串。

### Exa：搜索内嵌内容，或独立 Contents API

Exa 支持两种官方方式：

1. 在 /search 请求中使用 contents：
   - text: true 或对象形式返回页面全文，可设置 maxCharacters、HTML tag、verbosity、include/exclude sections；
   - highlights: true 或对象形式返回与 query 相关的关键片段；
   - summary 返回 LLM summary，可指定 query/schema；
   - maxAgeHours 控制缓存内容年龄，0 表示强制新鲜抓取，-1 表示只用缓存；
   - livecrawlTimeout 控制 live crawl 超时；
   - subpages/subpageTarget 获取结果页面的子页面；
   - extras.links/extras.imageLinks 获取页面中的 URL/图片 URL。
2. 调用独立的 POST https://api.exa.ai/contents：请求 ids 或 urls（二选一），最多 100 个；返回与 Search result 相似的内容结果，并提供每个请求项的 statuses，包含 success/error 以及 cached/crawled 来源信息。

这说明 Exa 的“搜索结果”和“内容获取”可以是一次调用，也可以是两阶段调用。当前统一模型已经区分 Snippet、Text、Highlights 和 Summary；Exa 适配器仍需明确哪些内容请求会产生额外计费或延迟。

来源：

- [Brave Web Search 文档](https://api-dashboard.search.brave.com/documentation/services/web-search)
- [Exa Search API Reference](https://exa.ai/docs/reference/search)
- [Exa Contents API Reference](https://exa.ai/docs/reference/get-contents)
- [Exa HTTP requests](https://exa.ai/docs/.mintlify/skills/build-with-exa/references/http-requests)

## 结果字段对比

| 统一语义 | Brave Web result | Exa Search/Contents result | 当前 Result |
| --- | --- | --- | --- |
| 标题 | title | title | Title，足够 |
| 页面 URL | url | url | URL，足够 |
| 稳定/文档 ID | 通用 Web result 不保证与 Exa 相同的 ID 语义；部分本地/垂直结果有自己的 id | id | ID，可选 |
| 短摘要 | description | 可由 highlights、summary 或 text 选项产生 | Snippet |
| 额外摘要 | extra_snippets: string[] | highlights: string[] 与 highlightScores: number[] | ExtraSnippets、Highlights |
| 全文 | Web Search 文档未定义通用全文字段/抓取流程 | text，可出现在 Search 或 Contents response | Text，可选 |
| LLM 摘要 | Web Search result 不以该形式定义 | summary | Summary，可选 |
| 日期元数据 | page_age 表示页面年龄 | publishedDate，ISO 8601 | PageAge 映射 page_age；PublishedDate 仅承载实际发布日期 |
| 作者 | 某些 Brave 垂直结果可能有 profile/来源字段；通用 Web result 不应强行假定 author | author | Author，可选 |
| favicon / 页面元信息 | meta_url.favicon、profile 等 | favicon，以及 image、extras 等 | Favicon，可选 |
| 子页面 | Web Search result 不以通用字段定义 | subpages[]，可带 id、url、title、author、publishedDate、text、highlights 等 | 无 |

Exa 官方示例的 SearchResultOutput 明确展示 title、url、publishedDate、author、id、image、favicon、text、highlights、highlightScores、summary、subpages、extras。Brave 官方 API Reference 则展示 Web result 以及多种垂直结果 schema；通用 Result 结构不能完整覆盖这些联合类型。

## 对现有抽象的建议

### 当前判断

Provider 的最小依赖方向是合理的：调用方只关心查询和 provider-neutral 结果，不需要知道 Brave 的 query string 或 Exa 的 JSON body。对当前 messages gateway 这种“取少量结果后拼成 tool result”的路径，保留 Search 方法是合适的。

Title、URL 和一个明确命名的短文本字段适合作为跨 provider 核心字段。Brave 最大 Web page size 为 20，适配器会裁剪更大的 MaxResults；Exa 接入时应按自身上限处理。IncludeDomains 和 ExcludeDomains 当前仍是通用模型中的预留能力，Brave Web Search 没有同名参数，不应在适配器中伪造无损映射。

### Exa 接入边界

当前模型已经覆盖 Exa 基础结果适配所需的大部分结果字段。后续实现可从 provider-specific 请求开始，不修改基础 Provider 合同：

```go
type ExaSearchOptions struct {
    Type              string
    Category          string
    AdditionalQueries []string
    IncludeContent    ContentOptions
}
```

不过这个示意仍需进一步做语义取舍：

- Brave 的 PageToken 是 provider-neutral opaque continuation 字段在 Brave 上的具体实现；Exa 当前没有对应的分页合同，不应强行生成 token。
- Exa 的 outputSchema、综合 output、SSE synthesized output、subpages 和 extras 不适合硬塞进基础 Result；如果当前 gateway 不需要，应在 Exa 适配器中明确关闭/忽略，而不是伪装成通用字段。
- Brave 的垂直结果、Rich Search、Local POIs 也不应在基础 Web Result 中隐式建模；如未来需要，应单独定义 capability 或专门接口。
- 当前 gateway 已消费 SearchResponse.Results；SearchResponse 的分页和 request ID 元数据为后续调用方保留，不改变现有 Anthropic tool result 合同。

### 最终判断

- 对“最小搜索交集”：足够。
- 对“Brave 基础适配”：已覆盖请求参数、鉴权、结果字段和页式分页。
- 对“Exa 基础单页适配”：现有结果模型基本足够，但请求模式、内容请求和日期/字段降级需要在适配器中写清楚。
- 对“保留两家 API 的主要搜索、分页和内容获取能力”：基础 Provider 合同足以承载结果与元数据；如果要支持 Exa 的完整内容/综合能力，仍需要独立的 content-fetch 或 advanced-search 能力，而不是继续扩大简单搜索接口。

## 官方来源索引

### Brave

- [Authentication](https://api-dashboard.search.brave.com/documentation/guides/authentication)
- [Web Search 文档](https://api-dashboard.search.brave.com/documentation/services/web-search)
- [Web Search API Reference（GET）](https://api-dashboard.search.brave.com/api-reference/web/search/get)
- [Web Search API Reference（POST）](https://api-dashboard.search.brave.com/api-reference/web/search/post)

### Exa

- [Search API Reference](https://exa.ai/docs/reference/search)
- [Search API Reference for coding agents](https://exa.ai/docs/reference/search-api-guide-for-coding-agents)
- [Contents API Reference](https://exa.ai/docs/reference/get-contents)
- [HTTP requests](https://exa.ai/docs/.mintlify/skills/build-with-exa/references/http-requests)
