<script setup lang="ts">
// Cast/crew rail. Accepts the movie/tv credit shape
// {display_name, character, credit_type, profile_image_id}.
const props = withDefaults(defineProps<{
  credits?: any[]
  title?: string
  kicker?: string
  limit?: number
}>(), { credits: () => [], title: 'Cast & crew', kicker: 'People', limit: 24 })

const people = computed(() =>
  (props.credits ?? [])
    .slice(0, props.limit)
    .map(credit => ({
      name: formatValue(credit.display_name ?? credit.name),
      role: formatValue(credit.character) || titleCase(credit.job ?? credit.credit_type ?? credit.role),
      imageId: credit.profile_image_id as string | undefined,
    }))
    .filter(person => person.name),
)
</script>

<template>
  <section v-if="people.length" class="rail credits-rail">
    <header class="rail__head">
      <div>
        <span class="section-label">{{ kicker }}</span>
        <h2>{{ title }}</h2>
      </div>
    </header>
    <div class="rail-track is-portrait">
      <PersonCard
        v-for="(person, index) in people"
        :key="`${person.name}-${index}`"
        :name="person.name"
        :role="person.role"
        :image-id="person.imageId"
      />
    </div>
  </section>
</template>

<style scoped>
.credits-rail { margin-top: 2.5rem; }
</style>
