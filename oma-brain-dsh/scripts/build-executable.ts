import { mkdir, readdir } from "node:fs/promises";
import { dirname, join, relative } from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const require = createRequire(import.meta.url);
function nativeFlockPath(): string | undefined {
  if (process.platform !== "darwin" && process.platform !== "linux")
    return undefined;
  const manifest = require.resolve(
    `@deepseek-ai/node-addon-system-${process.platform}-${process.arch}/package.json`,
  );
  const directory = dirname(manifest);
  if (process.platform === "darwin")
    return join(directory, "bin", "system.node");
  const report = process.report.getReport() as {
    header: { glibcVersionRuntime?: string };
  };
  return join(
    directory,
    "bin",
    report.header.glibcVersionRuntime ? "glibc" : "musl",
    "system.node",
  );
}
const flockPath = nativeFlockPath();
const filename =
  process.platform === "win32" ? "oma-brain-dsh.exe" : "oma-brain-dsh";
const outfile = join(root, "dist", filename);
await mkdir(join(root, "dist"), { recursive: true });

const platform =
  process.platform === "linux" &&
  !(process.report.getReport() as { header: { glibcVersionRuntime?: string } })
    .header.glibcVersionRuntime
    ? "linuxmusl"
    : process.platform;
const imagePackages = [
  `sharp-${platform}-${process.arch}`,
  `sharp-libvips-${platform}-${process.arch}`,
];
const imageFiles: { path: string; destination: string }[] = [];
for (const name of imagePackages) {
  const directory = dirname(require.resolve(`@img/${name}/package`));
  const library = join(directory, "lib");
  for (const entry of await readdir(library, {
    recursive: true,
    withFileTypes: true,
  })) {
    if (entry.isFile())
      imageFiles.push({
        path: join(entry.parentPath, entry.name),
        destination: join(
          "@img",
          name,
          "lib",
          relative(library, join(entry.parentPath, entry.name)),
        ),
      });
  }
}
const imageHash = new Bun.CryptoHasher("sha256");
for (const file of imageFiles) {
  imageHash.update(file.destination);
  imageHash.update(await Bun.file(file.path).arrayBuffer());
}
const imageBinding = imageFiles.find((file) =>
  file.destination.endsWith(".node"),
);
if (!imageBinding) throw new Error("Sharp native binding is missing");
const imageLoader =
  imageFiles
    .map(
      (file, index) =>
        `import file${index} from ${JSON.stringify(file.path)} with { type: "file" };`,
    )
    .join("\n") +
  `
import { readFileSync, writeFileSync, mkdirSync, existsSync, renameSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";
import { createRequire } from "node:module";
const root = join(homedir(), ".cache", "oma-brain", "native", ${JSON.stringify(imageHash.digest("hex"))});
${imageFiles
  .map(
    (file, index) => `{
  const path = join(root, ${JSON.stringify(file.destination)});
  if (!existsSync(path)) {
    mkdirSync(dirname(path), { recursive: true });
    const pending = path + "." + process.pid;
    writeFileSync(pending, readFileSync(file${index}));
    renameSync(pending, path);
  }
}`,
  )
  .join("\n")}
module.exports = createRequire(import.meta.url)(join(root, ${JSON.stringify(imageBinding.destination)}));
`;

const result = await Bun.build({
  entrypoints: [join(root, "src/main.ts")],
  target: "bun",
  packages: "bundle",
  define: { OMA_BRAIN_STANDALONE: "true" },
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
        build.onLoad({ filter: /[\\/]sharp\.cjs$/ }, () => ({
          contents: imageLoader,
          loader: "js",
        }));
        build.onLoad(
          { filter: /[\\/]dsh-attachment-local[\\/]lib[\\/]index\.js$/ },
          async ({ path }) => {
            const contents = await Bun.file(path).text();
            const dynamic =
              'const requireSharp = createLazyRequire("sharp", import.meta.url);';
            if (contents.split(dynamic).length !== 2)
              throw new Error(
                "DSH image loader changed; review the executable build adapter",
              );
            return {
              contents: contents.replace(
                dynamic,
                `const requireSharp = () => require(${JSON.stringify(require.resolve("sharp"))});`,
              ),
              loader: "js",
            };
          },
        );
        if (flockPath)
          build.onLoad(
            {
              filter:
                /[\\/]@deepseek-ai[\\/]node-addon-system[\\/]lib[\\/]flock\.js$/,
            },
            async ({ path }) => {
              const contents = await Bun.file(path).text();
              const dynamic = [
                "const require = createRequire(import.meta.url);",
                "const manifest = require.resolve(`@deepseek-ai/node-addon-system-${platform}-${arch}/package.json`);",
                "binding = require(join(dirname(manifest), 'bin', filename));",
              ].join("\n    ");
              if (contents.split(dynamic).length !== 2)
                throw new Error(
                  "Native flock loader changed; review the executable build adapter",
                );
              return {
                contents: contents.replace(
                  dynamic,
                  `binding = require(${JSON.stringify(flockPath)});`,
                ),
                loader: "js",
              };
            },
          );
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
