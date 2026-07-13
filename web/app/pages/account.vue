<script setup lang="ts">
// Signed-in user's account. Requires a session; redirects to /login once we know
// there isn't one.
useHead({ title: 'Account · Heya Metadata' })

const auth = useAuth()
const { user, ready, isAuthenticated } = auth

watch([ready, isAuthenticated], () => {
  if (ready.value && !isAuthenticated.value) {
    navigateTo({ path: '/login', query: { redirect: '/account' } })
  }
}, { immediate: true })

async function signOut() {
  await auth.logout()
  await navigateTo('/')
}
</script>

<template>
  <div class="shell page">
    <div v-if="!ready" class="progress-line"><span class="spinner" /><p>Loading your account…</p></div>

    <template v-else-if="user">
      <header class="page-heading">
        <div>
          <span class="section-label">Signed in</span>
          <h1>{{ user.username }}</h1>
        </div>
        <button type="button" class="btn" @click="signOut">Sign out</button>
      </header>

      <div class="overview-grid">
        <OverviewPanel title="Account" kicker="Identity">
          <FactList :facts="[
            { label: 'Username', value: user.username },
            { label: 'Role', value: titleCase(user.role || 'user') },
            { label: 'Member since', value: formatDate(user.created_at) },
            { label: 'Account ID', value: user.id },
          ]" />
        </OverviewPanel>

        <OverviewPanel title="Your contributions" kicker="Attribution">
          <p class="muted account__note">
            Anything you contribute will be attributed to this account. Contribution tools aren't
            available yet — this is where your uploads and edits will appear.
          </p>
        </OverviewPanel>
      </div>
    </template>
  </div>
</template>

<style scoped>
.account__note { margin: 0; font-size: 0.82rem; line-height: 1.7; }
</style>
