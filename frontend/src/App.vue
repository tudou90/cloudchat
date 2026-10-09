<template>
  <component :is="view" />
</template>

<script setup lang="ts">
import ChatView from './views/ChatView.vue'
import SecretCreateView from './views/SecretCreateView.vue'
import SecretRevealView from './views/SecretRevealView.vue'

// Minimal path routing: /chat/secret -> create, /chat/secret/<id> -> reveal.
const path = location.pathname.slice(import.meta.env.BASE_URL.length).replace(/\/+$/, '')
const view = path === 'secret'
  ? SecretCreateView
  : path.startsWith('secret/') ? SecretRevealView : ChatView
</script>
