<template>
  <main>
    <p class="eyebrow">Heya Metadata v2</p>
    <h1>The development stack is running.</h1>
    <p>
      Nuxt is intentionally plain for now. The stable development proxy is
      serving this page while forwarding API traffic to the Air-managed Go
      backend.
    </p>
    <nav>
      <a href="/api/docs">Scalar API reference</a>
      <a href="/api/v2/health/live">Backend health</a>
    </nav>
  </main>
</template>

<style>
:root {
  color-scheme: dark;
  font-family: Inter, ui-sans-serif, system-ui, sans-serif;
  background: #111827;
  color: #e5e7eb;
}

body {
  margin: 0;
}

main {
  box-sizing: border-box;
  max-width: 52rem;
  min-height: 100vh;
  margin: 0 auto;
  padding: clamp(4rem, 12vw, 9rem) 2rem;
}

.eyebrow {
  color: #5eead4;
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

h1 {
  max-width: 14ch;
  margin: 1rem 0;
  color: #f9fafb;
  font-size: clamp(2.5rem, 7vw, 5rem);
  line-height: 0.98;
}

p {
  max-width: 42rem;
  color: #9ca3af;
  font-size: 1.1rem;
  line-height: 1.7;
}

nav {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  margin-top: 2rem;
}

a {
  border: 1px solid #374151;
  border-radius: 0.6rem;
  padding: 0.75rem 1rem;
  color: #ccfbf1;
  text-decoration: none;
}

a:hover {
  border-color: #5eead4;
  background: #1f2937;
}
</style>
