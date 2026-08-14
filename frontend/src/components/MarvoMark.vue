<script setup lang="ts">
withDefaults(defineProps<{ title?: string }>(), {
  title: '',
})
</script>

<template>
  <svg
    class="marvo-mark"
    viewBox="0 0 64 64"
    :role="title ? 'img' : undefined"
    :aria-hidden="title ? undefined : 'true'"
    focusable="false"
  >
    <title v-if="title">{{ title }}</title>
    <path
      fill="currentColor"
      fill-rule="evenodd"
      d="M32 3 58 18v28L32 61 6 46V18zM16 20h13v13H16zm19 0h13v13H35zM21 33h8c0 7-3 11-9 13v-7c3-1 4-3 4-6zm19 0h8c0 7-3 11-9 13v-7c3-1 4-3 4-6z"
    />
  </svg>
</template>
