export const roleOptions = [
  {
    label: "User",
    value: "user",
    description: "Use Workbench",
  },
  {
    label: "Claude Code",
    value: "claude_code_user",
    description: "Use Workbench and Claude Code",
  },
  {
    label: "Developer",
    value: "developer",
    description: "Use Workbench, Claude Code and manage API keys",
  },
  {
    label: "Billing",
    value: "billing",
    description: "Use Workbench and manage billing details",
  },
  {
    label: "Admin",
    value: "admin",
    description: "Do all of the above, plus manage users",
  },
] as const;

export type PlatformRole = (typeof roleOptions)[number]["value"];
