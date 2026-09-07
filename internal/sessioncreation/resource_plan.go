// Package sessioncreation 为直接请求和 Deployment 模板提供共享的 Session 创建计划。
package sessioncreation

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"

	"github.com/samber/lo"
)

// Resource 将规范化的 File 配置与 API 专属预处理生成的 Session Resource 记录关联起来。
type Resource struct {
	Record       db.SessionResource
	FileSpec     *sessionresource.FileSpec
	FileMIMEType string
}

// ResourcePlan 保存由同一组规范资源生成的持久化输入和事件校验绑定。
type ResourcePlan struct {
	Resources         []db.CreateSessionResourceInput
	EventFileBindings []sessioncontract.EventFileBinding
}

// ValidateResources 校验 File 资源总量和挂载路径规则。
func ValidateResources(resources []Resource) error {
	specs := lo.FilterMap(resources, func(resource Resource, _ int) (sessionresource.FileSpec, bool) {
		return lo.FromPtr(resource.FileSpec), resource.FileSpec != nil
	})
	return sessionresource.ValidateFileSpecs(specs)
}

// BuildResourceInput 将单个规范资源转换为数据库写入输入。
func BuildResourceInput(resource Resource) (db.CreateSessionResourceInput, error) {
	input := db.CreateSessionResourceInput{Resource: resource.Record}
	if resource.FileSpec == nil {
		return input, nil
	}
	binding, err := resource.FileSpec.SessionFileBinding(resource.Record.ExternalID)
	if err != nil {
		return db.CreateSessionResourceInput{}, err
	}
	input.FileMount = &db.SessionFileMount{
		ResourceExternalID: binding.ResourceID,
		FileExternalID:     binding.FileID,
		Path:               binding.Path,
	}
	return input, nil
}

// PlanResources 从同一组规范资源生成全部资源写入和文件引用绑定，避免调用方采用不同的挂载规则。
func PlanResources(resources []Resource) (ResourcePlan, error) {
	plan := ResourcePlan{
		Resources:         make([]db.CreateSessionResourceInput, 0, len(resources)),
		EventFileBindings: make([]sessioncontract.EventFileBinding, 0, len(resources)),
	}
	for _, resource := range resources {
		input, err := BuildResourceInput(resource)
		if err != nil {
			return ResourcePlan{}, err
		}
		plan.Resources = append(plan.Resources, input)
		if input.FileMount != nil {
			plan.EventFileBindings = append(plan.EventFileBindings, sessioncontract.EventFileBinding{
				FileID:   input.FileMount.FileExternalID,
				Path:     input.FileMount.Path,
				MimeType: resource.FileMIMEType,
			})
		}
	}
	return plan, nil
}
