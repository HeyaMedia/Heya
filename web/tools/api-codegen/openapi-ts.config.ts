import { defineConfig } from '@hey-api/openapi-ts'

export default defineConfig({
  input: '../../shared/api.openapi.json',
  output: {
    path: '../../shared/api',
    header: [
      '// Generated from shared/api.openapi.json by @hey-api/openapi-ts.',
      '// Do not edit by hand; run `make gen-api-client`.',
    ],
  },
  plugins: ['@hey-api/typescript'],
})
