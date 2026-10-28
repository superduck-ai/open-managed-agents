import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import type { Plugin } from 'vite';

const require = createRequire(import.meta.url);
const packages = new Set(['@deepseek-ai/dsh-client-ui-trajectory', '@deepseek-ai/dsh-client-ui-conversation']);

export function harnessPlugin(): Plugin {
  return {
    name: 'harness-native-modules',
    resolveId(id) {
      if (!id.startsWith('harness:')) return;
      const name = id.slice('harness:'.length);
      if (!packages.has(name)) throw new Error(`Unsupported Harness module ${name}`);
      return `\0harness:${name}`;
    },
    async load(id) {
      if (!id.startsWith('\0harness:')) return;
      const name = id.slice('\0harness:'.length);
      const source = await readFile(require.resolve(`${name}/client`), 'utf8');
      const dependencies = [...new Set([...source.matchAll(/require\("([^"]+)"\)/g)].map((match) => match[1]))];
      const imports = dependencies
        .map((dependency, index) => `import * as dep${index} from ${JSON.stringify(dependency)};`)
        .join('\n');
      const entries = dependencies.map((dependency, index) => `${JSON.stringify(dependency)}:dep${index}`).join(',');
      if (!source.startsWith('window.__ModuleLoader__.load('))
        throw new Error(`Unexpected Harness module format ${name}`);
      const factory = source.replace('window.__ModuleLoader__.load(', 'const registration = (');
      return `${imports}\n${factory}\nconst modules={${entries}};\nexport default registration.factory((id)=>{if (!(id in modules)) throw new Error('Unknown Harness dependency '+id); return modules[id];});`;
    },
  };
}
