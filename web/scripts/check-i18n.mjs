import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

const sourceRoot = fileURLToPath(new URL('../src', import.meta.url));
const catalogs = ['en', 'zh-CN'].map((locale) => ({
  locale,
  messages: JSON.parse(readFileSync(`${sourceRoot}/shared/i18n/messages/${locale}.json`, 'utf8')),
}));
let errors = 0;
let checked = 0;

for (const filename of ts.sys.readDirectory(sourceRoot, ['.ts', '.tsx'], ['**/*.test.*', '**/test/**'])) {
  const source = ts.createSourceFile(filename, readFileSync(filename, 'utf8'), ts.ScriptTarget.Latest, true);
  function checkKey(node) {
    if (!node || !ts.isStringLiteralLike(node)) return;
    checked++;
    for (const { locale, messages } of catalogs) {
      if (Object.hasOwn(messages, node.text)) continue;
      const { line } = source.getLineAndCharacterOfPosition(node.getStart(source));
      console.error(`${filename}:${line + 1} missing ${locale} message ${node.text}`);
      errors++;
    }
  }
  function visit(node) {
    if (ts.isCallExpression(node)) {
      const name = node.expression;
      if (
        (ts.isIdentifier(name) && name.text === 'msg') ||
        (ts.isPropertyAccessExpression(name) && name.name.text === 'msg')
      ) {
        checkKey(node.arguments[0]);
      }
    }
    if (ts.isPropertyAssignment(node) && node.name.getText(source).replace(/['"]/g, '') === 'labelId') {
      checkKey(node.initializer);
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
}

console.log(`Checked ${checked} literal msg and labelId references across ${catalogs.length} catalogs.`);
process.exitCode = errors ? 1 : 0;
