# OpenAPI code-generation environment

The Heya frontend uses TypeScript 7, while the current OpenAPI generator still
executes compiler APIs removed by TypeScript 7. Code generation therefore runs
in this isolated TypeScript 5.9 environment and writes framework-independent
types to `web/shared/api/`.

Normal development should invoke `make gen-api-client` from the repository
root. The command regenerates both the OpenAPI JSON and these committed types.
