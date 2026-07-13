# 命名一致性门禁

## 目标

仓库通过语言原生惯例、独立 lint 命令、pre-commit 和 CI 对标识符命名进行确定性检查，使新代码在不依赖人工评审记忆的情况下保持一致。外部 API 和数据库字段仍按兼容合同命名，但例外被限制在边界层。

## Go

- package 使用简短小写名；导出标识符使用 PascalCase，未导出标识符使用 mixedCaps。
- 常见缩写保持一致的大写形式，例如 `API`、`HTTP`、`ID`、`URL` 和 `UUID`。
- `.golangci.yml` 启用 `revive` 的 `var-naming` 规则；现有 `just lint`、pre-commit Go package 检查和 GitHub `Lint` workflow 共用该配置。

## TypeScript 与 React

`web/eslint.naming.config.js` 只承载命名规则，使命名门禁不会被其他 ESLint 基线问题掩盖：

- 变量使用 camelCase；React 组件引用和其他构造器值允许 PascalCase；真正的模块常量允许 UPPER_CASE。
- 函数使用 camelCase，React 函数组件允许 PascalCase。
- 类型、接口、类和泛型类型参数使用 PascalCase。
- 普通参数使用 camelCase；组件或构造器引用参数允许 PascalCase。
- 类属性和方法使用 camelCase。

对象属性、类型属性和解构名称不强制改写，因为这些位置承载 Anthropic/API、数据库或第三方合同中的 `snake_case`。内部业务标识符不在此例外内。

`bun run lint:naming` 是独立入口；`just web-lint-naming`、pre-commit 的 `typescript-naming` hook 和 `.github/workflows/web-naming.yml` 调用同一命令。

## 验收

```bash
just lint
just web-lint-naming
just hooks-run
```
