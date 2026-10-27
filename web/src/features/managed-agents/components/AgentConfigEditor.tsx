import { indentWithTab } from '@codemirror/commands';
import { json as codeMirrorJson } from '@codemirror/lang-json';
import { yaml as codeMirrorYaml } from '@codemirror/lang-yaml';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { type Diagnostic, linter, lintGutter } from '@codemirror/lint';
import { EditorState, type Extension } from '@codemirror/state';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { tags as syntaxTags } from '@lezer/highlight';
import CodeMirror from '@uiw/react-codemirror';
import { type CSSProperties, useCallback, useMemo } from 'react';
import { useI18n } from '../../../shared/i18n';
import { type CodeFormat } from '../types';

export function normalizeAgentConfigEditorText(text: unknown) {
  return String(text ?? '')
    .replace(/\r\n?/g, '\n')
    .replace(/\u00a0/g, ' ');
}

export const agentConfigEditorTheme = EditorView.theme(
  {
    '&': {
      height: '100%',
      backgroundColor: 'transparent',
      color: 'var(--code-foreground)',
      fontSize: '13px',
    },
    '.cm-scroller': {
      fontFamily: 'var(--font-mono)',
      lineHeight: '20px',
      minHeight: '0',
      overflow: 'auto',
    },
    '.cm-content': {
      minHeight: 'var(--agent-config-editor-min-height, 220px)',
      padding: '12px 1.25rem 16px 0',
      caretColor: 'var(--foreground)',
    },
    '.cm-line': {
      padding: '0',
    },
    '.cm-gutters': {
      backgroundColor: 'transparent',
      borderRight: '0',
      color: 'var(--muted-foreground)',
    },
    '.cm-lineNumbers .cm-gutterElement': {
      minWidth: '3.75rem',
      padding: '0 1rem 0 1.5rem',
      textAlign: 'right',
    },
    '.cm-activeLine': {
      backgroundColor: 'var(--sand-a2)',
    },
    '.cm-activeLineGutter': {
      backgroundColor: 'transparent',
      color: 'var(--muted-foreground)',
    },
    '.cm-cursor, .cm-dropCursor': {
      borderLeftColor: 'var(--foreground)',
    },
    '.cm-content ::selection': {
      backgroundColor: 'var(--sand-a5)',
      color: 'var(--foreground)',
    },
    '&.cm-focused': {
      outline: 'none',
    },
    '.cm-focused &': {
      outline: 'none',
    },
    '.cm-lintRange-error': {
      backgroundImage: 'linear-gradient(45deg, transparent 65%, var(--destructive) 80%, transparent 90%)',
      backgroundPosition: 'left bottom',
      backgroundRepeat: 'repeat-x',
      backgroundSize: '8px 3px',
    },
    '.cm-lint-marker-error': {
      color: 'var(--destructive)',
    },
    '.cm-tooltip': {
      border: '0.5px solid var(--border)',
      borderRadius: '8px',
      backgroundColor: 'var(--popover)',
      color: 'var(--foreground)',
      boxShadow: 'var(--shadow-popover)',
    },
    '.cm-tooltip-lint': {
      padding: '6px 8px',
    },
    '.cm-diagnostic-error': {
      borderLeft: 'none',
      color: 'var(--destructive)',
    },
    '.cm-diagnosticSource': {
      color: 'var(--muted-foreground)',
    },
    '.cm-panels': {
      backgroundColor: 'var(--popover)',
      color: 'var(--foreground)',
    },
    '.cm-panels-bottom': {
      borderTop: '0.5px solid var(--border)',
    },
    '.cm-panels-top': {
      borderBottom: '0.5px solid var(--border)',
    },
    '.cm-panel.cm-search label, .cm-textfield, .cm-button, .cm-diagnosticSource': {
      fontSize: '12px',
    },
    '.cm-textfield': {
      backgroundColor: 'var(--background)',
      color: 'var(--foreground)',
      border: '0.5px solid var(--input)',
      borderRadius: '4px',
    },
    '.cm-button': {
      backgroundImage: 'none',
      backgroundColor: 'var(--secondary)',
      color: 'var(--secondary-foreground)',
      border: '0.5px solid var(--border)',
      borderRadius: '4px',
    },
    '.cm-button:hover, .cm-button:active': {
      backgroundImage: 'none',
      backgroundColor: 'var(--muted)',
    },
    '.cm-textfield:focus-visible, .cm-button:focus-visible': {
      outline: '0.5px solid var(--ring)',
      outlineOffset: '-0.5px',
    },
    '.cm-panel.cm-search input[type=checkbox]': {
      accentColor: 'var(--primary)',
    },
    '.cm-searchMatch': {
      backgroundColor: 'var(--violet-a3)',
      color: 'var(--foreground)',
    },
    '.cm-searchMatch-selected': {
      backgroundColor: 'var(--violet-a5)',
    },
  },
  { dark: true },
);

