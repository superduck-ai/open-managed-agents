module github.com/superduck-ai/open-managed-agents

go 1.27.0

ignore ./web/node_modules

tool github.com/superduck-ai/yourbatis/cmd/sqlmapgen

replace (
	github.com/riverqueue/river => github.com/superduck-ai/river v0.46.0-oma-v0.0.1
	github.com/riverqueue/river/riverdriver => github.com/superduck-ai/river/riverdriver v0.46.0-oma-v0.0.1
	github.com/riverqueue/river/riverdriver/riverdatabasesql => github.com/superduck-ai/river/riverdriver/riverdatabasesql v0.46.0-oma-v0.0.1
	github.com/riverqueue/river/riverdriver/riverpgxv5 => github.com/superduck-ai/river/riverdriver/riverpgxv5 v0.46.0-oma-v0.0.1
	github.com/riverqueue/river/rivershared => github.com/superduck-ai/river/rivershared v0.46.0-oma-v0.0.1
	github.com/riverqueue/river/rivertype => github.com/superduck-ai/river/rivertype v0.46.0-oma-v0.0.1
)

require (
	charm.land/fantasy v0.45.1
	github.com/alicebob/miniredis/v2 v2.35.0
	github.com/anthropics/anthropic-sdk-go v1.75.0
	github.com/aws/aws-sdk-go-v2 v1.47.1
	github.com/aws/aws-sdk-go-v2/credentials v1.20.6
	github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager v0.3.4
	github.com/aws/aws-sdk-go-v2/service/s3 v1.113.1
	github.com/aws/smithy-go v1.28.2
	github.com/charmbracelet/crush v0.0.0
	github.com/go-chi/chi/v5 v5.3.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/gosimple/slug v1.15.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/jackc/pgx/v5 v5.10.0
	github.com/klauspost/compress v1.20.0
	github.com/modelcontextprotocol/go-sdk v1.7.1-0.20260825092617-32404652e60b
	github.com/nats-io/nats-server/v2 v2.14.6
	github.com/nats-io/nats.go v1.53.1
	github.com/pressly/goose/v3 v3.28.0
	github.com/redis/go-redis/v9 v9.20.1
	github.com/riverqueue/river v0.46.0
	github.com/riverqueue/river/riverdriver/riverdatabasesql v0.42.0
	github.com/riverqueue/river/rivertype v0.46.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/samber/lo v1.53.0
	github.com/spf13/cobra v1.10.2
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1
	github.com/superduck-ai/e2b-go-sdk v0.0.2
	github.com/superduck-ai/yourbatis v0.1.5
	go.opentelemetry.io/proto/otlp v1.11.0
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/net v0.59.0
	golang.org/x/sync v0.23.0
	golang.org/x/text v0.42.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260911204522-f61a6ca850bd
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	charm.land/catwalk v0.52.49 // indirect
	charm.land/lipgloss/v2 v2.0.6 // indirect
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.24.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/JohannesKaufmann/html-to-markdown v1.6.0 // indirect
	github.com/PuerkitoBio/goquery v1.13.0 // indirect
	github.com/andybalholm/brotli v1.2.3 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.2-default-no-op // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.33.6 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.1 // indirect
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charlievieth/fastwalk v1.0.14 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/openai-go v0.0.0-20260921175203-216db9e71b83 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260811164956-006e29f97886 // indirect
	github.com/charmbracelet/x/ansi v0.11.8 // indirect
	github.com/charmbracelet/x/etag v0.2.0 // indirect
	github.com/charmbracelet/x/exp/charmtone v0.1.0 // indirect
	github.com/charmbracelet/x/exp/slice v0.1.0 // indirect
	github.com/charmbracelet/x/exp/strings v0.1.0 // indirect
	github.com/charmbracelet/x/powernap v0.1.6 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/denisbrodbeck/machineid v1.0.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.9.0 // indirect
	github.com/go-git/go-git/v5 v5.19.2 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.22 // indirect
	github.com/googleapis/gax-go/v2 v2.24.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/itchyny/gojq v0.12.19 // indirect
	github.com/itchyny/timefmt-go v0.1.8 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/kaptinlin/jsonpointer v0.4.28 // indirect
	github.com/kaptinlin/jsonschema v0.9.10 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-sqlite3 v0.35.4 // indirect
	github.com/ncruces/go-sqlite3-wasm/v5 v5.0.35304 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/pjbgf/sha1cd v0.6.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/posthog/posthog-go v1.25.1 // indirect
	github.com/qjebbs/go-jsons v1.0.0-alpha.6 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/riverqueue/river/riverdriver v0.46.0 // indirect
	github.com/riverqueue/river/riverdriver/riverpgxv5 v0.46.0 // indirect
	github.com/riverqueue/river/rivershared v0.46.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/sourcegraph/jsonrpc2 v0.2.2 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/u-root/u-root v0.15.1-0.20251208185023-2f8c7e763cf8 // indirect
	github.com/u-root/uio v0.0.0-20240224005618-d2acac8f3701 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.71.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.71.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.3 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/api v0.298.0 // indirect
	google.golang.org/genai v1.71.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260911204522-f61a6ca850bd // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.58.0 // indirect
	mvdan.cc/sh/v3 v3.14.1 // indirect
	mvdan.cc/sh/x v0.0.0-20260907232430-b5028a3332a4 // indirect
)

replace github.com/charmbracelet/crush => ./third_party/crush
