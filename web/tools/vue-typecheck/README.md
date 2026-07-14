# Vue type-check compatibility environment

The Heya frontend uses TypeScript 7. The current `vue-tsc` release still loads
compiler internals that TypeScript 7 no longer exports, so Vue SFC/template
checking runs here against TypeScript 5.9 until Vue's language tools support
TypeScript 7.

The root `bun run typecheck` command runs both checks:

- TypeScript 7 checks the Nuxt-generated project and ordinary `.ts` files.
- This isolated `vue-tsc` install checks `.vue` scripts and templates.

Keep this package isolated: moving `vue-tsc` back to the root would make it
resolve the root TypeScript 7 compiler and fail before checking the project.