export const agentConfigHighlightStyle = HighlightStyle.define([
  { tag: [syntaxTags.propertyName, syntaxTags.attributeName], color: 'var(--syntax-key)' },
  { tag: [syntaxTags.string, syntaxTags.special(syntaxTags.string)], color: 'var(--syntax-string)' },
  { tag: [syntaxTags.number, syntaxTags.integer, syntaxTags.float], color: 'var(--syntax-number)' },
  { tag: [syntaxTags.bool, syntaxTags.null], color: 'var(--syntax-literal)' },
  { tag: [syntaxTags.keyword, syntaxTags.atom], color: 'var(--syntax-keyword)' },
  { tag: [syntaxTags.comment, syntaxTags.lineComment, syntaxTags.blockComment], color: 'var(--syntax-comment)' },
  {
    tag: [syntaxTags.punctuation, syntaxTags.separator, syntaxTags.brace, syntaxTags.squareBracket],
    color: 'var(--syntax-punctuation)',
  },
  { tag: [syntaxTags.heading, syntaxTags.name], color: 'var(--syntax-title)' },
]);

export const agentConfigEditorBasicSetup = {
  lineNumbers: false,
  foldGutter: false,
  highlightActiveLineGutter: false,
  highlightActiveLine: true,
  highlightSpecialChars: true,
  history: true,
  drawSelection: false,
  dropCursor: true,
  allowMultipleSelections: false,
  indentOnInput: true,
  bracketMatching: true,
  closeBrackets: true,
  autocompletion: false,
  rectangularSelection: false,
  crosshairCursor: false,
  highlightSelectionMatches: true,
  searchKeymap: true,
  foldKeymap: false,
  completionKeymap: false,
  lintKeymap: true,
  tabSize: 2,
} as const;

export function AgentConfigEditor({
  value,
  format,
  onChange,
  id = 'create-agent-config-editor',
  ariaLabel,
  lineNumbers: showLineNumbers = false,
  validate,
  minHeight = '220px',
}: {
  value: string;
  format: CodeFormat;
  onChange: (value: string) => void;
  id?: string;
  ariaLabel?: string;
  lineNumbers?: boolean;
  validate?: (text: string, format: CodeFormat) => string | null;
  minHeight?: string;
}) {
  const { msg } = useI18n();
  const label = ariaLabel ?? `Agent config ${format}`;
  const editorStyle = useMemo<CSSProperties>(
    () =>
      ({
        '--agent-config-editor-min-height': minHeight,
      }) as CSSProperties,
    [minHeight],
  );
  const extensions = useMemo<Extension[]>(
    () => [
      format === 'YAML' ? codeMirrorYaml() : codeMirrorJson(),
      syntaxHighlighting(agentConfigHighlightStyle),
      agentConfigEditorTheme,
      EditorState.phrases.of({
        Find: msg('editor.find', 'Find'),
        Replace: msg('editor.replace', 'Replace'),
        next: msg('editor.nextMatch', 'Next'),
        previous: msg('editor.previousMatch', 'Previous'),
        all: msg('editor.selectMatches', 'Select all'),
        'match case': msg('editor.matchCase', 'Match case'),
        regexp: msg('editor.regexp', 'Regular expression'),
        'by word': msg('editor.wholeWord', 'Whole word'),
        replace: msg('editor.replace', 'Replace'),
        'replace all': msg('editor.replaceAll', 'Replace all'),
        close: msg('common.close', 'Close'),
        Diagnostics: msg('editor.diagnostics', 'Diagnostics'),
        'No diagnostics': msg('editor.noDiagnostics', 'No diagnostics'),
      }),
      EditorView.lineWrapping,
      EditorView.contentAttributes.of({
        role: 'textbox',
        'aria-label': label,
        'aria-multiline': 'true',
        spellcheck: 'false',
      }),
      ...(showLineNumbers ? [lineNumbers()] : []),
      lintGutter(),
      linter((view) => agentConfigEditorDiagnostics(view.state.doc.toString(), format, validate), { delay: 250 }),
      keymap.of([indentWithTab]),
    ],
    [format, label, msg, showLineNumbers, validate],
  );

  const handleChange = useCallback(
    (nextValue: string) => {
      onChange(normalizeAgentConfigEditorText(nextValue));
    },
    [onChange],
  );
  const handleCreateEditor = useCallback((view: EditorView) => {
    (view.dom as HTMLElement & { __agentConfigCodeMirrorView?: EditorView }).__agentConfigCodeMirrorView = view;
    (view.contentDOM as HTMLElement & { __agentConfigCodeMirrorView?: EditorView }).__agentConfigCodeMirrorView = view;
  }, []);

  return (
    <div id={id} role="tabpanel" className="agent-config-codemirror min-h-0 flex-1 overflow-hidden" style={editorStyle}>
      <CodeMirror
        className="h-full min-h-0 overflow-hidden"
        value={value}
        height="100%"
        minHeight={minHeight}
        basicSetup={agentConfigEditorBasicSetup}
        extensions={extensions}
        theme="none"
        indentWithTab={false}
        onChange={handleChange}
        onCreateEditor={handleCreateEditor}
      />
    </div>
  );
}

export function agentConfigEditorDiagnostics(
  text: string,
  format: CodeFormat,
  validate?: (text: string, format: CodeFormat) => string | null,
): Diagnostic[] {
  const message = validate?.(text, format) ?? null;
  if (!message) {
    return [];
  }

  const firstLineLength = text.split('\n', 1)[0]?.length ?? 0;
  return [
    {
      from: 0,
      to: Math.min(text.length, Math.max(1, firstLineLength)),
      severity: 'error',
      source: 'Agent config',
      message,
    },
  ];
}
