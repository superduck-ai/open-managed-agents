package environments

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	"go.yaml.in/yaml/v3"
)

const repositorySkillScanScript = `import json,pathlib,sys
result=[]
for root in json.load(sys.stdin):
    repo=pathlib.Path(root)
    base=repo/'.claude'/'skills'
    if not base.is_dir():
        continue
    for child in sorted(base.iterdir()):
        manifest=child/'SKILL.md'
        if not child.is_dir() or not manifest.is_file():
            continue
        if not manifest.resolve().is_relative_to(repo.resolve()):
            continue
        try:
            with manifest.open(encoding='utf-8') as handle:
                if handle.readline().rstrip('\r\n')!='---':
                    continue
                lines=[]
                for line in handle:
                    if line.rstrip('\r\n')=='---':
                        result.append({'path':str(manifest),'frontmatter':''.join(lines)})
                        break
                    lines.append(line)
        except UnicodeError:
            continue
print(json.dumps(result,ensure_ascii=False))
`

func (r *Runner) RepositorySkills(ctx context.Context, code db.CodeSession, roots []string) ([]codesessions.RepositorySkill, error) {
	_, sandbox, err := r.EnsureSessionSandbox(ctx, code)
	if err != nil {
		return nil, err
	}
	if sandbox.ProviderSandboxID == nil || *sandbox.ProviderSandboxID == "" {
		return nil, errRepositorySkillScan
	}
	input, err := json.Marshal(roots)
	if err != nil {
		return nil, err
	}
	result, err := r.provider.RunCommand(ctx, *sandbox.ProviderSandboxID, e2bruntime.CommandRequest{Command: "python3 -c " + shellQuote(repositorySkillScanScript), Stdin: input, Timeout: 30 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("scan repository Skill catalog: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, errRepositorySkillScan
	}
	return parseRepositorySkillScan(result.Stdout, roots)
}

func parseRepositorySkillScan(raw []byte, roots []string) ([]codesessions.RepositorySkill, error) {
	var rows []struct {
		Path        string `json:"path"`
		Frontmatter string `json:"frontmatter"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	result := make([]codesessions.RepositorySkill, 0, len(rows))
	for _, row := range rows {
		if !repositorySkillPath(row.Path, roots) {
			return nil, errRepositorySkillScan
		}
		var metadata struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal([]byte(row.Frontmatter), &metadata); err != nil {
			continue
		}
		metadata.Name = strings.TrimSpace(metadata.Name)
		metadata.Description = strings.TrimSpace(metadata.Description)
		if metadata.Name == "" || metadata.Description == "" {
			continue
		}
		result = append(result, codesessions.RepositorySkill{Name: metadata.Name, Description: metadata.Description, Path: row.Path})
	}
	return result, nil
}

func repositorySkillPath(file string, roots []string) bool {
	if !path.IsAbs(file) || path.Clean(file) != file {
		return false
	}
	for _, root := range roots {
		relative, found := strings.CutPrefix(file, path.Join(root, ".claude/skills")+"/")
		if !found {
			continue
		}
		parts := strings.Split(relative, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] == "SKILL.md" {
			return true
		}
	}
	return false
}
