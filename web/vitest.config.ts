import { defineConfig } from 'vitest/config'

// display.ts is pure and Nuxt-free, so a plain node environment is enough.
export default defineConfig({
  test: {
    environment: 'node',
    include: ['tests/**/*.test.ts'],
  },
})
