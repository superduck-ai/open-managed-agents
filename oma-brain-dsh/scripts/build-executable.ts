import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const filename =
  process.platform === "win32" ? "oma-brain-dsh.exe" : "oma-brain-dsh";
const outfile = join(root, "dist", filename);
await mkdir(join(root, "dist"), { recursive: true });

const result = await Bun.build({
  entrypoints: [join(root, "src/main.ts")],
  target: "bun",
  packages: "bundle",
  // Keep deployment configuration in the runtime environment, outside the binary.
  env: "disable",
  compile: {
    outfile,
    autoloadDotenv: true,
    autoloadBunfig: false,
    autoloadTsconfig: false,
    autoloadPackageJson: false,
  },
  plugins: [
    {
      name: "bundle-dsh-llm-version",
      setup(build) {
        build.onLoad(
          { filter: /[\\/]@deepseek-ai[\\/]dsh-llm[\\/]lib[\\/]index\.js$/ },
          async ({ path }) => {
            const contents = await Bun.file(path).text();
            const dynamic =
              'const { version } = createRequire(import.meta.url)("../package.json");';
            if (contents.split(dynamic).length !== 2) {
              throw new Error(
                "dsh-llm metadata lookup changed; review the executable build adapter",
              );
            }
            // Preserve dsh's own version while making the metadata visible to the bundler.
            // Only the in-memory build input changes; installed dsh files stay untouched.
            return {
              contents: contents.replace(
                dynamic,
                'import { version } from "../package.json";',
              ),
              loader: "js",
            };
          },
        );
      },
    },
  ],
});
if (!result.success)
  throw new AggregateError(result.logs, "Executable build failed");
console.log(`Built ${outfile} (${process.platform}-${process.arch})`);
