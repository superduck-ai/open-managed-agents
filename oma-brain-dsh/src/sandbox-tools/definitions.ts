import type { RemoteTool } from "../runtime/contracts.ts";

// Built-in contract for the OMA sandbox MCP aliases. No runtime catalog lookup.
const definitions: RemoteTool[] = [
  {
    "name": "Bash",
    "remoteName": "bash",
    "description": "Execute a Bash command in the remote sandbox. Timeout is in seconds, defaults to 120 and is capped at 600. Use work_dir to select the working directory.",
    "inputSchema": {
      "properties": {
        "command": {
          "description": "The bash command to execute.",
          "type": "string"
        },
        "env": {
          "additionalProperties": {
            "type": "string"
          },
          "description": "Extra environment variables set process-level on the spawned command (inherited by child processes).",
          "type": "object"
        },
        "on_timeout": {
          "description": "What to do when timeout elapses: kill (default) terminates the process group; background leaves it running and returns output_file + pid.",
          "enum": [
            "kill",
            "background"
          ],
          "type": "string"
        },
        "timeout": {
          "description": "Timeout in seconds (default 120, max 600).",
          "type": "integer"
        },
        "work_dir": {
          "description": "Working directory for the command (default: the server's work dir). A relative path is resolved against the default.",
          "type": "string"
        }
      },
      "required": [
        "command"
      ],
      "type": "object"
    }
  },
  {
    "name": "Read",
    "remoteName": "read",
    "description": "Read a file in the remote sandbox. file_path must be absolute. Optional start_line is zero-based and end_line is exclusive.",
    "inputSchema": {
      "properties": {
        "end_line": {
          "description": "optional, 0-based ending line index (exclusive)",
          "type": "integer"
        },
        "file_path": {
          "description": "absolute path of source file",
          "type": "string"
        },
        "start_line": {
          "description": "optional, 0-based starting line index",
          "type": "integer"
        }
      },
      "required": [
        "file_path"
      ],
      "type": "object"
    }
  },
  {
    "name": "Write",
    "remoteName": "write",
    "description": "Write content to an absolute file_path in the remote sandbox, replacing existing contents.",
    "inputSchema": {
      "properties": {
        "content": {
          "description": "The content to write to the file",
          "type": "string"
        },
        "file_path": {
          "description": "The absolute path to the file to write (must be absolute, not relative)",
          "type": "string"
        }
      },
      "required": [
        "file_path",
        "content"
      ],
      "type": "object"
    }
  },
  {
    "name": "Edit",
    "remoteName": "edit",
    "description": "Replace old_string with new_string in a remote sandbox file. file_path must be absolute.",
    "inputSchema": {
      "properties": {
        "file_path": {
          "description": "The absolute path to the file to modify (must be absolute, not relative)",
          "type": "string"
        },
        "new_string": {
          "description": "The text to replace it with",
          "type": "string"
        },
        "old_string": {
          "description": "The text to replace",
          "type": "string"
        }
      },
      "required": [
        "file_path",
        "old_string",
        "new_string"
      ],
      "type": "object"
    }
  },
  {
    "name": "Grep",
    "remoteName": "grep",
    "description": "Search remote sandbox files with ripgrep. Choose content, files_with_matches or count using output_mode; include and type filter files.",
    "inputSchema": {
      "properties": {
        "-A": {
          "description": "Number of lines to show after each match (rg -A). Requires output_mode: \"content\", ignored otherwise.",
          "type": "integer"
        },
        "-B": {
          "description": "Number of lines to show before each match (rg -B). Requires output_mode: \"content\", ignored otherwise.",
          "type": "integer"
        },
        "-C": {
          "description": "Alias for context.",
          "type": "integer"
        },
        "-i": {
          "description": "Case insensitive search (rg -i)",
          "type": "boolean"
        },
        "-n": {
          "description": "Show line numbers in output (rg -n). Requires output_mode: \"content\", ignored otherwise. Defaults to true.",
          "type": "boolean"
        },
        "context": {
          "description": "Number of lines to show before and after each match (rg -C). Requires output_mode: \"content\", ignored otherwise.",
          "type": "integer"
        },
        "head_limit": {
          "description": "Limit output to first N lines/entries, equivalent to \"| head -N\". Works across all output modes: content (limits output lines), files_with_matches (limits file paths), count (limits count entries). Defaults to 250 when unspecified. Pass 0 for unlimited (use sparingly — large result sets waste context).",
          "type": "integer"
        },
        "include": {
          "description": "Glob pattern to filter files (e.g. \"*.js\", \"*.{ts,tsx}\") - maps to rg --glob",
          "type": "string"
        },
        "multiline": {
          "description": "Enable multiline mode where . matches newlines and patterns can span lines (rg -U --multiline-dotall). Default: false.",
          "type": "boolean"
        },
        "offset": {
          "description": "Skip first N lines/entries before applying head_limit, equivalent to \"| tail -n +N | head -N\". Works across all output modes. Defaults to 0.",
          "type": "integer"
        },
        "output_mode": {
          "description": "Output mode: \"content\" shows matching lines (supports -A/-B/-C context, -n line numbers, head_limit), \"files_with_matches\" shows file paths (supports head_limit), \"count\" shows match counts (supports head_limit). Defaults to \"files_with_matches\".",
          "enum": [
            "content",
            "files_with_matches",
            "count"
          ],
          "type": "string"
        },
        "path": {
          "description": "File or directory to search in (rg PATH). Defaults to current working directory",
          "type": "string"
        },
        "pattern": {
          "description": "The regex pattern to search for in file contents",
          "type": "string"
        },
        "type": {
          "description": "File type to search (rg --type). Common types: js, py, rust, go, java, etc. More efficient than include for standard file types.",
          "type": "string"
        }
      },
      "required": [
        "pattern"
      ],
      "type": "object"
    }
  },
  {
    "name": "Glob",
    "remoteName": "glob",
    "description": "Find remote sandbox files by glob pattern. Optional path selects the directory; results are sorted by modification time, oldest first.",
    "inputSchema": {
      "properties": {
        "path": {
          "description": "The directory to search in. Defaults to the current working directory.",
          "type": "string"
        },
        "pattern": {
          "description": "The glob pattern to match files against (e.g. \"**/*.ts\", \"src/**/*.js\")",
          "type": "string"
        }
      },
      "required": [
        "pattern"
      ],
      "type": "object"
    }
  }
];

/** Fresh definitions for each plugin instance; schemas are never shared mutably. */
export function sandboxToolDefinitions(): RemoteTool[] {
  return structuredClone(definitions);
}
