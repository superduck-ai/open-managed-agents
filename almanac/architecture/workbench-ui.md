---
title: "Workbench UI"
summary: "Workbench provides prompt engineering interface with message editor, variables, tools, examples, evaluation, and streaming response preview."
topics: [architecture]
sources:
  - id: workbench-page
    type: file
    path: web/src/features/workbench/WorkbenchPage.tsx
  - id: workbench-shell
    type: file
    path: web/src/features/workbench/shell.tsx
  - id: workbench-editor
    type: file
    path: web/src/features/workbench/editor.tsx
  - id: workbench-drawers
    type: file
    path: web/src/features/workbench/drawers.tsx
  - id: workbench-evaluate
    type: file
    path: web/src/features/workbench/evaluate.tsx
  - id: workbench-model
    type: file
    path: web/src/features/workbench/model.ts
  - id: workbench-api
    type: file
    path: web/src/features/workbench/api.ts
  - id: agents-md
    type: file
    path: web/AGENTS.md
---

## Workbench UI

The Workbench provides a comprehensive prompt engineering interface for creating, testing, and evaluating prompts with Claude models. The `WorkbenchPage` component implements the full feature set with message editing, variable management, tool configuration, examples, and evaluation testing [@workbench-page].

## Page Layout

The Workbench uses a shell component (`WorkbenchShell`) that provides the main layout structure [@workbench-shell]. The page is divided into:
- **Top bar**: Prompt selector, new prompt button, prompt title, save status
- **View switcher**: Toggle between Prompt and Evaluate modes
- **Main area**: Message editor (left) and response preview (right)
- **Drawers**: Slide-out panels for model settings, variables, tools, examples, and history

## Prompt and Revision Management

The Workbench manages prompts as versioned revisions with:
- **Prompt list**: Searchable list of all prompts with filtering options
- **Create/copy/rename/delete**: Full CRUD operations for prompts
- **Version history**: Revision list with restore capability
- **Autosave**: Draft changes saved to workspace KV store
- **Auto-title**: AI-generated titles for untitled prompts

The prompt selector allows switching between prompts and creating new ones. Prompt changes are tracked with save status indicators.

## Message Editor

The message editor displays the prompt conversation with editable message blocks [@workbench-editor]. Each message shows:
- **Role badge**: User or assistant indicator
- **Content editor**: Editable text with variable highlighting
- **File attachments**: Support for image and file uploads
- **Action buttons**: Remove, add message pair, pre-fill response

The system prompt section expands to show role and context configuration. Variables in messages are highlighted and clickable to open the variables drawer.

## Variables

Variables are extracted from message content using double-brace syntax (e.g., `{{variable_name}}`) per the Workbench model [@workbench-model]. The variables drawer provides:
- **List view**: All detected variables with input fields
- **Generate button**: AI-powered variable value generation
- **Generation logic**: Instructions for generating realistic values
- **Test case generation**: Batch creation of evaluation test cases

Variable values are used when running the prompt to substitute placeholders.

## Tools

The tools drawer manages tools available to the prompt [@workbench-drawers]:
- **Custom tools**: User-defined tools with JSON schema
- **Web search**: Built-in web search tool configuration
- **Tool list**: Active tools with remove buttons
- **Add tool**: Forms for creating custom tools or web search

Tool configurations are included in the prompt revision and sent with run requests.

## Examples

Few-shot examples help the model understand the expected output format. The examples drawer [@workbench-drawers]:
- **List view**: Existing examples with variable values and ideal output
- **Add/edit form**: Create or modify examples with AI generation
- **Generate button**: AI-generated examples based on the prompt
- **Additional context**: Optional context field for complex examples

Examples are included in run requests to guide model behavior.

## Evaluate Mode

When the prompt has variables but no tools, the Evaluate tab becomes available per the Workbench evaluate component [@workbench-evaluate]. This view provides:
- **Test case table**: Variables values, ideal output, and model output columns
- **Run all**: Execute all test cases sequentially
- **Comparison mode**: Compare outputs across different model versions
- **CRUD operations**: Create, update, and delete test cases
- **Generate test cases**: AI-generated test cases with variable values

The evaluate view helps systematically test prompt behavior across different inputs.

## Running Prompts

The Run button executes the prompt with the current variable values, examples, and tool configuration. The run process:
- **Streaming**: Real-time response display via SSE
- **Abort**: Stop button to cancel in-progress runs
- **Error handling**: Display error messages for failed runs
- **Event tracking**: Log streaming events for debugging

The response preview shows the model output with syntax highlighting for code blocks.

## Code Export

The Get Code button opens a dialog with code snippets for running the prompt:
- **Language selector**: Python, TypeScript, and other languages
- **API code**: Ready-to-run code using the Anthropic API
- **Environment variables**: Configuration for API key and base URL

This helps developers integrate prompts into their applications.

## Keyboard Shortcuts

The Workbench supports keyboard shortcuts for efficiency:
- **Cmd+Enter**: Run the prompt (or Run All in Evaluate mode)
- **Escape**: Close dialogs and drawers

## State Management

The Workbench maintains extensive local state for:
- **Prompt data**: Current prompt, draft, revisions, and examples
- **UI state**: Active drawer, open dialogs, scroll positions
- **Run state**: Running status, response text, stream events
- **Form state**: Variable values, tool configurations, search queries

State updates trigger autosave drafts and URL synchronization for the active tab.

## Access Control

The Workbench checks account permissions to determine access. The `workbenchAccessState` function evaluates:
- **Account features**: Whether the account has Workbench access
- **Product name**: Display name for access denied message
- **Prepaid credits**: Credit balance for prompt generation features

Users without access see a friendly message instead of the full interface.

## Streaming

The Workbench uses SSE for streaming model responses per the frontend conventions [@agents-md]. The `streamWorkbenchCompletion` API [@workbench-api] sends the prompt and receives delta events that are accumulated for display. Streaming supports:
- **Abort controller**: Cancel in-progress requests
- **Delta parsing**: Extract text from streaming events
- **Error handling**: Display errors from failed streams

This provides real-time feedback during prompt execution.
