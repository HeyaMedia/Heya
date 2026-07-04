#!/usr/bin/env bun
// dead-components — list Vue components under web/app/components that have
// zero references anywhere in web/app + web/shared.
//
// Component names are file basenames (Nuxt is configured with
// `pathPrefix: false`). Reference forms checked, per component `FooBar`:
//
//   <FooBar ...>             PascalCase tag
//   <foo-bar ...>            kebab-case tag
//   LazyFooBar               Nuxt lazy prefix (tag or code token)
//   .../FooBar.vue           explicit import path
//   'FooBar' / "FooBar"      resolveComponent / dynamic <component :is>
//
// Output is a list of CANDIDATES for manual review (mutually-referencing
// dead clusters and template-string usages aren't detected). Always exits 0
// — this is a report, not an error gate. Dependency-free: Bun.Glob + fs.
//
// Run: `bun tools/dead-components.ts` (any cwd — paths resolve from this file)
// or `make dead-components`.

import { Glob } from "bun";
import { readFile } from "node:fs/promises";
import { basename, join, resolve } from "node:path";

const webDir = resolve(import.meta.dir, "..", "web");
const componentsDir = join(webDir, "app", "components");

const kebab = (name: string): string =>
  name.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();

// Component inventory: every .vue file under web/app/components.
const componentFiles: string[] = [];
for await (const rel of new Glob("**/*.vue").scan({ cwd: componentsDir })) {
  componentFiles.push(join(componentsDir, rel));
}
componentFiles.sort();

// Haystack: all code files under web/app + web/shared.
const contents = new Map<string, string>();
const codeGlob = new Glob("**/*.{vue,ts,tsx,js,mjs}");
for (const root of [join(webDir, "app"), join(webDir, "shared")]) {
  for await (const rel of codeGlob.scan({ cwd: root })) {
    const file = join(root, rel);
    contents.set(file, await readFile(file, "utf8"));
  }
}

const dead: string[] = [];
for (const file of componentFiles) {
  const name = basename(file, ".vue");
  const needles = [
    `<${name}`,
    `<${kebab(name)}`,
    `Lazy${name}`,
    `${name}.vue`,
    `'${name}'`,
    `"${name}"`,
  ];
  let referenced = false;
  for (const [other, text] of contents) {
    if (other === file) continue; // self-references don't keep a component alive
    if (needles.some((n) => text.includes(n))) {
      referenced = true;
      break;
    }
  }
  if (!referenced) dead.push(file);
}

if (dead.length === 0) {
  console.log(
    `dead-components: all ${componentFiles.length} components referenced`,
  );
} else {
  console.log(
    `dead-components: ${dead.length} of ${componentFiles.length} components have zero references:`,
  );
  for (const f of dead) {
    console.log(`  web/${f.slice(webDir.length + 1)}`);
  }
}
